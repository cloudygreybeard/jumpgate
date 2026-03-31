package cmd

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/bootstrap"
	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var setupRemoteInitCmd = &cobra.Command{
	Use:   "remote-init [CONTEXT]",
	Short: "Generate a bootstrap payload for initialising the remote end",
	Long: `Generate a compact base64 string containing the minimal remote-role
config for a given context. The string is designed to be copied via
clipboard (e.g. through Windows App) and pasted into the remote's
'jumpgate init --paste' command.

The payload contains only gate hostname, port, auth user, relay port,
and the context name — enough for the remote to open a relay tunnel.
Once the relay is up, 'jumpgate setup remote' can push the full
configuration over the tunnel.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctxName := flagContext
		if len(args) > 0 {
			ctxName = args[0]
		}

		cfg, rc, err := loadConfigAndContext(ctxName)
		if err != nil {
			return err
		}

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

		fmt.Println("=== Remote bootstrap payload ===")
		fmt.Println()
		fmt.Println("On the remote, install jumpgate then run:")
		fmt.Println()
		fmt.Println("  jumpgate bootstrap")
		fmt.Println()
		fmt.Println("When prompted, paste this string:")
		fmt.Println()
		fmt.Println(b64)
		fmt.Println()
		fmt.Println("(If sshd is already running, use 'jumpgate init --paste' + 'jumpgate connect' instead.)")

		return nil
	},
}

var setupRemoteCmd = &cobra.Command{
	Use:   "remote [CONTEXT]",
	Short: "Push full configuration and hooks to the remote end over SSH",
	Long: `Orchestrate full setup of the remote end over an established SSH
tunnel. This pushes the complete remote config, hooks, SSH snippets,
and Windows integration scripts, then runs 'jumpgate setup ssh' on the
remote to regenerate its SSH config.

Requires the remote to be reachable via 'ssh <context>'. Typically run
after 'jumpgate connect' has established the tunnel following an
initial bootstrap with 'jumpgate init --paste'.`,
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

		return runSetupRemote(cmd, rc)
	},
}

func init() {
	setupCmd.AddCommand(setupRemoteInitCmd)
	setupCmd.AddCommand(setupRemoteCmd)
}

func runSetupRemote(cmd *cobra.Command, rc *config.ResolvedContext) error {
	ctx := cmd.Context()
	remoteHost := rc.Derived.RemoteHost
	configDir := rc.Derived.ConfigDir
	verbose := flagVerbose > 0

	fmt.Printf("=== Setup remote [%s] via %s ===\n", rc.Name, remoteHost)

	// Verify remote is reachable
	stepStart := time.Now()
	fmt.Print("  Checking remote connectivity... ")
	probe := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", remoteHost, "echo ok")
	if err := probe.Run(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("remote %s is not reachable -- is the relay tunnel up? (jumpgate connect)", remoteHost)
	}
	printStepDone(verbose, stepStart)

	// Generate and push remote config
	stepStart = time.Now()
	fmt.Print("  Pushing remote config... ")
	remoteCfg := bootstrap.RemoteConfig(rc.Derived.ContextName, &rc.Context)
	tmpFile, err := os.CreateTemp("", "jumpgate-remote-config-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	data, err := marshalRemoteConfig(remoteCfg)
	if err != nil {
		return err
	}
	if _, err := tmpFile.Write(data); err != nil {
		return err
	}
	tmpFile.Close()

	if err := scpToRemote(ctx, tmpFile.Name(), remoteHost, ".config/jumpgate/config.yaml"); err != nil {
		return fmt.Errorf("pushing config: %w", err)
	}
	printStepDone(verbose, stepStart)

	// Push hooks (if any exist locally)
	hooksDir := filepath.Join(configDir, "hooks")
	if entries, err := os.ReadDir(hooksDir); err == nil && len(entries) > 0 {
		stepStart = time.Now()
		fmt.Print("  Pushing hooks... ")
		_ = sshRun(ctx, remoteHost, "mkdir -p .config/jumpgate/hooks")
		pushFiles(ctx, verbose, entries, hooksDir, ".config/jumpgate/hooks/", remoteHost)
		_ = sshRun(ctx, remoteHost, "chmod +x .config/jumpgate/hooks/*")
		printStepDone(verbose, stepStart)
	}

	// Push SSH snippets (if any exist locally)
	snippetsDir := filepath.Join(configDir, "ssh", "snippets")
	if entries, err := os.ReadDir(snippetsDir); err == nil && len(entries) > 0 {
		stepStart = time.Now()
		fmt.Print("  Pushing SSH snippets... ")
		_ = sshRun(ctx, remoteHost, "mkdir -p .config/jumpgate/ssh/snippets")
		pushFiles(ctx, verbose, entries, snippetsDir, ".config/jumpgate/ssh/snippets/", remoteHost)
		printStepDone(verbose, stepStart)
	}

	// Push Windows integration scripts (if any exist locally)
	windowsDir := filepath.Join(configDir, "windows")
	if entries, err := os.ReadDir(windowsDir); err == nil && len(entries) > 0 {
		stepStart = time.Now()
		fmt.Print("  Pushing Windows scripts... ")
		_ = sshRun(ctx, remoteHost, "mkdir -p jumpgate/windows")
		pushFiles(ctx, verbose, entries, windowsDir, "jumpgate/windows/", remoteHost)
		printStepDone(verbose, stepStart)
	}

	// Run jumpgate setup ssh on the remote.
	// Try the PATH first, then common install locations for fresh systems
	// where ~/bin may not be in PATH yet (e.g. bootstrap via PowerShell).
	stepStart = time.Now()
	fmt.Print("  Running 'jumpgate setup ssh' on remote... ")
	setupErr := sshRun(ctx, remoteHost, "jumpgate setup ssh")
	if setupErr != nil {
		slog.Debug("jumpgate not in PATH, trying ~/bin")
		// PowerShell needs & for path invocation; sh -c handles ~/bin natively
		setupErr = sshRun(ctx, remoteHost, `& "$HOME\bin\jumpgate" setup ssh`)
	}
	if setupErr != nil {
		setupErr = sshRun(ctx, remoteHost, "$HOME/bin/jumpgate setup ssh")
	}
	if setupErr != nil {
		return fmt.Errorf("remote setup ssh failed: %w", setupErr)
	}
	printStepDone(verbose, stepStart)

	// Install Windows shortcuts (if install-shortcut.ps1 exists on remote)
	if err := sshRun(ctx, remoteHost, "test -f jumpgate/windows/install-shortcut.ps1"); err == nil {
		stepStart = time.Now()
		fmt.Print("  Installing Windows shortcuts... ")
		installCmd := `winpath=$(wslpath -w ~/jumpgate/windows/install-shortcut.ps1) && /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -ExecutionPolicy Bypass -File "$winpath"`
		if err := sshRun(ctx, remoteHost, installCmd); err != nil {
			slog.Warn("Windows shortcut installation failed (non-fatal)", "error", err)
			fmt.Println("SKIPPED")
		} else {
			printStepDone(verbose, stepStart)
		}
	}

	fmt.Println()
	fmt.Printf("Remote [%s] is fully set up.\n", rc.Name)
	return nil
}

func printStepDone(verbose bool, start time.Time) {
	if verbose {
		fmt.Printf("OK (%s)\n", time.Since(start).Truncate(time.Millisecond))
	} else {
		fmt.Println("OK")
	}
}

func formatSize(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func pushFiles(ctx context.Context, verbose bool, entries []os.DirEntry, srcDir, dstPrefix, remoteHost string) {
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(srcDir, e.Name())
		dst := dstPrefix + e.Name()

		if verbose {
			info, _ := e.Info()
			size := ""
			if info != nil {
				size = " " + formatSize(info.Size())
			}
			fmt.Printf("\n    → %s%s... ", e.Name(), size)
		}

		t := time.Now()
		if err := scpToRemote(ctx, src, remoteHost, dst); err != nil {
			if verbose {
				fmt.Printf("FAILED (%s)\n", err)
			}
			slog.Warn("could not push file", "file", e.Name(), "error", err)
		} else if verbose {
			fmt.Printf("OK (%s)", time.Since(t).Truncate(time.Millisecond))
		}
	}
	if verbose {
		fmt.Println()
	}
}

func persistPort(rc *config.ResolvedContext) error {
	configPath := filepath.Join(rc.Derived.ConfigDir, "config.yaml")
	_, doc, err := config.LoadRaw(configPath)
	if err != nil {
		return err
	}
	if err := config.SetContext(doc, rc.Name, rc.Context); err != nil {
		return err
	}
	return config.SaveRaw(configPath, doc)
}

func marshalRemoteConfig(cfg *config.Config) ([]byte, error) {
	header := []byte("# Jumpgate remote config -- generated by: jumpgate setup remote\n\n")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshalling remote config: %w", err)
	}
	return append(header, data...), nil
}

func scpToRemote(ctx context.Context, localPath, remoteHost, remotePath string) error {
	dir := filepath.Dir(remotePath)
	if dir != "." {
		_ = sshRun(ctx, remoteHost, "mkdir -p "+dir)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "scp", "-O", "-q", localPath, remoteHost+":"+remotePath)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
		}
		return err
	}
	return nil
}

func sshRun(ctx context.Context, remoteHost, command string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", remoteHost, command)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			slog.Debug("ssh remote command failed", "command", command, "stderr", stderr.String())
		}
		return err
	}
	return nil
}
