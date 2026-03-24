package cmd

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/setup"
	"github.com/spf13/cobra"
)

//go:embed all:embed
var embeddedFS embed.FS

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive first-time setup (all steps)",
	RunE: func(cmd *cobra.Command, args []string) error {
		configDir := config.DefaultConfigDir()
		configTemplate, err := loadConfigTemplate()
		if err != nil {
			return err
		}

		if err := setup.SetupConfigSimple(configDir, configTemplate); err != nil {
			return err
		}

		ctxName := flagContext
		rc, err := loadResolvedContext(ctxName)
		if err != nil {
			fmt.Println("  Skipping SSH setup (config not yet customised)")
		} else {
			if err := runSetupSSH(rc); err != nil {
				return err
			}
		}

		fmt.Println()
		fmt.Println("Setup complete. Edit your config and run: jumpgate connect")
		return nil
	},
}

var setupConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Create CONFIG_DIR, copy config template and hooks",
	RunE: func(cmd *cobra.Command, args []string) error {
		configDir := config.DefaultConfigDir()
		configTemplate, err := loadConfigTemplate()
		if err != nil {
			return err
		}
		return setup.SetupConfigSimple(configDir, configTemplate)
	},
}

var setupSSHCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Generate SSH configs, update ~/.ssh/config Include",
	RunE: func(cmd *cobra.Command, args []string) error {
		rc, err := loadResolvedContext(flagContext)
		if err != nil {
			return err
		}
		return runSetupSSH(rc)
	},
}

var setupCredentialsCmd = &cobra.Command{
	Use:   "credentials",
	Short: "Run the setup-credentials hook (interactive)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		rc, err := loadResolvedContext(flagContext)
		if err != nil {
			return err
		}
		return setup.SetupCredentials(ctx, rc)
	},
}

func init() {
	setupCmd.AddCommand(setupConfigCmd)
	setupCmd.AddCommand(setupSSHCmd)
	setupCmd.AddCommand(setupCredentialsCmd)
	rootCmd.AddCommand(setupCmd)
}

func runSetupSSH(rc *config.ResolvedContext) error {
	fmt.Printf("=== SSH config [%s] ===\n", rc.Name)

	cfgPath := flagConfig
	if cfgPath == "" {
		cfgPath = config.DefaultConfigFile()
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	socketDir := filepath.Join(home, ".ssh", "sockets")

	snippets, err := loadSnippets(rc.Derived.ConfigDir)
	if err != nil {
		return err
	}

	mode := rc.Context.Role

	if err := setup.GenerateSSHConfig(cfg, rc.Derived.ConfigDir, socketDir, mode, snippets); err != nil {
		return err
	}

	var sshConfigFile string
	if mode == "remote" {
		sshConfigFile = filepath.Join(rc.Derived.ConfigDir, "ssh", "config.remote")
	} else {
		sshConfigFile = filepath.Join(rc.Derived.ConfigDir, "ssh", "config.local")
	}
	if err := setup.AddSSHInclude(sshConfigFile); err != nil {
		return err
	}

	return nil
}

func loadConfigTemplate() ([]byte, error) {
	return embeddedFS.ReadFile("embed/config.yaml.example")
}

func loadSnippets(configDir string) (map[string]string, error) {
	snippets := make(map[string]string)

	// Load embedded defaults
	entries, err := embeddedFS.ReadDir("embed/snippets")
	if err != nil {
		return nil, fmt.Errorf("reading embedded snippets: %w", err)
	}
	for _, e := range entries {
		data, err := embeddedFS.ReadFile("embed/snippets/" + e.Name())
		if err != nil {
			return nil, err
		}
		snippets[e.Name()] = string(data)
	}

	// Overlay user-provided snippets from configDir/ssh/snippets/
	userSnippetDir := filepath.Join(configDir, "ssh", "snippets")
	userEntries, err := os.ReadDir(userSnippetDir)
	if err != nil {
		return snippets, nil // no user snippets dir is fine
	}
	for _, e := range userEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tpl" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(userSnippetDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading user snippet %s: %w", e.Name(), err)
		}
		snippets[e.Name()] = string(data)
	}

	return snippets, nil
}
