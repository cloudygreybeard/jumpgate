package connect

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

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
	// Remove relay marker from the gate before tearing down the connection
	gateHost := rc.Derived.GateHost
	markerID := rc.Context.UID
	if markerID == "" {
		markerID = rc.Name
	}
	if err := internalssh.RemoveRelayMarker(ctx, gateHost, markerID); err != nil {
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
		remoteHost,
		fmt.Sprintf("jumpgate disconnect %s", rc.Name),
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
