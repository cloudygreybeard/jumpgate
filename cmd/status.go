package cmd

import (
	"github.com/cloudygreybeard/jumpgate/internal/connect"
	"github.com/cloudygreybeard/jumpgate/internal/output"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [CONTEXT]",
	Short: "Show gate / auth / remote status",
	Long: `Show the current connection status: gate session, Kerberos ticket,
remote reachability (local role) or relay tunnel and SSH service
(remote role).

Supports -o json and -o yaml for machine-parseable output.`,
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

		info := connect.CollectStatus(ctx, rc)

		of, err := outputFormat()
		if err != nil {
			return err
		}

		if output.IsStructured(of) {
			return output.Print(of, info)
		}

		connect.PrintStatus(info)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
