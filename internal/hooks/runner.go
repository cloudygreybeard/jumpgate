package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func RunRequired(ctx context.Context, rc *config.ResolvedContext, hookName string) (string, error) {
	path, err := ResolveRequired(rc.Derived.ConfigDir, rc.Name, hookName)
	if err != nil {
		return "", err
	}
	return runCapture(ctx, path, BuildEnv(rc))
}

func RunOptional(ctx context.Context, rc *config.ResolvedContext, hookName string) (bool, error) {
	path, err := Resolve(rc.Derived.ConfigDir, rc.Name, hookName)
	if err != nil {
		return false, err
	}
	if path == "" {
		slog.Debug("optional hook skipped (not found)", "hook", hookName)
		return true, nil
	}
	return false, runPassthrough(ctx, path, BuildEnv(rc))
}

func RunOptionalCheck(ctx context.Context, rc *config.ResolvedContext, hookName string) bool {
	path, err := Resolve(rc.Derived.ConfigDir, rc.Name, hookName)
	if err != nil || path == "" {
		return true
	}
	err = runPassthrough(ctx, path, BuildEnv(rc))
	return err == nil
}

func runCapture(ctx context.Context, path string, env []string) (string, error) {
	slog.Info("running hook", "path", path)
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = env
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hook %s: %w", path, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func runPassthrough(ctx context.Context, path string, env []string) error {
	slog.Info("running hook", "path", path)
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
