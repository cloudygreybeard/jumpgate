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
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/cloudygreybeard/jumpgate/internal/transfer"
	"golang.org/x/crypto/ssh"
)

// bundleCommandPrefix is the internal command prefix used to receive a
// tar.gz archive over stdin and extract it to a target directory. Handled
// entirely within the embedded server — no subprocess needed.
const bundleCommandPrefix = "__jumpgate_receive_bundle"

// shutdownCommand is sent by the local bootstrapper after setup remote
// completes. The server handles it by closing the listener and exiting.
const shutdownCommand = "__jumpgate_bootstrap_done"

// ShutdownCommand returns the command string that triggers server exit.
func ShutdownCommand() string { return shutdownCommand }

// BundleCommand returns the command string for receiving a bundle.
func BundleCommand() string { return bundleCommandPrefix }

// defaultAllowedCommands is the day-0 command allowlist for bootstrap exec.
// Only the basename of the first token in the command is checked.
var defaultAllowedCommands = map[string]bool{
	bundleCommandPrefix: true,
	shutdownCommand:     true,
	"cat":               true,
	"chmod":             true,
	"command":           true,
	"cp":                true,
	"echo":              true,
	"hostname":          true,
	"id":                true,
	"jumpgate":          true,
	"jumpgate.exe":      true,
	"ls":                true,
	"mkdir":             true,
	"powershell.exe":    true,
	"rm":                true,
	"scp":               true,
	"scp.exe":           true,
	"test":              true,
	"true":              true,
	"uname":             true,
	"whoami":            true,
	"wsl.exe":           true,
}

// commandAllowed checks whether the first token (basename) of the
// command is in the allowlist. This prevents arbitrary code execution
// while permitting the bootstrap workflow commands.
func commandAllowed(command string) bool {
	first := strings.TrimSpace(command)
	if idx := strings.IndexAny(first, " \t"); idx >= 0 {
		first = first[:idx]
	}
	base := filepath.Base(first)
	return defaultAllowedCommands[base]
}

// BannerPrefix is the prefix of the SSH server version string. The full
// banner includes the context UID so the local probe can verify it is
// connecting to the correct remote (e.g. "SSH-2.0-jumpgate-bootstrap_<uid>").
const BannerPrefix = "jumpgate-bootstrap"

// Banner returns the full SSH server version string for a given context UID.
func Banner(uid string) string {
	if uid == "" {
		return "SSH-2.0-" + BannerPrefix
	}
	return "SSH-2.0-" + BannerPrefix + "_" + uid
}

// Server is a minimal SSH server for bootstrap. It listens on a local
// port, authenticates with a single authorized public key, and handles
// exec requests by spawning the platform shell.
type Server struct {
	config         *ssh.ServerConfig
	addr           string
	mu             sync.Mutex
	listener       net.Listener
	fingerprint    string
	authKeyType    string
	authKeyComment string
	shutdownOnce   sync.Once
	shutdownCh     chan struct{}
}

// New creates a Server that listens on addr, authenticates against the
// public key in authorizedKeyPath, and presents the host key from
// hostKeyPath. The contextUID is included in the SSH banner so the local
// probe can verify it is connecting to the correct remote.
func New(hostKeyPath, authorizedKeyPath, addr, contextUID string) (*Server, error) {
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
		ServerVersion: Banner(contextUID),
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
		shutdownCh:     make(chan struct{}),
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
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

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

// ShutdownCh returns a channel that is closed when the server receives
// the shutdown command from the local bootstrapper. The caller (bootstrap
// cmd) should select on this to stop the relay SSH process.
func (s *Server) ShutdownCh() <-chan struct{} {
	return s.shutdownCh
}

func (s *Server) requestShutdown() {
	s.shutdownOnce.Do(func() {
		slog.Info("bootstrap-sshd: shutdown requested by remote setup completion")
		close(s.shutdownCh)
	})
}

// Addr returns the listener address, or empty if not yet listening.
func (s *Server) Addr() string {
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if ln != nil {
		return ln.Addr().String()
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
		go s.handleSession(ctx, ch, requests)
	}
}

func (s *Server) handleSession(ctx context.Context, ch ssh.Channel, reqs <-chan *ssh.Request) {
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

			exitCode := s.runCommand(ctx, ch, command)

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

func (s *Server) runCommand(ctx context.Context, ch ssh.Channel, command string) int {
	if !commandAllowed(command) {
		slog.Warn("bootstrap-sshd: blocked command (not in allowlist)", "command", command)
		_, _ = fmt.Fprintf(ch.Stderr(), "jumpgate bootstrap: command not permitted: %s\n", strings.SplitN(command, " ", 2)[0])
		return 126
	}
	slog.Info("bootstrap-sshd: exec", "command", command)

	if command == shutdownCommand {
		_, _ = fmt.Fprintln(ch, "bootstrap server shutting down")
		s.requestShutdown()
		return 0
	}

	if strings.HasPrefix(command, bundleCommandPrefix) {
		return handleReceiveBundle(ch, command)
	}

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

// handleReceiveBundle extracts a tar.gz archive from the channel's stdin
// into baseDir. The command format is: __jumpgate_receive_bundle <base_dir>
// All extraction happens in-process — no external tar binary needed.
func handleReceiveBundle(ch ssh.Channel, command string) int {
	parts := strings.SplitN(command, " ", 2)
	baseDir := ""
	if len(parts) > 1 {
		baseDir = strings.TrimSpace(parts[1])
	}
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			_, _ = fmt.Fprintf(ch.Stderr(), "cannot determine home directory: %v\n", err)
			return 1
		}
		baseDir = home
	}

	count, err := transfer.ExtractBundle(ch, baseDir)
	if err != nil {
		slog.Warn("bootstrap-sshd: bundle extraction failed", "error", err, "base_dir", baseDir)
		_, _ = fmt.Fprintf(ch.Stderr(), "bundle extraction failed: %v\n", err)
		return 1
	}

	slog.Info("bootstrap-sshd: bundle extracted", "files", count, "base_dir", baseDir)
	_, _ = fmt.Fprintf(ch, "extracted %d files\n", count)
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
