package connect

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/auth"
	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/hooks"
	"github.com/cloudygreybeard/jumpgate/internal/setup"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
)

const DefaultPollTimeout = 5 * time.Minute

func Connect(ctx context.Context, rc *config.ResolvedContext, cfg *config.Config) error {
	if rc.IsLocal() {
		return connectLocal(ctx, rc, cfg)
	}
	return connectRemote(ctx, rc, cfg)
}

// pollDelay returns a capped exponential backoff duration.
// Sequence: 2s, 4s, 8s, 16s, 30s, 30s, ...
func pollDelay(attempt int) time.Duration {
	const base = 2 * time.Second
	const cap = 30 * time.Second
	shift := attempt
	if shift > 4 {
		shift = 4
	}
	d := base << uint(shift)
	if d > cap {
		return cap
	}
	return d
}

func connectLocal(ctx context.Context, rc *config.ResolvedContext, cfg *config.Config) error {
	// Auto-regenerate SSH config if stale
	if cfg != nil {
		ensureSSHConfig(rc, cfg)
	}

	if err := auth.EnsureGate(ctx, rc); err != nil {
		return err
	}

	if err := auth.EnsureKerberos(ctx, rc); err != nil {
		return err
	}

	_, _ = hooks.RunOptional(ctx, rc, "load-ssh-key")

	if updated := discoverRelayPort(ctx, rc, cfg); updated {
		ensureSSHConfig(rc, cfg)
	}

	fmt.Println()

	ccFile := rc.Context.Auth.CCFile
	remoteHost := rc.Derived.RemoteHost

	if internalssh.Probe(ctx, remoteHost, ccFile) {
		fmt.Printf("--- remote [%s] reachable ---\n", rc.Name)
		_, _ = hooks.RunOptional(ctx, rc, "on-connect")
		return nil
	}

	fmt.Printf("Remote [%s]: not yet reachable -- waiting...\n", rc.Name)
	_, _ = hooks.RunOptional(ctx, rc, "pre-poll")
	fmt.Printf("  Polling with backoff (2s..30s) for up to %ds (Ctrl-C to stop).\n",
		int(DefaultPollTimeout.Seconds()))
	fmt.Println()

	pollCtx, pollCancel := context.WithTimeout(ctx, DefaultPollTimeout)
	defer pollCancel()

	attempt := 0
	for {
		// Circuit breaker: check gate liveness before probing remote
		if rc.Derived.GateSocket != "" && !socketExists(rc.Derived.GateSocket) {
			return fmt.Errorf("gate session lost during polling -- reconnect with: jumpgate connect")
		}
		if err := internalssh.Check(ctx, rc.Derived.GateHost); err != nil {
			return fmt.Errorf("gate session lost during polling -- reconnect with: jumpgate connect")
		}

		_, _ = hooks.RunOptional(pollCtx, rc, "on-poll-tick")

		if internalssh.Probe(pollCtx, remoteHost, ccFile) {
			fmt.Println()
			fmt.Printf("--- remote [%s] reachable ---\n", rc.Name)
			_, _ = hooks.RunOptional(ctx, rc, "on-connect")
			return nil
		}

		delay := pollDelay(attempt)
		fmt.Printf("\r  waiting... attempt %d (next in %s)", attempt+1, delay)

		timer := time.NewTimer(delay)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			fmt.Println()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("timed out waiting for remote (%ds)", int(DefaultPollTimeout.Seconds()))
		case <-timer.C:
		}
		attempt++
	}
}

func connectRemote(ctx context.Context, rc *config.ResolvedContext, cfg *config.Config) error {
	if rc.Context.Relay.RemotePort == 0 {
		port := rand.Intn(16384) + 49152
		rc.Context.Relay.RemotePort = port
		fmt.Printf("Relay [%s]: auto-generated port %d\n", rc.Name, port)

		if err := persistRelayPort(rc); err != nil {
			slog.Warn("could not persist relay port to config", "error", err)
		}

		if cfg != nil {
			if ctxCfg, ok := cfg.Contexts[rc.Name]; ok {
				ctxCfg.Relay.RemotePort = port
				cfg.Contexts[rc.Name] = ctxCfg
			}
			ensureSSHConfig(rc, cfg)
		}
	}

	if runtime.GOOS == "windows" {
		return connectRemoteWindows(ctx, rc)
	}
	return connectRemoteUnix(ctx, rc)
}

