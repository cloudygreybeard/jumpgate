package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

// KnownHostsFile returns the path to jumpgate's dedicated known_hosts file.
// Relay connections use this instead of ~/.ssh/known_hosts so that jumpgate
// never touches the user's global SSH trust store.
func KnownHostsFile() string {
	return filepath.Join(config.DefaultConfigDir(), "known_hosts")
}

// ClearStaleHostKey removes a known_hosts entry for [localhost]:port from
// jumpgate's own known_hosts file. Called only during explicit reset
// operations (bootstrap --reinit) — not on every connection.
func ClearStaleHostKey(port int) {
	if port <= 0 {
		return
	}
	target := fmt.Sprintf("[localhost]:%d", port)
	cmd := exec.Command("ssh-keygen", "-R", target, "-f", KnownHostsFile())
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		slog.Debug("ssh-keygen -R (no-op if no entry existed)", "target", target, "error", err)
	} else {
		slog.Debug("cleared stale known_hosts entry", "target", target, "file", KnownHostsFile())
	}
}

// relaySSHOptions returns SSH options that direct host key checks to
// jumpgate's own known_hosts file with accept-new semantics.
func relaySSHOptions() []string {
	return []string{
		"-o", "UserKnownHostsFile=" + KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
	}
}

// ProbeResult describes why a probe failed (if it did).
type ProbeResult struct {
	Reachable bool
	// Detail is set for actionable errors the user should see immediately
	// rather than polling through. Empty means "not reachable yet, keep trying."
	Detail string
}

func Probe(ctx context.Context, host, ccFile string) ProbeResult {
	args := []string{
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
	}
	args = append(args, relaySSHOptions()...)
	args = append(args, host, "true")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if ccFile != "" {
		cmd.Env = append(cmd.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	slog.Debug("probe", "host", host, "reachable", err == nil)
	if err == nil {
		return ProbeResult{Reachable: true}
	}

	out := stderr.String()
	switch {
	case strings.Contains(out, "REMOTE HOST IDENTIFICATION HAS CHANGED") ||
		strings.Contains(out, "Host key verification failed"):
		return ProbeResult{Detail: "host-key-changed"}
	case strings.Contains(out, "Permission denied"):
		return ProbeResult{Detail: "permission-denied"}
	case strings.Contains(out, "Connection refused") ||
		strings.Contains(out, "Connection timed out") ||
		strings.Contains(out, "No route to host") ||
		strings.Contains(out, "connect to host") ||
		ctx.Err() != nil:
		return ProbeResult{} // not reachable yet, keep polling
	default:
		slog.Debug("probe stderr", "output", out)
		return ProbeResult{}
	}
}

func ProbeHostname(ctx context.Context, host, ccFile string) (string, bool) {
	args := []string{
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
	}
	args = append(args, relaySSHOptions()...)
	args = append(args, host, "hostname")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if ccFile != "" {
		cmd.Env = append(cmd.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
