package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
)

const markerDir = "~/.jumpgate"

func markerPath(contextUID string) string {
	return fmt.Sprintf("%s/relay-%s.port", markerDir, contextUID)
}

// WriteRelayMarker writes the relay port to a marker file on the gate host.
// The contextUID should be the context's stable UID to avoid leaking names on shared hosts.
func WriteRelayMarker(ctx context.Context, gateHost, contextUID string, port int) error {
	cmd := fmt.Sprintf("mkdir -p %s && echo %d > %s", markerDir, port, markerPath(contextUID))
	slog.Debug("relay-marker", "op", "write", "host", gateHost, "port", port)

	c := exec.CommandContext(ctx, "ssh", gateHost, cmd)
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("writing relay marker on gate: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ReadRelayMarker reads the relay port from a marker file on the gate host.
// Returns 0, nil if the file does not exist or cannot be read.
func ReadRelayMarker(ctx context.Context, gateHost, contextUID string) (int, error) {
	cmd := fmt.Sprintf("cat %s 2>/dev/null || true", markerPath(contextUID))
	slog.Debug("relay-marker", "op", "read", "host", gateHost)

	c := exec.CommandContext(ctx, "ssh", gateHost, cmd)
	out, err := c.Output()
	if err != nil {
		slog.Debug("relay-marker", "op", "read", "error", err)
		return 0, nil
	}

	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, nil
	}

	port, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid port in relay marker: %q", s)
	}
	return port, nil
}

// RemoveRelayMarker removes the marker file from the gate host.
func RemoveRelayMarker(ctx context.Context, gateHost, contextUID string) error {
	cmd := fmt.Sprintf("rm -f %s", markerPath(contextUID))
	slog.Debug("relay-marker", "op", "remove", "host", gateHost)

	c := exec.CommandContext(ctx, "ssh", gateHost, cmd)
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("removing relay marker on gate: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
