package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/spf13/cobra"
)

var configMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Check for old config format, print guidance",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := flagConfig
		if cfgPath == "" {
			cfgPath = config.DefaultConfigFile()
		}

		configDir := config.DefaultConfigDir()

		data, err := os.ReadFile(cfgPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("  No config.yaml found. Run: jumpgate setup config")
				return nil
			}
			return err
		}

		// Simple heuristic: if "contexts:" key is missing, it's old format
		if !strings.Contains(string(data), "contexts:") {
			fmt.Println("=== Migration required ===")
			fmt.Printf("  Your %s uses the old flat format.\n", cfgPath)
			fmt.Println("  See config.yaml.example for the new multi-context structure.")
		} else {
			fmt.Println("  config.yaml already uses multi-context format.")
		}

		hooksDir := filepath.Join(configDir, "hooks")
		entries, err := os.ReadDir(hooksDir)
		if err != nil || len(entries) == 0 {
			fmt.Println()
			fmt.Println("=== Hooks not installed ===")
			fmt.Println("  Run: jumpgate setup config   (installs example hooks)")
		}

		return nil
	},
}

func init() {
	configCmd.AddCommand(configMigrateCmd)
}
