package cmd

import (
	"fmt"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/sitepack"
	"github.com/spf13/cobra"
)

var flagExportDir string

var configExportCmd = &cobra.Command{
	Use:   "export [CONTEXT]",
	Short: "Export a context as a site pack",
	Long: `Generate a site pack directory from an existing context.

The site pack can be shared and used by others to bootstrap their own
jumpgate configuration with 'jumpgate init --from <dir>'.

The export includes:
  - site.yaml with value schema and prompts
  - values.yaml with the context's current values
  - values.example.yaml with placeholder values
  - templates/config.yaml.tpl with template variables
  - hooks, SSH snippets, and windows scripts from your config dir`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagExportDir == "" {
			return fmt.Errorf("--output-dir is required")
		}

		ctxName := flagContext
		if len(args) > 0 {
			ctxName = args[0]
		}

		rc, err := loadResolvedContext(ctxName)
		if err != nil {
			return err
		}

		configDir := config.DefaultConfigDir()

		fmt.Printf("=== Exporting context %q to %s ===\n", rc.Derived.ContextName, flagExportDir)
		if err := sitepack.Export(rc.Derived.ContextName, &rc.Context, configDir, flagExportDir); err != nil {
			return err
		}

		fmt.Println()
		fmt.Printf("Site pack created in %s\n", flagExportDir)
		fmt.Println()
		fmt.Println("To use this site pack:")
		fmt.Println("  cp values.example.yaml values.yaml")
		fmt.Println("  $EDITOR values.yaml")
		fmt.Println("  jumpgate init --from " + flagExportDir)
		return nil
	},
}

func init() {
	configExportCmd.Flags().StringVar(&flagExportDir, "output-dir", "", "directory to write the site pack into (required)")
	configCmd.AddCommand(configExportCmd)
}
