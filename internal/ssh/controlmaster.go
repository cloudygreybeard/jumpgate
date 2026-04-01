package ssh

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// relaySentinel is echoed by the remote command after SSH authentication
// and RemoteForward both succeed. With ExitOnForwardFailure=yes, SSH exits
// before running the command if forwarding fails, so receiving this string
// on stdout is positive confirmation that the relay tunnel is alive.
const relaySentinel = "__JUMPGATE_RELAY_OK__"

func OpenControlMaster(ctx context.Context, host string, extraArgs ...string) error {
	args := []string{"-o", "ControlPersist=4h", "-N", "-f"}
	args = append(args, extraArgs...)
	args = append(args, host)
	return runSSH(ctx, "open-controlmaster", args...)
}

// RunRelayForeground runs the relay SSH session in the foreground (blocking).
// Used on Windows where ControlMaster is unavailable and every SSH command
// requires separate authentication, so we run a single clean session.
//
// Instead of -N (no remote command), a sentinel command is run on the
// bastion: "echo <sentinel>; cat > /dev/null". With ExitOnForwardFailure=yes,
// SSH exits before running the command if forwarding fails. Therefore,
// receiving the sentinel on stdout is positive confirmation that
// authentication succeeded AND the RemoteForward port is bound.
//
// If the bastion restricts command execution (ForceCommand), the sentinel
// won't arrive and we fall back to assuming alive after 60 seconds.
func RunRelayForeground(ctx context.Context, host string, relayPort, localPort int) error {
	args := []string{
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	}
	if relayPort > 0 {
		args = append(args, "-R", fmt.Sprintf("%d:127.0.0.1:%d", relayPort, localPort))
	}
	args = append(args, host,
		fmt.Sprintf("echo %s; cat > /dev/null", relaySentinel))

	slog.Debug("ssh", "op", "relay-foreground", "args", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh relay start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	relayConfirmed := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			if scanner.Text() == relaySentinel {
				close(relayConfirmed)
				break
			}
			fmt.Fprintln(os.Stdout, scanner.Text())
		}
		_, _ = io.Copy(io.Discard, stdoutPipe)
	}()

	start := time.Now()

	// Phase 1: wait for positive confirmation that auth + forwarding worked.
	fallback := time.NewTimer(60 * time.Second)
	select {
	case <-relayConfirmed:
		fallback.Stop()
		uptime := time.Since(start).Truncate(time.Second)
		fmt.Fprintf(os.Stderr, "  relay: alive (%s)\n", uptime)
	case <-fallback.C:
		slog.Debug("relay sentinel not received, assuming alive (ForceCommand?)")
		uptime := time.Since(start).Truncate(time.Second)
		fmt.Fprintf(os.Stderr, "  relay: alive (%s)\n", uptime)
	case err := <-done:
		fallback.Stop()
		if err != nil {
			return fmt.Errorf("ssh relay: %w", err)
		}
		return nil
	case <-ctx.Done():
		fallback.Stop()
		return killRelay(cmd, done)
	}

	// Phase 2: periodic heartbeat — SSH is confirmed alive.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

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
		case <-ctx.Done():
			return killRelay(cmd, done)
		}
	}
}

func killRelay(cmd *exec.Cmd, done <-chan error) error {
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	return nil
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