// connectRemoteWindows runs the relay as a single foreground SSH session.
// Without ControlMaster, every SSH command requires separate authentication,
// so we skip the port check and marker write and run one clean session.
// Uses the gate host (not relay alias) to avoid a double RemoteForward: the
// relay alias already includes a RemoteForward directive in the SSH config,
// so passing -R as well would cause a duplicate port bind failure.
func connectRemoteWindows(ctx context.Context, rc *config.ResolvedContext) error {
	gateHost := rc.Derived.GateHost
	relayPort := rc.Context.Relay.RemotePort

	fmt.Printf("Relay [%s]: connecting via %s (RemoteForward %d -> localhost:22)...\n",
		rc.Name, gateHost, relayPort)
	fmt.Println("  (foreground session — Ctrl+C to close)")

	err := internalssh.RunRelayForeground(ctx, gateHost, relayPort, 22)
	if err != nil && ctx.Err() == nil {
		fmt.Println()
		fmt.Printf("If relay port %d is stale from a previous session, wait 60-120s and retry,\n", relayPort)
		fmt.Printf("or use: jumpgate connect --relay-port %d\n", randomRelayPort())
	}
	return err
}

func randomRelayPort() int {
	return rand.Intn(16384) + 49152
}

func connectRemoteUnix(ctx context.Context, rc *config.ResolvedContext) error {
	socketPath := rc.Derived.RelaySocket
	relayHost := rc.Derived.RelayHost

	if socketExists(socketPath) {
		if err := internalssh.CheckSocket(ctx, relayHost, socketPath); err == nil {
			fmt.Printf("Relay [%s]: already active\n", rc.Name)
			return nil
		}
	}

	gateHost := rc.Derived.GateHost
	if err := internalssh.CheckPortAvailable(ctx, gateHost, rc.Context.Relay.RemotePort); err != nil {
		return fmt.Errorf("relay port %d is already in use on the gate -- use --relay-port to try a different port", rc.Context.Relay.RemotePort)
	}

	fmt.Printf("Relay [%s]: connecting to %s (RemoteForward %d -> localhost:22)...\n",
		rc.Name, relayHost, rc.Context.Relay.RemotePort)

	if err := internalssh.OpenRelay(ctx, relayHost, socketPath); err != nil {
		return fmt.Errorf("opening relay: %w", err)
	}

	if err := internalssh.CheckSocket(ctx, relayHost, socketPath); err != nil {
		return fmt.Errorf("relay [%s]: failed to connect", rc.Name)
	}
	fmt.Printf("Relay [%s]: active\n", rc.Name)

	markerID := rc.Context.UID
	if markerID == "" {
		markerID = rc.Name
	}
	if err := internalssh.WriteRelayMarker(ctx, gateHost, markerID, rc.Context.Relay.RemotePort); err != nil {
		slog.Warn("could not write relay marker on gate", "error", err)
	} else {
		slog.Debug("relay marker written", "port", rc.Context.Relay.RemotePort)
	}

	return nil
}

// discoverRelayPort reads the relay marker file from the gate and updates
// the in-memory config (and persists) if the port differs. Returns true if
// the port was updated, signalling the caller to regenerate SSH config.
func discoverRelayPort(ctx context.Context, rc *config.ResolvedContext, cfg *config.Config) bool {
	if cfg == nil {
		return false
	}

	gateHost := rc.Derived.GateHost
	markerID := rc.Context.UID
	if markerID == "" {
		markerID = rc.Name
	}
	markerPort, err := internalssh.ReadRelayMarker(ctx, gateHost, markerID)
	if err != nil {
		slog.Debug("relay marker read failed", "error", err)
		return false
	}
	if markerPort == 0 {
		return false
	}
	if markerPort == rc.Context.Relay.RemotePort {
		return false
	}

	fmt.Printf("Relay [%s]: discovered port %d from gate (config has %d)\n",
		rc.Name, markerPort, rc.Context.Relay.RemotePort)

	rc.Context.Relay.RemotePort = markerPort

	// Update the in-memory config so SSH config regeneration picks up the new port
	if ctxCfg, ok := cfg.Contexts[rc.Name]; ok {
		ctxCfg.Relay.RemotePort = markerPort
		cfg.Contexts[rc.Name] = ctxCfg
	}

	if err := persistRelayPort(rc); err != nil {
		slog.Warn("could not persist discovered relay port", "error", err)
	}

	return true
}

// persistRelayPort writes the auto-generated relay port back to config.yaml.
func persistRelayPort(rc *config.ResolvedContext) error {
	configPath := filepath.Join(rc.Derived.ConfigDir, "config.yaml")

	_, doc, err := config.LoadRaw(configPath)
	if err != nil {
		return err
	}

	if err := config.SetContext(doc, rc.Name, rc.Context); err != nil {
		return err
	}

	return config.SaveRaw(configPath, doc)
}

func socketExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Type()&os.ModeSocket != 0
}

// ensureSSHConfig regenerates SSH config if config.yaml is newer.
func ensureSSHConfig(rc *config.ResolvedContext, cfg *config.Config) {
	home, _ := os.UserHomeDir()
	socketDir := filepath.Join(home, ".ssh", "sockets")

	mode := rc.Context.Role
	if err := setup.EnsureSSHConfig(cfg, rc.Derived.ConfigDir, socketDir, mode); err != nil {
		slog.Debug("ssh config auto-regen skipped", "error", err)
	}
}
