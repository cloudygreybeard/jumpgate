package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configImportCmd = &cobra.Command{
	Use:   "import [FILE]",
	Short: "Import a context from JSON or YAML",
	Long: `Import a context from a JSON or YAML file (or stdin).

Accepts either the full output of 'jumpgate config view -o json/yaml'
(the ConfigView envelope) or a bare context object. The format is
auto-detected.

Examples:
  jumpgate config view -o json work | jumpgate config import --context staging
  jumpgate config import --context lab config-export.json
  jumpgate config import < context.yaml`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var r io.Reader
		if len(args) == 0 || args[0] == "-" {
			r = os.Stdin
		} else {
			f, err := os.Open(args[0])
			if err != nil {
				return fmt.Errorf("opening %s: %w", args[0], err)
			}
			defer f.Close()
			r = f
		}

		data, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}

		ctxName, ctx, err := parseImportData(data)
		if err != nil {
			return err
		}

		if flagImportContext != "" {
			ctxName = flagImportContext
		}
		if ctxName == "" {
			return fmt.Errorf("context name not found in input; use --context to specify one")
		}

		cfgPath := configFilePath()
		_, doc, err := config.LoadRaw(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if err := config.SetContext(doc, ctxName, ctx); err != nil {
			return err
		}
		if err := config.SaveRaw(cfgPath, doc); err != nil {
			return err
		}

		fmt.Printf("Imported context %q\n", ctxName)
		return autoRegenSSH()
	},
}

var flagImportContext string

func init() {
	configImportCmd.Flags().StringVar(&flagImportContext, "context", "", "context name (overrides name in input)")
	configCmd.AddCommand(configImportCmd)
}

// parseImportData tries to unmarshal input as a ConfigView envelope first,
// then falls back to a bare config.Context. JSON is attempted before YAML
// since valid JSON is also valid YAML.
func parseImportData(data []byte) (string, config.Context, error) {
	// Try ConfigView envelope (JSON)
	var view ConfigView
	if err := json.Unmarshal(data, &view); err == nil && view.Config.Gate.Hostname != "" {
		return view.Name, view.Config, nil
	}

	// Try bare Context (JSON)
	var ctx config.Context
	if err := json.Unmarshal(data, &ctx); err == nil && ctx.Gate.Hostname != "" {
		return "", ctx, nil
	}

	// Try ConfigView envelope (YAML)
	if err := yaml.Unmarshal(data, &view); err == nil && view.Config.Gate.Hostname != "" {
		return view.Name, view.Config, nil
	}

	// Try bare Context (YAML)
	if err := yaml.Unmarshal(data, &ctx); err == nil && (ctx.Gate.Hostname != "" || ctx.Role != "") {
		return "", ctx, nil
	}

	return "", config.Context{}, fmt.Errorf("input is not a valid context (expected ConfigView or Context in JSON/YAML)")
}
