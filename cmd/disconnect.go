package cmd

import (
	"github.com/cloudygreybeard/jumpgate/internal/connect"
	"github.com/spf13/cobra"
)

var flagDisconnectAll bool

var disconnectCmd = &cobra.Command{
	Use:   "disconnect [CONTEXT]",
	Short: "Close session and destroy Kerberos ticket",
	Long: `Close the local gate session (or remote relay) and destroy the Kerberos ticket.

With --all, also tears down the remote relay before closing the local gate.`,
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

		if flagDisconnectAll {
			connect.DisconnectAll(ctx, rc)
		} else {
			connect.Disconnect(ctx, rc)
		}
		return nil
	},
}

func init() {
	disconnectCmd.Flags().BoolVarP(&flagDisconnectAll, "all", "a", false, "also tear down the remote relay")
	rootCmd.AddCommand(disconnectCmd)
}
