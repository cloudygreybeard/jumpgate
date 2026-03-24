package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

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

		rc, err := loadResolvedContext(ctxName)
		if err != nil {
			return err
		}

		remoteCfg := bootstrap.RemoteConfig(rc.Derived.ContextName, &rc.Context)
		b64, err := bootstrap.Encode(remoteCfg)
		if err != nil {
			return fmt.Errorf("encoding bootstrap payload: %w", err)
		}

		fmt.Println("=== Remote bootstrap payload ===")
		fmt.Println()
		fmt.Println("On the remote, install jumpgate then run:")
		fmt.Println()
		fmt.Println("  jumpgate init --paste")
		fmt.Println()
		fmt.Println("When prompted, paste this string:")
		fmt.Println()
		fmt.Println(b64)
		fmt.Println()
		fmt.Printf("Then run 'jumpgate connect' on the remote to open the relay.\n")

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

	fmt.Printf("=== Setup remote [%s] via %s ===\n", rc.Name, remoteHost)

	// Verify remote is reachable
	fmt.Print("  Checking remote connectivity... ")
	probe := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", remoteHost, "true")
	if err := probe.Run(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("remote %s is not reachable -- is the relay tunnel up? (jumpgate connect)", remoteHost)
	}
	fmt.Println("OK")

	// Generate and push remote config
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
	fmt.Println("OK")

	// Push hooks (if any exist locally)
	hooksDir := filepath.Join(configDir, "hooks")
	if entries, err := os.ReadDir(hooksDir); err == nil && len(entries) > 0 {
		fmt.Print("  Pushing hooks... ")
		_ = sshRun(ctx, remoteHost, "mkdir -p .config/jumpgate/hooks")
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := filepath.Join(hooksDir, e.Name())
			dst := ".config/jumpgate/hooks/" + e.Name()
			if err := scpToRemote(ctx, src, remoteHost, dst); err != nil {
				slog.Warn("could not push hook", "file", e.Name(), "error", err)
			}
		}
		// Make hooks executable on remote
		_ = sshRun(ctx, remoteHost, "chmod +x .config/jumpgate/hooks/*")
		fmt.Println("OK")
	}

	// Push SSH snippets (if any exist locally)
	snippetsDir := filepath.Join(configDir, "ssh", "snippets")
	if entries, err := os.ReadDir(snippetsDir); err == nil && len(entries) > 0 {
		fmt.Print("  Pushing SSH snippets... ")
		_ = sshRun(ctx, remoteHost, "mkdir -p .config/jumpgate/ssh/snippets")
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := filepath.Join(snippetsDir, e.Name())
			dst := ".config/jumpgate/ssh/snippets/" + e.Name()
			if err := scpToRemote(ctx, src, remoteHost, dst); err != nil {
				slog.Warn("could not push snippet", "file", e.Name(), "error", err)
			}
		}
		fmt.Println("OK")
	}

	// Push Windows integration scripts (if any exist locally)
	windowsDir := filepath.Join(configDir, "windows")
	if entries, err := os.ReadDir(windowsDir); err == nil && len(entries) > 0 {
		fmt.Print("  Pushing Windows scripts... ")
		_ = sshRun(ctx, remoteHost, "mkdir -p jumpgate/windows")
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := filepath.Join(windowsDir, e.Name())
			dst := "jumpgate/windows/" + e.Name()
			if err := scpToRemote(ctx, src, remoteHost, dst); err != nil {
				slog.Warn("could not push windows script", "file", e.Name(), "error", err)
			}
		}
		fmt.Println("OK")
	}

	// Run jumpgate setup ssh on the remote
	fmt.Print("  Running 'jumpgate setup ssh' on remote... ")
	if err := sshRun(ctx, remoteHost, "jumpgate setup ssh"); err != nil {
		return fmt.Errorf("remote setup ssh failed: %w", err)
	}
	fmt.Println("OK")

	// Install Windows shortcuts (if install-shortcut.ps1 exists on remote)
	if err := sshRun(ctx, remoteHost, "test -f jumpgate/windows/install-shortcut.ps1"); err == nil {
		fmt.Print("  Installing Windows shortcuts... ")
		installCmd := `winpath=$(wslpath -w ~/jumpgate/windows/install-shortcut.ps1) && /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -ExecutionPolicy Bypass -File "$winpath"`
		if err := sshRun(ctx, remoteHost, installCmd); err != nil {
			slog.Warn("Windows shortcut installation failed (non-fatal)", "error", err)
			fmt.Println("SKIPPED")
		} else {
			fmt.Println("OK")
		}
	}

	fmt.Println()
	fmt.Printf("Remote [%s] is fully set up.\n", rc.Name)
	return nil
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
	cmd := exec.CommandContext(ctx, "scp", "-q", localPath, remoteHost+":"+remotePath)
	return cmd.Run()
}

func sshRun(ctx context.Context, remoteHost, command string) error {
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", remoteHost, command)
	return cmd.Run()
}
