package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/auth"
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
	Short: "One-time setup for a new remote (run on both sides)",
	Long: `One-command bootstrap — run on both local and remote.

Local (role=local):
  1. Generates and displays the bootstrap payload + install instructions
  2. Establishes gate session and authenticates (Kerberos if configured)
  3. Waits for the remote's bootstrap server to appear on the relay
  4. Pushes full configuration, hooks, and SSH snippets to the remote

Remote (role=remote):
  1. If no config exists, prompts for the bootstrap payload
  2. Starts a temporary embedded SSH server on localhost:2222
  3. Opens a relay tunnel through the gate

The workflow:
  Local:   jumpgate bootstrap       (prints payload, waits for remote)
  Remote:  jumpgate bootstrap       (paste payload, authenticate to bastion)
  Local:   automatically detects remote, pushes config — done

Once sshd is running on the remote, use 'jumpgate connect' instead.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctxName := flagContext
		if len(args) > 0 {
			ctxName = args[0]
		}

		configDir := config.DefaultConfigDir()
		configPath := filepath.Join(configDir, "config.yaml")

		// No config (or --reinit): remote init-from-paste flow
		if flagBootstrapReinit || os.IsNotExist(statErr(configPath)) {
			rc, err := bootstrapInit(configDir)
			if err != nil {
				return err
			}
			return runBootstrapRemote(cmd.Context(), rc)
		}

		cfg, rc, err := loadConfigAndContext(ctxName)
		if err != nil {
			return err
		}

		if rc.IsLocal() {
			return runBootstrapLocal(cmd, rc, cfg)
		}
		return runBootstrapRemote(cmd.Context(), rc)
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

func runBootstrapLocal(cmd *cobra.Command, rc *config.ResolvedContext, cfg *config.Config) error {
	ctx := cmd.Context()

	fmt.Printf("=== Bootstrap local [%s] ===\n", rc.Name)
	fmt.Println()

	// 1. Generate and display the bootstrap payload
	portBefore := rc.Context.Relay.RemotePort
	remoteCfg := bootstrap.RemoteConfig(rc.Derived.ContextName, &rc.Context)

	if rc.Context.Relay.RemotePort != portBefore {
		fmt.Printf("Relay [%s]: assigned port %d\n", rc.Name, rc.Context.Relay.RemotePort)
		if ctxCfg, ok := cfg.Contexts[rc.Name]; ok {
			ctxCfg.Relay.RemotePort = rc.Context.Relay.RemotePort
			cfg.Contexts[rc.Name] = ctxCfg
		}
		if err := persistPort(rc); err != nil {
			slog.Warn("could not persist relay port to local config", "error", err)
		}
	}

	b64, err := bootstrap.Encode(remoteCfg)
	if err != nil {
		return fmt.Errorf("encoding bootstrap payload: %w", err)
	}

	fmt.Println("Install jumpgate on the remote, then run: jumpgate bootstrap")
	fmt.Println("When prompted, paste this string:")
	fmt.Println()
	fmt.Println(b64)
	fmt.Println()
	fmt.Println("Install commands (pick one):")
	fmt.Println("  PowerShell:  irm https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.ps1 | iex")
	fmt.Println("  curl | sh:   curl -fsSL https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.sh | sh")
	fmt.Println()

	// 2. Establish gate + auth
	if err := auth.EnsureGate(ctx, rc); err != nil {
		return err
	}
	if err := auth.EnsureKerberos(ctx, rc); err != nil {
		return err
	}

	// Regenerate SSH config so the relay port is current
	if err := runSetupSSH(rc); err != nil {
		slog.Warn("could not regenerate SSH config", "error", err)
	}

	// 3. Wait for the remote bootstrap server to appear
	remoteHost := rc.Derived.RemoteHost
	fmt.Println()
	fmt.Printf("Waiting for remote [%s] to connect (relay port %d)...\n",
		rc.Name, rc.Context.Relay.RemotePort)
	fmt.Println("  Run 'jumpgate bootstrap' on the remote and authenticate to the bastion.")
	fmt.Println()

	if err := waitForRemote(ctx, remoteHost); err != nil {
		return err
	}

	fmt.Printf("Remote [%s] detected!\n", rc.Name)
	fmt.Println()

	// 4. Push full config
	return runSetupRemote(cmd, rc)
}

// waitForRemote polls until the remote bootstrap server is reachable via SSH,
// using accept-new for host keys and echo instead of true for PowerShell
// compatibility. No hooks are invoked during polling.
func waitForRemote(ctx context.Context, remoteHost string) error {
	const maxWait = 10 * time.Minute
	pollCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	attempt := 0
	for {
		probeCtx, probeCancel := context.WithTimeout(pollCtx, 10*time.Second)
		probe := exec.CommandContext(probeCtx, "ssh",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=5",
			"-o", "StrictHostKeyChecking=accept-new",
			remoteHost, "echo ok",
		)
		err := probe.Run()
		probeCancel()

		if err == nil {
			return nil
		}

		attempt++
		delay := bootstrapPollDelay(attempt)
		fmt.Printf("\r  waiting... attempt %d (next in %s)  ", attempt, delay)

		timer := time.NewTimer(delay)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			fmt.Println()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("timed out waiting for remote (%s)", maxWait)
		case <-timer.C:
		}
	}
}

func bootstrapPollDelay(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d < 5*time.Second {
		d = 5 * time.Second
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func runBootstrapRemote(parentCtx context.Context, rc *config.ResolvedContext) error {
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
