package cmd

import (
	"github.com/cloudygreybeard/jumpgate/internal/connect"
	"github.com/spf13/cobra"
)

var flagWatchRelayPort int

var watchCmd = &cobra.Command{
	Use:   "watch [CONTEXT]",
	Short: "Monitor relay with heartbeat (remote side)",
	Long: `Monitors the relay session, printing a heartbeat every 30 seconds.
Press Ctrl-C to disconnect.

Use --relay-port to override the configured relay port for this session
(not persisted to config).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		ctxName := flagContext
		if len(args) > 0 {
			ctxName = args[0]
		}

		rc, err := loadResolvedContext(ctxName)
		if err != nil {
			return err
		}

		if flagWatchRelayPort > 0 {
			rc.Context.Relay.RemotePort = flagWatchRelayPort
		}

		return connect.Watch(ctx, rc)
	},
}

func init() {
	watchCmd.Flags().IntVar(&flagWatchRelayPort, "relay-port", 0, "override relay port for this session")
	rootCmd.AddCommand(watchCmd)
}
