package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"text/tabwriter"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/output"
	"github.com/spf13/cobra"
)

// ConfigView is the structured representation of `config view` output.
type ConfigView struct {
	Platform PlatformInfo   `json:"platform" yaml:"platform"`
	Name     string         `json:"context" yaml:"context"`
	Derived  config.Derived `json:"derived" yaml:"derived"`
	Config   config.Context `json:"config" yaml:"config"`
}

type PlatformInfo struct {
	OS   string `json:"os" yaml:"os"`
	Arch string `json:"arch" yaml:"arch"`
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
	Long: `Manage jumpgate contexts and configuration.

Subcommands: list, current, use, create, delete, rename, edit, view,
import, export, migrate.

Use -o json/yaml/wide with view and list for structured output.`,
}

var configViewCmd = &cobra.Command{
	Use:   "view [CONTEXT]",
	Short: "Dump resolved configuration",
	Long: `Display the fully resolved configuration for a context, including
platform info, derived values (computed hosts, socket paths), and all
config fields.

Supports -o json and -o yaml for machine-parseable output. The structured
output can be piped to 'jumpgate config import' to clone or transfer
contexts between installations.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctxName := flagContext
		if len(args) > 0 {
			ctxName = args[0]
		}

		rc, err := loadResolvedContext(ctxName)
		if err != nil {
			return err
		}

		of, err := outputFormat()
		if err != nil {
			return err
		}

		if output.IsStructured(of) {
			view := ConfigView{
				Platform: PlatformInfo{OS: runtime.GOOS, Arch: runtime.GOARCH},
				Name:     rc.Derived.ContextName,
				Derived:  rc.Derived,
				Config:   rc.Context,
			}
			return output.Print(of, view)
		}

		p := rc.Context
		d := rc.Derived
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

		fmt.Fprintln(w, "=== Platform ===")
		fmt.Fprintf(w, "  OS:\t%s\n", runtime.GOOS)
		fmt.Fprintf(w, "  Arch:\t%s\n", runtime.GOARCH)
		fmt.Fprintf(w, "  CONFIG_DIR:\t%s\n", d.ConfigDir)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== Context ===")
		fmt.Fprintf(w, "  CONTEXT:\t%s\n", d.ContextName)
		fmt.Fprintf(w, "  ROLE:\t%s\n", p.Role)
		fmt.Fprintf(w, "  GATE_HOST:\t%s\n", d.GateHost)
		fmt.Fprintf(w, "  REMOTE_HOST:\t%s\n", d.RemoteHost)
		fmt.Fprintf(w, "  RELAY_HOST:\t%s\n", d.RelayHost)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== Gate ===")
		fmt.Fprintf(w, "  GATE_HOSTNAME:\t%s\n", p.Gate.Hostname)
		fmt.Fprintf(w, "  GATE_PORT:\t%d\n", p.Gate.Port)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== Auth ===")
		fmt.Fprintf(w, "  AUTH_TYPE:\t%s\n", p.Auth.Type)
		fmt.Fprintf(w, "  AUTH_REALM:\t%s\n", p.Auth.Realm)
		fmt.Fprintf(w, "  AUTH_USER:\t%s\n", p.Auth.User)
		fmt.Fprintf(w, "  AUTH_PRINCIPAL:\t%s\n", d.AuthPrincipal)
		fmt.Fprintf(w, "  AUTH_KDC:\t%s\n", p.Auth.KDC)
		fmt.Fprintf(w, "  AUTH_KDC_LOCAL_PORT:\t%d\n", p.Auth.KDCLocalPort)
		fmt.Fprintf(w, "  AUTH_KDC_REMOTE_PORT:\t%d\n", p.Auth.KDCRemotePort)
		fmt.Fprintf(w, "  AUTH_KINIT:\t%s\n", p.Auth.Kinit)
		fmt.Fprintf(w, "  AUTH_CC_FILE:\t%s\n", p.Auth.CCFile)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== Remote ===")
		fmt.Fprintf(w, "  REMOTE_USER:\t%s\n", p.Remote.User)
		fmt.Fprintf(w, "  REMOTE_KEY:\t%s\n", p.Remote.Key)
		fmt.Fprintf(w, "  REMOTE_DIR:\t%s\n", p.Remote.RemoteDir)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== Relay ===")
		fmt.Fprintf(w, "  RELAY_PORT:\t%d\n", p.Relay.RemotePort)
		if rc.IsLocal() {
			fmt.Fprintf(w, "  GATE_SOCKET:\t%s\n", d.GateSocket)
		} else {
			fmt.Fprintf(w, "  RELAY_SOCKET:\t%s\n", d.RelaySocket)
		}
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== Keychain ===")
		fmt.Fprintf(w, "  TOTP_SERVICE:\t%s\n", p.Keychain.TOTPService)
		fmt.Fprintf(w, "  TOTP_ACCOUNT:\t%s\n", p.Keychain.TOTPAccount)
		fmt.Fprintf(w, "  KRB_SERVICE:\t%s\n", p.Keychain.KRBService)
		fmt.Fprintf(w, "  KRB_ACCOUNT:\t%s\n", p.Keychain.KRBAccount)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== TOTP ===")
		fmt.Fprintf(w, "  TOTP_CLI:\t%s\n", p.TOTP.CLI)
		fmt.Fprintf(w, "  TOTP_NAMESPACE:\t%s\n", p.TOTP.Namespace)
		fmt.Fprintf(w, "  TOTP_ACCOUNT:\t%s\n", p.TOTP.Account)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "=== Container ===")
		fmt.Fprintf(w, "  CONTAINER_RT:\t%s\n", p.Container.Runtime)
		fmt.Fprintf(w, "  CONTAINER_IMAGE:\t%s\n", p.Container.Image)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  WINDOWS_APP:\t%s\n", p.WindowsApp)

		return w.Flush()
	},
}

func init() {
	configCmd.AddCommand(configViewCmd)
	rootCmd.AddCommand(configCmd)
}

func loadResolvedContext(contextName string) (*config.ResolvedContext, error) {
	_, rc, err := loadConfigAndContext(contextName)
	return rc, err
}

func loadConfigAndContext(contextName string) (*config.Config, *config.ResolvedContext, error) {
	cfgPath := flagConfig
	if cfgPath == "" {
		cfgPath = config.DefaultConfigFile()
	}

	slog.Debug("loading config", "path", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config from %s: %w", cfgPath, err)
	}

	rc, err := cfg.Resolve(contextName)
	if err != nil {
		return nil, nil, err
	}

	slog.Debug("resolved context", "name", rc.Name, "gate", rc.Context.Gate.Hostname)
	return cfg, rc, nil
}
