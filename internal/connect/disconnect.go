package connect

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/auth"
	"github.com/cloudygreybeard/jumpgate/internal/config"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
)

func Disconnect(ctx context.Context, rc *config.ResolvedContext) {
	if rc.IsLocal() {
		auth.CloseGate(ctx, rc)
	} else {
		disconnectRemote(ctx, rc)
	}
}

func disconnectRemote(ctx context.Context, rc *config.ResolvedContext) {
	if runtime.GOOS == "windows" {
		disconnectRemoteWindows(ctx, rc)
		return
	}
	disconnectRemoteUnix(ctx, rc)
}

// disconnectRemoteWindows handles disconnect on Windows where there is no
// ControlMaster. The relay runs as a foreground SSH session (Ctrl+C to stop),
// so there is no socket to exit. Marker cleanup is skipped to avoid requiring
// a fresh bastion authentication just to disconnect.
func disconnectRemoteWindows(_ context.Context, rc *config.ResolvedContext) {
	fmt.Printf("Relay [%s]: not active (foreground mode)\n", rc.Name)

	cmd := exec.CommandContext(context.Background(), "kdestroy")
	if err := cmd.Run(); err != nil {
		fmt.Println("Auth: no ticket to destroy")
	} else {
		fmt.Println("Auth: ticket destroyed")
	}
}

func disconnectRemoteUnix(ctx context.Context, rc *config.ResolvedContext) {
	socketPath := rc.Derived.RelaySocket
	relayHost := rc.Derived.RelayHost

	if socketExists(socketPath) {
		// Remove marker while the ControlMaster is still alive so we
		// piggyback on the existing session (no extra auth).
		markerID := rc.Context.UID
		if markerID == "" {
			markerID = rc.Name
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := internalssh.RemoveRelayMarker(cleanupCtx, relayHost, markerID); err != nil {
			slog.Debug("relay marker cleanup failed", "error", err)
		}
		cancel()

		if err := internalssh.ExitSocket(ctx, relayHost, socketPath); err != nil {
			fmt.Printf("Relay [%s]: already gone\n", rc.Name)
		} else {
			fmt.Printf("Relay [%s]: closed\n", rc.Name)
		}
	} else {
		fmt.Printf("Relay [%s]: not active\n", rc.Name)
	}

	cmd := exec.CommandContext(ctx, "kdestroy")
	if err := cmd.Run(); err != nil {
		fmt.Println("Auth: no ticket to destroy")
	} else {
		fmt.Println("Auth: ticket destroyed")
	}
}

func DisconnectRemoteSide(ctx context.Context, rc *config.ResolvedContext) {
	fmt.Printf("=== Remote relay [%s] ===\n", rc.Name)

	ccFile := rc.Context.Auth.CCFile
	remoteHost := rc.Derived.RemoteHost

	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
		"-o", "UserKnownHostsFile="+internalssh.KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
		remoteHost,
		fmt.Sprintf("jumpgate disconnect '%s'", rc.Name),
	)
	if ccFile != "" {
		cmd.Env = append(cmd.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}
	if err := cmd.Run(); err != nil {
		fmt.Println("  Could not reach remote (is the relay up?)")
	}
}

func DisconnectAll(ctx context.Context, rc *config.ResolvedContext) {
	DisconnectRemoteSide(ctx, rc)
	Disconnect(ctx, rc)
}

// ForceCleanup kills orphaned SSH processes for the relay/gate and removes
// stale socket files. Use when normal disconnect leaves processes behind.
//
// Phase 1: kill processes matching the current config's ControlPath (precise).
// Phase 2: list any other SSH processes that look like jumpgate relays so the
// user can decide whether to kill them manually.
func ForceCleanup(rc *config.ResolvedContext) {
	killed := 0

	for _, sock := range []string{rc.Derived.RelaySocket, rc.Derived.GateSocket} {
		if sock == "" {
			continue
		}
		cmd := exec.CommandContext(context.Background(), "pkill", "-f", fmt.Sprintf("ControlPath=%s", sock))
		if err := cmd.Run(); err == nil {
			killed++
		}
		if err := os.Remove(sock); err == nil {
			fmt.Printf("Removed stale socket: %s\n", sock)
		}
	}

	if killed > 0 {
		fmt.Printf("Force cleanup: killed %d orphaned SSH process(es)\n", killed)
	} else {
		fmt.Println("Force cleanup: no orphaned processes for current config")
	}

	listOrphanedRelays()
}

// listOrphanedRelays finds SSH processes that look like jumpgate-managed
// relays (ControlMaster with -N) but weren't matched by the precise kill.
func listOrphanedRelays() {
	cmd := exec.CommandContext(context.Background(),
		"sh", "-c", "ps auxww | grep '[s]sh.*ControlMaster.*-N'")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return
	}

	lines := strings.TrimSpace(string(out))
	if lines == "" {
		return
	}

	fmt.Println("\nPossible orphaned relay processes (not matched by current config):")
	for _, line := range strings.Split(lines, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		fmt.Printf("  PID %s: %s\n", fields[1], strings.Join(fields[10:], " "))
	}
	fmt.Println("  To kill: kill <PID>")
}
