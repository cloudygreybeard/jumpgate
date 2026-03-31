package cmd

import (
	"github.com/cloudygreybeard/jumpgate/internal/connect"
	"github.com/spf13/cobra"
)

var flagRelayPort int

var connectCmd = &cobra.Command{

	Use:   "connect [CONTEXT]",
	Short: "Open gate session, authenticate, wait for remote",
	Long: `On local: opens the gate ControlMaster, authenticates via Kerberos if
configured, reads the relay port marker from the gate (auto-discovering
port changes from the remote), and polls until the remote host is reachable.

On remote: registers the SSH relay with the gate and writes a relay port
marker file (~/.jumpgate/relay-<context>.port) so the local side can
auto-discover the port.

Use --relay-port to override the configured relay port for this session
(not persisted to config). If remote_port is 0 in config, a random port
in the ephemeral range (49152-65535) is generated and persisted on first use.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		ctxName := flagContext
		if len(args) > 0 {
			ctxName = args[0]
		}

		cfg, rc, err := loadConfigAndContext(ctxName)
		if err != nil {
			return err
		}

		if flagRelayPort > 0 {
			rc.Context.Relay.RemotePort = flagRelayPort
			if ctxCfg, ok := cfg.Contexts[rc.Name]; ok {
				ctxCfg.Relay.RemotePort = flagRelayPort
				cfg.Contexts[rc.Name] = ctxCfg
			}
		}

		return connect.Connect(ctx, rc, cfg)
	},
}

func init() {
	connectCmd.Flags().IntVar(&flagRelayPort, "relay-port", 0, "override relay port for this session")
	rootCmd.AddCommand(connectCmd)
}
