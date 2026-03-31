// Package sshd provides a minimal embedded SSH server for bootstrap.
// It handles exec requests only (sufficient for ssh and scp), using
// public key authentication. Used by "jumpgate bootstrap" to accept
// connections through the relay tunnel before real sshd is available.
package sshd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Server is a minimal SSH server for bootstrap. It listens on a local
// port, authenticates with a single authorized public key, and handles
// exec requests by spawning the platform shell.
type Server struct {
	config         *ssh.ServerConfig
	addr           string
	listener       net.Listener
	fingerprint    string
	authKeyType    string
	authKeyComment string
}

// New creates a Server that listens on addr, authenticates against the
// public key in authorizedKeyPath, and presents the host key from
// hostKeyPath.
func New(hostKeyPath, authorizedKeyPath, addr string) (*Server, error) {
	hostKeyBytes, err := os.ReadFile(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading host key: %w", err)
	}
	hostKey, err := ssh.ParsePrivateKey(hostKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing host key: %w", err)
	}

	authKeyBytes, err := os.ReadFile(authorizedKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading authorized key: %w", err)
	}
	authorizedKey, comment, _, _, err := ssh.ParseAuthorizedKey(authKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing authorized key: %w", err)
	}

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authorizedKey.Marshal()) {
				slog.Debug("bootstrap-sshd: pubkey accepted", "user", conn.User())
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("unknown public key for %s", conn.User())
		},
	}
	cfg.AddHostKey(hostKey)

	fp := ssh.FingerprintSHA256(hostKey.PublicKey())
	slog.Info("bootstrap-sshd: host key fingerprint", "fingerprint", fp)

	return &Server{
		config:         cfg,
		addr:           addr,
		fingerprint:    fp,
		authKeyType:    authorizedKey.Type(),
		authKeyComment: comment,
	}, nil
}

// Fingerprint returns the SHA256 fingerprint of the server's host key.
func (s *Server) Fingerprint() string {
	return s.fingerprint
}

// AuthKeyType returns the SSH key type of the authorized key (e.g. "ssh-ed25519").
func (s *Server) AuthKeyType() string {
	return s.authKeyType
}

// AuthKeyComment returns the comment from the authorized key file (typically
// the key name or email address).
func (s *Server) AuthKeyComment() string {
	return s.authKeyComment
}

// ListenAndServe starts the SSH server. It blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	s.listener = ln

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	slog.Info("bootstrap-sshd: listening", "addr", s.addr)

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Debug("bootstrap-sshd: accept error", "error", err)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
	wg.Wait()
	slog.Info("bootstrap-sshd: stopped")
	return nil
}

// Addr returns the listener address, or empty if not yet listening.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

func (s *Server) handleConn(ctx context.Context, netConn net.Conn) {
	defer netConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		slog.Debug("bootstrap-sshd: handshake failed", "error", err)
		return
	}
	defer sshConn.Close()
	slog.Info("bootstrap-sshd: connection", "user", sshConn.User(), "remote", sshConn.RemoteAddr())

	go func() {
		<-ctx.Done()
		_ = sshConn.Close()
	}()

	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			slog.Debug("bootstrap-sshd: channel accept error", "error", err)
			continue
		}
		go handleSession(ctx, ch, requests)
	}
}

func handleSession(ctx context.Context, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "exec":
			if len(req.Payload) < 4 {
				_ = req.Reply(false, nil)
				continue
			}
			cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
			if cmdLen+4 > len(req.Payload) {
				_ = req.Reply(false, nil)
				continue
			}
			command := string(req.Payload[4 : 4+cmdLen])
			_ = req.Reply(true, nil)

			exitCode := runCommand(ctx, ch, command)

			// Signal EOF on stdout so the client's io.ReadAll returns,
			// then send exit-status before closing the channel.
			_ = ch.CloseWrite()
			exitMsg := ssh.Marshal(struct{ Status uint32 }{uint32(exitCode)})
			_, _ = ch.SendRequest("exit-status", false, exitMsg)
			return

		case "env":
			_ = req.Reply(true, nil)

		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func runCommand(ctx context.Context, ch ssh.Channel, command string) int {
	slog.Debug("bootstrap-sshd: exec", "command", command)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	cmd.Stdout = ch
	cmd.Stderr = ch.Stderr()

	// Use StdinPipe so cmd.Run returns promptly when the process exits,
	// rather than waiting for the SSH channel's read side to close.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		slog.Debug("bootstrap-sshd: stdin pipe error", "error", err)
		_, _ = fmt.Fprintf(ch.Stderr(), "exec error: %v\n", err)
		return 1
	}
	go func() {
		_, _ = io.Copy(stdin, ch)
		_ = stdin.Close()
	}()

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		slog.Debug("bootstrap-sshd: exec error", "error", err)
		_, _ = fmt.Fprintf(ch.Stderr(), "exec error: %v\n", err)
		return 1
	}
	return 0
}

// GenerateHostKey creates an ed25519 private key and writes it in
// OpenSSH PEM format to path. Returns the public key fingerprint.
func GenerateHostKey(path string) (string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generating key: %w", err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return "", fmt.Errorf("creating signer: %w", err)
	}

	privBytes, err := ssh.MarshalPrivateKey(priv, "jumpgate bootstrap host key")
	if err != nil {
		return "", fmt.Errorf("marshalling private key: %w", err)
	}

	if err := os.WriteFile(path, pem.EncodeToMemory(privBytes), 0600); err != nil {
		return "", fmt.Errorf("writing host key: %w", err)
	}

	_ = pub // used indirectly via signer
	return ssh.FingerprintSHA256(signer.PublicKey()), nil
}
