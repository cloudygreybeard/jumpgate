package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/bootstrap"
	"github.com/cloudygreybeard/jumpgate/internal/config"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
	"github.com/cloudygreybeard/jumpgate/internal/sshd"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const bootstrapSSHPort = 2222

var flagBootstrapServerOnly bool
var flagBootstrapReinit bool

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap [CONTEXT]",
	Short: "One-time bootstrap: embedded SSH server + relay tunnel",
	Long: `Start a temporary embedded SSH server and open a relay tunnel
through the gate. This is a one-time command for initial remote setup,
before WSL or sshd are installed.

If no config exists yet, you will be prompted to paste the bootstrap
payload (generated on the local end by 'jumpgate setup remote-init').
This replaces the separate 'jumpgate init --paste' step.

The embedded server listens on localhost:2222 and accepts connections
authenticated with the public key included in the bootstrap payload.
The relay tunnel forwards the gate port to this server.

On the local side, 'jumpgate setup remote' connects through the relay
as a normal SSH client to push configuration, install WSL, and set up
sshd. Once sshd is running, use 'jumpgate connect' instead — this
command is never needed again.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctxName := flagContext
		if len(args) > 0 {
			ctxName = args[0]
		}

		configDir := config.DefaultConfigDir()
		configPath := filepath.Join(configDir, "config.yaml")

		// If no config exists (or --reinit), run the init-from-paste flow inline
		if flagBootstrapReinit || os.IsNotExist(statErr(configPath)) {
			rc, err := bootstrapInit(configDir)
			if err != nil {
				return err
			}
			return runBootstrap(cmd.Context(), rc)
		}

		_, rc, err := loadConfigAndContext(ctxName)
		if err != nil {
			return err
		}
		return runBootstrap(cmd.Context(), rc)
	},
}

func init() {
	bootstrapCmd.Flags().BoolVar(&flagBootstrapServerOnly, "server-only", false,
		"run the embedded SSH server without opening the relay tunnel")
	bootstrapCmd.Flags().BoolVar(&flagBootstrapReinit, "reinit", false,
		"re-prompt for the bootstrap payload even if config already exists")
	rootCmd.AddCommand(bootstrapCmd)
}

func statErr(path string) error {
	_, err := os.Stat(path)
	return err
}

// bootstrapInit prompts for the base64 payload and writes config, authorized
// key, host key, and SSH config — the same work as 'jumpgate init --paste'
// but invoked automatically when bootstrap finds no existing config.
func bootstrapInit(configDir string) (*config.ResolvedContext, error) {
	fmt.Println("No jumpgate config found — starting first-time setup.")
	fmt.Println()
	fmt.Println("On your local machine, run:")
	fmt.Println("  jumpgate setup remote-init")
	fmt.Println()
	fmt.Println("Paste the bootstrap string below, then press Enter:")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("no input provided")
	}

	cfg, err := bootstrap.DecodeConfig(line)
	if err != nil {
		return nil, fmt.Errorf("decoding bootstrap payload: %w", err)
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
		return nil, fmt.Errorf("creating config dir: %w", err)
	}

	cfgToWrite := *cfg
	cfgToWrite.AuthorizedKey = ""

	header := []byte("# Jumpgate remote config -- bootstrapped via: jumpgate bootstrap\n\n")
	cfgData, err := yaml.Marshal(&cfgToWrite)
	if err != nil {
		return nil, fmt.Errorf("marshalling config: %w", err)
	}
	if err := os.WriteFile(configPath, append(header, cfgData...), 0644); err != nil {
		return nil, fmt.Errorf("writing config: %w", err)
	}
	fmt.Printf("  Wrote %s\n", configPath)

	if cfg.AuthorizedKey != "" {
		authKeyPath := filepath.Join(configDir, "authorized_key")
		if err := os.WriteFile(authKeyPath, []byte(cfg.AuthorizedKey+"\n"), 0644); err != nil {
			return nil, fmt.Errorf("writing authorized key: %w", err)
		}
		fmt.Printf("  Wrote %s\n", authKeyPath)
	}

	hostKeyPath := filepath.Join(configDir, "hostkey")
	fp, err := sshd.GenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("generating host key: %w", err)
	}
	fmt.Printf("  Generated host key (fingerprint: %s)\n", fp)

	rc, err := cfg.Resolve(ctxName)
	if err != nil {
		return nil, fmt.Errorf("resolving context: %w", err)
	}

	if err := runSetupSSH(rc); err != nil {
		return nil, err
	}

	fmt.Println()
	return rc, nil
}

func runBootstrap(parentCtx context.Context, rc *config.ResolvedContext) error {
	if rc.Context.Role != "remote" {
		return fmt.Errorf("bootstrap is only for remote-role contexts (current role: %s)", rc.Context.Role)
	}

	configDir := rc.Derived.ConfigDir
	hostKeyPath := filepath.Join(configDir, "hostkey")
	authKeyPath := filepath.Join(configDir, "authorized_key")

	if _, err := os.Stat(hostKeyPath); err != nil {
		return fmt.Errorf("host key not found at %s -- re-run 'jumpgate bootstrap' to reinitialise", hostKeyPath)
	}
	if _, err := os.Stat(authKeyPath); err != nil {
		return fmt.Errorf("authorized key not found at %s -- re-run 'jumpgate bootstrap' to reinitialise", authKeyPath)
	}

	listenAddr := fmt.Sprintf("127.0.0.1:%d", bootstrapSSHPort)
	srv, err := sshd.New(hostKeyPath, authKeyPath, listenAddr)
	if err != nil {
		return fmt.Errorf("starting bootstrap SSH server: %w", err)
	}

	ctx, cancel := signal.NotifyContext(parentCtx, os.Interrupt)
	defer cancel()

	// Start the embedded SSH server in a background goroutine
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- srv.ListenAndServe(ctx)
	}()

	fmt.Printf("Bootstrap [%s]: embedded SSH server on %s\n", rc.Name, listenAddr)
	fmt.Println()
	authLabel := srv.AuthKeyComment()
	if authLabel == "" {
		authLabel = "(no comment)"
	}
	fmt.Printf("  Host key:       %s\n", srv.Fingerprint())
	fmt.Printf("  Authorized key: %s (%s)\n", authLabel, srv.AuthKeyType())
	fmt.Printf("  Auth methods:   public key only (no passwords)\n")
	fmt.Printf("  Bind address:   %s (loopback only, not network-exposed)\n", listenAddr)
	fmt.Printf("  Capabilities:   exec only (no shell, no pty, no sftp)\n")
	fmt.Println()

	if flagBootstrapServerOnly {
		fmt.Println("  (server-only mode — Ctrl+C to stop)")
		<-ctx.Done()
		<-srvErr
		fmt.Println()
		fmt.Println("Bootstrap server stopped.")
		return nil
	}

	// Use the gate host alias (not relay) to avoid the config-baked
	// RemoteForward that targets sshd. We pass our own -R explicitly.
	gateHost := rc.Derived.GateHost
	relayPort := rc.Context.Relay.RemotePort

	fmt.Printf("Bootstrap [%s]: opening relay via %s (RemoteForward %d -> localhost:%d)...\n",
		rc.Name, gateHost, relayPort, bootstrapSSHPort)
	fmt.Println("  (foreground session — Ctrl+C to close)")
	fmt.Println()

	relayErr := internalssh.RunRelayForeground(ctx, gateHost, relayPort, bootstrapSSHPort)

	// Relay ended — shut down the embedded server
	cancel()
	<-srvErr

	if relayErr != nil && ctx.Err() == nil {
		return fmt.Errorf("relay: %w", relayErr)
	}

	fmt.Println()
	fmt.Println("Bootstrap session ended.")
	return nil
}
