package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
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
func RunRelayForeground(ctx context.Context, host string) error {
	args := []string{"-N", host}
	slog.Debug("ssh", "op", "relay-foreground", "args", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh relay: %w", err)
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
