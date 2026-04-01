package connect

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
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
	gateHost := rc.Derived.GateHost
	markerID := rc.Context.UID
	if markerID == "" {
		markerID = rc.Name
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := internalssh.RemoveRelayMarker(cleanupCtx, gateHost, markerID); err != nil {
		slog.Debug("relay marker cleanup failed", "error", err)
	}

	socketPath := rc.Derived.RelaySocket
	relayHost := rc.Derived.RelayHost

	if socketExists(socketPath) {
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
