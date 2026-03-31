package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

func OpenControlMaster(ctx context.Context, host string, extraArgs ...string) error {
	args := []string{"-o", "ControlPersist=4h", "-N", "-f"}
	args = append(args, extraArgs...)
	args = append(args, host)
	return runSSH(ctx, "open-controlmaster", args...)
}

// RunRelayForeground runs the relay SSH session in the foreground (blocking).
// Used on Windows where ControlMaster is unavailable and every SSH command
// requires separate authentication, so we run a single clean session.
// relayPort is passed explicitly via -R so the tunnel works even when the
// SSH config was generated before the port was auto-assigned.
// localPort is the target port on the local side (22 for sshd, 2222 for
// the embedded bootstrap SSH server).
func RunRelayForeground(ctx context.Context, host string, relayPort, localPort int) error {
	args := []string{
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	}
	if relayPort > 0 {
		args = append(args, "-R", fmt.Sprintf("%d:127.0.0.1:%d", relayPort, localPort))
	}
	args = append(args, host)
	slog.Debug("ssh", "op", "relay-foreground", "args", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh relay start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// First tick at 10s to confirm auth succeeded, then every 30s.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	start := time.Now()
	firstTick := true

	for {
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("ssh relay: %w", err)
			}
			return nil
		case <-ticker.C:
			uptime := time.Since(start).Truncate(time.Second)
			fmt.Fprintf(os.Stderr, "  relay: alive (%s)\n", uptime)
			if firstTick {
				ticker.Reset(30 * time.Second)
				firstTick = false
			}
		case <-ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			return nil
		}
	}
}

func OpenRelay(ctx context.Context, host, socketPath string) error {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale socket: %w", err)
	}
	args := []string{
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + socketPath,
		"-o", "ControlPersist=yes",
		"-f", "-N",
		host,
	}
	return runSSH(ctx, "open-relay", args...)
}

func Check(ctx context.Context, host string) error {
	return runSSHQuiet(ctx, "check", "-O", "check", host)
}

func CheckSocket(ctx context.Context, host, socketPath string) error {
	return runSSHQuiet(ctx, "check-socket", "-o", "ControlPath="+socketPath, "-O", "check", host)
}

func Exit(ctx context.Context, host string) error {
	return runSSH(ctx, "exit", "-O", "exit", host)
}

func ExitSocket(ctx context.Context, host, socketPath string) error {
	return runSSH(ctx, "exit-socket", "-o", "ControlPath="+socketPath, "-O", "exit", host)
}

func Forward(ctx context.Context, host, spec string) error {
	return runSSH(ctx, "forward", "-O", "forward", "-L", spec, host)
}

func CancelForward(ctx context.Context, host, spec string) error {
	return runSSH(ctx, "cancel-forward", "-O", "cancel", "-L", spec, host)
}

func runSSH(ctx context.Context, label string, args ...string) error {
	slog.Debug("ssh", "op", label, "args", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh %s: %w", label, err)
	}
	return nil
}

// runSSHQuiet runs ssh without printing stdout/stderr (for check operations).
func runSSHQuiet(ctx context.Context, label string, args ...string) error {
	slog.Debug("ssh", "op", label, "args", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh %s: %w", label, err)
	}
	return nil
}
