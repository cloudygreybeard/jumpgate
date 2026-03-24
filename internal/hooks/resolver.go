package hooks

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Resolve finds the hook script path using the resolution order:
// 1. CONFIG_DIR/contexts/<CONTEXT>/hooks/<hook-name>
// 2. CONFIG_DIR/hooks/<hook-name>
// Returns ("", nil) if not found.
func Resolve(configDir, contextName, hookName string) (string, error) {
	contextPath := filepath.Join(configDir, "contexts", contextName, "hooks", hookName)
	if isExecutable(contextPath) {
		slog.Debug("hook resolved", "hook", hookName, "path", contextPath, "scope", "context")
		return contextPath, nil
	}

	globalPath := filepath.Join(configDir, "hooks", hookName)
	if isExecutable(globalPath) {
		slog.Debug("hook resolved", "hook", hookName, "path", globalPath, "scope", "global")
		return globalPath, nil
	}

	slog.Debug("hook not found", "hook", hookName)
	return "", nil
}

// ResolveRequired finds a hook that must exist, returning an error if missing.
func ResolveRequired(configDir, contextName, hookName string) (string, error) {
	path, err := Resolve(configDir, contextName, hookName)
	if err != nil {
		return "", err
	}
	if path == "" {
		contextPath := filepath.Join(configDir, "contexts", contextName, "hooks", hookName)
		globalPath := filepath.Join(configDir, "hooks", hookName)
		return "", fmt.Errorf("required hook %q not found\n  Expected: %s\n       or:  %s\n  Run: jumpgate setup config   (installs example hooks)",
			hookName, contextPath, globalPath)
	}
	return path, nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0111 != 0
}
