package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var askpassCmd = &cobra.Command{
	Use:    "askpass",
	Short:  "SSH_ASKPASS helper (echoes $JUMPGATE_ASKPASS_TOKEN)",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(os.Getenv("JUMPGATE_ASKPASS_TOKEN"))
	},
}

func init() {
	rootCmd.AddCommand(askpassCmd)
}
