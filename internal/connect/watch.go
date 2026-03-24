package connect

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
)

const watchInterval = 30 * time.Second

func Watch(ctx context.Context, rc *config.ResolvedContext) error {
	socketPath := rc.Derived.RelaySocket
	relayHost := rc.Derived.RelayHost

	if !socketExists(socketPath) || internalssh.CheckSocket(ctx, relayHost, socketPath) != nil {
		return fmt.Errorf("watch: no active relay found")
	}

	fmt.Printf("watch [%s]: monitoring relay -- Ctrl-C to disconnect\n", rc.Name)
	fmt.Println()

	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			fmt.Println("watch: shutting down")
			_ = internalssh.ExitSocket(context.Background(), relayHost, socketPath)
			fmt.Println("watch: done")
			return nil
		case <-ticker.C:
			if !socketExists(socketPath) || internalssh.CheckSocket(ctx, relayHost, socketPath) != nil {
				fmt.Println()
				return fmt.Errorf("watch: relay session ended unexpectedly")
			}
			fmt.Printf("\r  alive  %s", time.Now().Format("2006-01-02T15:04:05-0700"))
		}
	}
}
