package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/bootstrap"
	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/setup"
	"github.com/cloudygreybeard/jumpgate/internal/sitepack"
	"github.com/cloudygreybeard/jumpgate/internal/sshd"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var flagFrom string
var flagPaste bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Bootstrap jumpgate config, hooks, and SSH from a site pack or defaults",
	Long: `Initialise jumpgate configuration from scratch.

Without flags:  creates ~/.config/jumpgate/ with an example config template.
With --from:    reads a site pack directory containing site.yaml, values.yaml,
                templates, hooks, and snippets, renders everything into
                ~/.config/jumpgate/, and generates SSH config automatically.
With --paste:   prompts for a base64 bootstrap string (generated on the local
                end by 'jumpgate setup remote-init'), decodes it into a remote-
                role config, writes config.yaml, and generates SSH config.

Site pack structure:

  site.yaml              metadata + value schema with defaults
  values.yaml            your answers (flat key: value pairs)
  templates/             Go text/template files (*.tpl -> config dir)
  hooks/                 shell scripts, copied and made executable
  snippets/              SSH config fragments (*.tpl), overlaid on defaults
  windows/               optional Windows integration scripts`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&flagFrom, "from", "", "path to a site pack directory")
	initCmd.Flags().BoolVar(&flagPaste, "paste", false, "bootstrap from a pasted base64 string (remote-init payload)")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	configDir := config.DefaultConfigDir()

	if flagPaste {
		return runInitFromPaste(configDir)
	}
	if flagFrom != "" {
		return runInitFromPack(cmd, configDir)
	}
	return runInitDefault(configDir)
}

func runInitDefault(configDir string) error {
	configTemplate, err := loadConfigTemplate()
	if err != nil {
		return err
	}
	return runInitSetupConfig(configDir, configTemplate)
}

func runInitFromPack(cmd *cobra.Command, configDir string) error {
	pack, err := sitepack.LoadPack(flagFrom)
	if err != nil {
		return err
	}

	fmt.Printf("=== Site pack: %s ===\n", pack.Name)
	if pack.Description != "" {
		fmt.Printf("  %s\n", pack.Description)
	}
	fmt.Println()

	vals, err := sitepack.LoadValues(pack.Dir, pack.Values)
	if err != nil {
		return err
	}

	missing := false
	for _, def := range pack.Values {
		if v, ok := vals[def.Key]; !ok || v == "" {
			missing = true
			break
		}
	}
	if missing {
		fmt.Println("Some values are missing. Please provide them:")
		reader := bufio.NewReader(os.Stdin)
		if err := sitepack.PromptMissing(vals, pack.Values, reader); err != nil {
			return err
		}
		fmt.Println()
	}

	fmt.Printf("=== Rendering to %s ===\n", configDir)
	if err := sitepack.Render(pack, vals, configDir); err != nil {
		return err
	}
	fmt.Println()

	// Auto-run setup ssh
	rc, err := loadResolvedContext(flagContext)
	if err != nil {
		fmt.Println("  Config rendered. Run 'jumpgate setup ssh' after editing config.yaml.")
		return nil
	}

	if err := runSetupSSH(rc); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Ready. Run 'jumpgate connect' to start.")

	// Run credentials setup hook if available
	hooksDir := filepath.Join(configDir, "hooks")
	if _, err := os.Stat(filepath.Join(hooksDir, "setup-credentials")); err == nil {
		fmt.Println()
		fmt.Println("Tip: run 'jumpgate setup credentials' to store credentials in your keychain.")
	}

	return nil
}

func runInitFromPaste(configDir string) error {
	fmt.Println("Paste the bootstrap string from your local jumpgate, then press Enter:")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	line = strings.TrimSpace(line)

	if line == "" {
		return fmt.Errorf("no input provided")
	}

	cfg, err := bootstrap.DecodeConfig(line)
	if err != nil {
		return fmt.Errorf("decoding bootstrap payload: %w", err)
	}

	for name, ctx := range cfg.Contexts {
		if ctx.UID == "" {
			ctx.UID = config.GenerateUID()
			cfg.Contexts[name] = ctx
		}
	}

	ctxName := cfg.DefaultContext
	if _, ok := cfg.Contexts[ctxName]; !ok {
		for k := range cfg.Contexts {
			ctxName = k
			break
		}
	}

	fmt.Printf("\n=== Bootstrap: context %q (role: remote) ===\n", ctxName)

	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(configDir, "ssh"), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	// Strip authorized_key from config before writing config.yaml
	// (it's stored separately and not needed in the main config file)
	cfgToWrite := *cfg
	cfgToWrite.AuthorizedKey = ""

	header := []byte("# Jumpgate remote config -- bootstrapped via: jumpgate init --paste\n\n")
	cfgData, err := yaml.Marshal(&cfgToWrite)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	if err := os.WriteFile(configPath, append(header, cfgData...), 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	fmt.Printf("  Wrote %s\n", configPath)

	// Write authorized key for bootstrap SSH server
	if cfg.AuthorizedKey != "" {
		authKeyPath := filepath.Join(configDir, "authorized_key")
		if err := os.WriteFile(authKeyPath, []byte(cfg.AuthorizedKey+"\n"), 0644); err != nil {
			return fmt.Errorf("writing authorized key: %w", err)
		}
		fmt.Printf("  Wrote %s\n", authKeyPath)
	}

	// Generate host key for bootstrap SSH server
	hostKeyPath := filepath.Join(configDir, "hostkey")
	fp, err := sshd.GenerateHostKey(hostKeyPath)
	if err != nil {
		return fmt.Errorf("generating host key: %w", err)
	}
	fmt.Printf("  Generated host key (fingerprint: %s)\n", fp)

	rc, err := cfg.Resolve(ctxName)
	if err != nil {
		fmt.Println("  Config written. Run 'jumpgate setup ssh' to generate SSH config.")
		return nil
	}

	if err := runSetupSSH(rc); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Ready. Run 'jumpgate bootstrap' to start the bootstrap relay,")
	fmt.Println("       or 'jumpgate connect' if sshd is already running.")
	fmt.Println()
	fmt.Println("Tip: 'jumpgate bootstrap' does this automatically if no config exists.")
	return nil
}

func runInitSetupConfig(configDir string, configTemplate []byte) error {
	return setup.SetupConfigSimple(configDir, configTemplate)
}
