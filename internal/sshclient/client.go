// Package sshclient provides a native Go SSH client that connects to the
// remote bootstrap server through the gate's ControlMaster via ssh -W.
// This replaces external scp/ssh calls for file transfer, reducing
// per-operation overhead from ~20s (full SSH handshake each time) to a
// single persistent connection.
package sshclient

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// proxyConn wraps an ssh -W subprocess as a net.Conn.
type proxyConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
	closed bool
}

func (c *proxyConn) Read(b []byte) (int, error)  { return c.stdout.Read(b) }
func (c *proxyConn) Write(b []byte) (int, error) { return c.stdin.Write(b) }
func (c *proxyConn) LocalAddr() net.Addr          { return &net.TCPAddr{} }
func (c *proxyConn) RemoteAddr() net.Addr         { return &net.TCPAddr{} }

func (c *proxyConn) SetDeadline(_ time.Time) error      { return nil }
func (c *proxyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *proxyConn) SetWriteDeadline(_ time.Time) error { return nil }

func (c *proxyConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	return c.cmd.Process.Kill()
}

// Client holds an SSH connection to the remote bootstrap server.
type Client struct {
	conn  *ssh.Client
	proxy *proxyConn
}

// Dial connects to the remote bootstrap server at host:port through the
// gate's ControlMaster using ssh -W. Authentication uses the provided
// private key (the same key whose public half is in the remote's
// authorized_key file).
func Dial(ctx context.Context, gateHost string, remotePort int, keyPath string) (*Client, error) {
	target := fmt.Sprintf("localhost:%d", remotePort)

	proxyArgs := []string{
		"-W", target,
		"-o", "UserKnownHostsFile=" + internalssh.KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		gateHost,
	}
	slog.Debug("sshclient: proxy", "args", strings.Join(proxyArgs, " "))

	cmd := exec.CommandContext(ctx, "ssh", proxyArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ssh -W start: %w", err)
	}

	proxy := &proxyConn{cmd: cmd, stdin: stdin, stdout: stdout}

	authMethods, err := buildAuthMethods(keyPath)
	if err != nil {
		proxy.Close()
		return nil, err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(proxy, target, &ssh.ClientConfig{
		User:            remoteUser(),
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		proxy.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	return &Client{conn: client, proxy: proxy}, nil
}

// Exec runs a command on the remote and returns its combined output.
func (c *Client) Exec(command string) ([]byte, error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	return sess.CombinedOutput(command)
}

// ExecStream runs a command on the remote, pipes stdinData to its stdin,
// and returns stdout and stderr contents.
func (c *Client) ExecStream(command string, stdinData io.Reader) (stdout, stderr []byte, err error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer sess.Close()

	sess.Stdin = stdinData

	var stdoutBuf, stderrBuf strings.Builder
	sess.Stdout = &stdoutBuf
	sess.Stderr = &stderrBuf

	err = sess.Run(command)
	return []byte(stdoutBuf.String()), []byte(stderrBuf.String()), err
}

// Close shuts down the SSH connection and the proxy subprocess.
func (c *Client) Close() error {
	err := c.conn.Close()
	_ = c.proxy.Close()
	return err
}

// buildAuthMethods returns SSH auth methods. It prefers the SSH agent
// (handles passphrase-protected keys transparently) and falls back to
// reading the key file directly for unprotected keys.
func buildAuthMethods(keyPath string) ([]ssh.AuthMethod, error) {
	// Try SSH agent first
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			ag := agent.NewClient(conn)
			slog.Debug("sshclient: using SSH agent for auth")
			return []ssh.AuthMethod{ssh.PublicKeysCallback(ag.Signers)}, nil
		}
		slog.Debug("sshclient: agent dial failed, trying key file", "error", err)
	}

	// Fall back to reading key directly
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading key %s: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing key (is it passphrase-protected? load it into ssh-agent): %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

func remoteUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "bootstrap"
}
