package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func OpenControlMaster(ctx context.Context, host string, extraArgs ...string) error {
	args := []string{"-o", "ControlPersist=4h", "-N", "-f"}
	args = append(args, extraArgs...)
	args = append(args, host)
	return runSSH(ctx, "open-controlmaster", args...)
}

func OpenRelay(ctx context.Context, host, socketPath string) error {
	if runtime.GOOS == "windows" {
		return openRelayWindows(ctx, host)
	}
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

// openRelayWindows launches the relay SSH process in the background without
// ControlMaster or -f, neither of which Windows OpenSSH supports.
func openRelayWindows(ctx context.Context, host string) error {
	args := []string{"-N", host}
	slog.Debug("ssh", "op", "open-relay-win", "args", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh open-relay-win: %w", err)
	}
	RelayPID = cmd.Process.Pid
	slog.Debug("relay process started", "pid", RelayPID)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		RelayPID = 0
	}()
	relayExited = done
	return nil
}

// RelayPID holds the PID of the background relay process on Windows.
var RelayPID int

// relayExited receives the exit error when the background relay process ends.
var relayExited <-chan error

// RelayExited returns a channel that receives when the relay process exits.
// Returns nil if no relay has been started.
func RelayExited() <-chan error { return relayExited }

func Check(ctx context.Context, host string) error {
	return runSSHQuiet(ctx, "check", "-O", "check", host)
}

func CheckSocket(ctx context.Context, host, socketPath string) error {
	if runtime.GOOS == "windows" {
		if RelayPID == 0 {
			return fmt.Errorf("relay process not running")
		}
		proc, err := os.FindProcess(RelayPID)
		if err != nil {
			return fmt.Errorf("relay process %d not found", RelayPID)
		}
		_ = proc.Release()
		return nil
	}
	return runSSHQuiet(ctx, "check-socket", "-o", "ControlPath="+socketPath, "-O", "check", host)
}

func Exit(ctx context.Context, host string) error {
	return runSSH(ctx, "exit", "-O", "exit", host)
}

func ExitSocket(ctx context.Context, host, socketPath string) error {
	if runtime.GOOS == "windows" {
		return exitRelayWindows()
	}
	return runSSH(ctx, "exit-socket", "-o", "ControlPath="+socketPath, "-O", "exit", host)
}

func exitRelayWindows() error {
	if RelayPID == 0 {
		return nil
	}
	proc, err := os.FindProcess(RelayPID)
	if err != nil {
		return nil
	}
	slog.Debug("killing relay process", "pid", RelayPID)
	err = proc.Kill()
	RelayPID = 0
	return err
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
