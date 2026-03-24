package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// CheckPortAvailable probes the gate host to see whether the given TCP port
// is already bound. Returns nil when the port is free, an error when it is
// occupied or the check itself fails.
func CheckPortAvailable(ctx context.Context, gateHost string, port int) error {
	pattern := fmt.Sprintf(":%d", port)

	cmd := exec.CommandContext(ctx, "ssh", gateHost, "ss", "-tln")
	slog.Debug("ssh", "op", "port-check", "host", gateHost, "port", port)

	out, err := cmd.Output()
	if err != nil {
		slog.Debug("port check: could not run ss on gate, skipping", "error", err)
		return nil
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, pattern) {
			return fmt.Errorf("port %d in use on gate", port)
		}
	}
	return nil
}
