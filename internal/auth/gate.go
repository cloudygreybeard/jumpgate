package auth

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/hooks"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
)

func EnsureGate(ctx context.Context, rc *config.ResolvedContext) error {
	if rc.Derived.GateSocket != "" && socketExists(rc.Derived.GateSocket) {
		fmt.Printf("Gate [%s]: already connected\n", rc.Name)
		return nil
	}

	if !hooks.RunOptionalCheck(ctx, rc, "check-credentials") {
		fmt.Println("Credentials missing -- running setup...")
		_, err := hooks.RunOptional(ctx, rc, "setup-credentials")
		if err != nil {
			return fmt.Errorf("setup-credentials: %w", err)
		}
	}

	fmt.Printf("Gate [%s]: opening session...\n", rc.Name)

	token, err := hooks.RunRequired(ctx, rc, "get-gate-token")
	if err != nil {
		return fmt.Errorf("get-gate-token: %w", err)
	}

	if err := openGateSession(ctx, rc, token); err != nil {
		return err
	}

	fmt.Printf("Gate [%s]: connected\n", rc.Name)
	return nil
}

func openGateSession(ctx context.Context, rc *config.ResolvedContext, token string) error {
	// Write a temp shell script for SSH_ASKPASS instead of a symlink.
	// The token stays in the environment, not in the script itself.
	tmpDir, err := os.MkdirTemp("", "jumpgate-askpass-")
	if err != nil {
		return fmt.Errorf("creating temp dir for askpass: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Chmod(tmpDir, 0700); err != nil {
		return fmt.Errorf("securing askpass dir: %w", err)
	}

	askpassPath := filepath.Join(tmpDir, "askpass")
	script := "#!/bin/sh\nprintf '%s' \"$JUMPGATE_ASKPASS_TOKEN\"\n"
	if err := os.WriteFile(askpassPath, []byte(script), 0700); err != nil {
		return fmt.Errorf("writing askpass script: %w", err)
	}

	args := []string{"-o", "ControlPersist=4h", "-N", "-f", rc.Derived.GateHost}
	slog.Debug("opening gate", "host", rc.Derived.GateHost, "askpass", askpassPath)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Env = append(os.Environ(),
		"SSH_ASKPASS="+askpassPath,
		"SSH_ASKPASS_REQUIRE=force",
		"JUMPGATE_ASKPASS_TOKEN="+token,
	)

	if os.Getenv("DISPLAY") == "" {
		cmd.Env = append(cmd.Env, "DISPLAY=:0")
	}

	cmd.Stdout = os.Stdout
	cmd.Stdin = nil

	var stderr strings.Builder
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if strings.Contains(msg, "Permission denied") {
			return fmt.Errorf("gate authentication failed (token may have expired) -- retry: jumpgate connect")
		}
		return fmt.Errorf("gate session: %w", err)
	}
	return nil
}

func socketExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Type()&os.ModeSocket != 0
}

func CloseGate(ctx context.Context, rc *config.ResolvedContext) {
	_, _ = hooks.RunOptional(ctx, rc, "pre-disconnect")

	if rc.Derived.GateSocket != "" && socketExists(rc.Derived.GateSocket) {
		if err := internalssh.Exit(ctx, rc.Derived.GateHost); err != nil {
			fmt.Printf("Gate [%s]: session already gone\n", rc.Name)
		} else {
			fmt.Printf("Gate [%s]: session closed\n", rc.Name)
		}
	} else {
		fmt.Printf("Gate [%s]: not connected\n", rc.Name)
	}

	destroyTicket(ctx, rc.Context.Auth.CCFile)

	_, _ = hooks.RunOptional(ctx, rc, "post-disconnect")
}

func destroyTicket(ctx context.Context, ccFile string) {
	cmd := exec.CommandContext(ctx, "kdestroy")
	if ccFile != "" {
		cmd.Env = append(os.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}
	if err := cmd.Run(); err != nil {
		fmt.Println("Auth: no ticket to destroy")
	} else {
		fmt.Println("Auth: ticket destroyed")
	}
}
