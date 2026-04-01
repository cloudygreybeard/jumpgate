package cmd

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/bootstrap"
	"github.com/cloudygreybeard/jumpgate/internal/config"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
	"github.com/cloudygreybeard/jumpgate/internal/sshclient"
	"github.com/cloudygreybeard/jumpgate/internal/sshd"
	"github.com/cloudygreybeard/jumpgate/internal/transfer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var setupRemoteInitCmd = &cobra.Command{
	Use:   "remote-init [CONTEXT]",
	Short: "Generate a bootstrap payload for initialising the remote end",
	Long: `Generate a compact base64 string containing the minimal remote-role
config for a given context. The string is designed to be copied via
clipboard (e.g. through Windows App) and pasted when 'jumpgate bootstrap'
prompts on the remote.

Note: 'jumpgate bootstrap' on the local side generates this payload
automatically and waits for the remote — this command is an alternative
for scripted or manual workflows.

The payload contains only gate hostname, port, auth user, relay port,
and the context name — enough for the remote to open a relay tunnel.`,
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

Note: 'jumpgate bootstrap' on the local side runs this automatically
after detecting the remote — this command is an alternative for manual
or incremental updates.

Requires the remote to be reachable via 'ssh <context>'.`,
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

	// Check if the remote is running the bootstrap server (native transfer)
	// or regular sshd (legacy scp).
	isBootstrap := isBootstrapServer(ctx, remoteHost, rc.Context.UID)

	if isBootstrap && rc.Context.Remote.Key != "" {
		return runSetupRemoteBundled(ctx, rc, verbose)
	}

	// Verify remote is reachable
	stepStart := time.Now()
	fmt.Print("  Checking remote connectivity... ")
	probe := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "UserKnownHostsFile="+internalssh.KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
		remoteHost, "echo ok",
	)
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

	// WSL setup: if the remote has WSL available, copy config and generate
	// SSH config so 'jumpgate connect' works from WSL immediately.
	if err := setupRemoteWSL(ctx, remoteHost, verbose); err != nil {
		slog.Warn("WSL setup failed (non-fatal)", "error", err)
	}

	fmt.Println()
	fmt.Printf("Remote [%s] is fully set up.\n", rc.Name)
	return nil
}

// runSetupRemoteBundled pushes all config files to the remote in a single
// tar.gz bundle over a native Go SSH connection. Used when the remote is
// running the embedded bootstrap server. This replaces ~12 individual scp
// calls with a single streamed archive, reducing bootstrap time from
// minutes to seconds.
func runSetupRemoteBundled(ctx context.Context, rc *config.ResolvedContext, verbose bool) error {
	configDir := rc.Derived.ConfigDir
	gateHost := rc.Derived.GateHost
	relayPort := rc.Context.Relay.RemotePort
	keyPath := expandHome(rc.Context.Remote.Key)

	// 1. Connect via native Go SSH client
	stepStart := time.Now()
	fmt.Print("  Connecting (native)... ")
	client, err := sshclient.Dial(ctx, gateHost, relayPort, keyPath)
	if err != nil {
		fmt.Println("FAILED")
		slog.Debug("native SSH dial failed, falling back to scp", "error", err)
		return fmt.Errorf("native SSH connection failed: %w", err)
	}
	defer client.Close()
	printStepDone(verbose, stepStart)

	// 2. Build the bundle
	fmt.Print("  Building bundle... ")

	remoteCfg := bootstrap.RemoteConfig(rc.Derived.ContextName, &rc.Context)
	cfgData, err := marshalRemoteConfig(remoteCfg)
	if err != nil {
		return err
	}

	var files []transfer.FileEntry

	// Config yaml (from generated bytes, not disk)
	// We'll add it separately via CreateBundleFromBytes merged approach
	// Actually, let's collect everything and build a single bundle

	// Collect files from disk directories
	hooksDir := filepath.Join(configDir, "hooks")
	if hooksFiles, err := transfer.CollectFiles(hooksDir, ".config/jumpgate/hooks/"); err == nil {
		for i := range hooksFiles {
			hooksFiles[i].Mode = 0755
		}
		files = append(files, hooksFiles...)
	}

	snippetsDir := filepath.Join(configDir, "ssh", "snippets")
	if snippetFiles, err := transfer.CollectFiles(snippetsDir, ".config/jumpgate/ssh/snippets/"); err == nil {
		files = append(files, snippetFiles...)
	}

	windowsDir := filepath.Join(configDir, "windows")
	if winFiles, err := transfer.CollectFiles(windowsDir, "jumpgate/windows/"); err == nil {
		files = append(files, winFiles...)
	}

	// Build the archive in memory
	var bundle bytes.Buffer
	if err := transfer.CreateBundleMixed(&bundle, cfgData, ".config/jumpgate/config.yaml", files); err != nil {
		return fmt.Errorf("building bundle: %w", err)
	}

	bundleSize := bundle.Len()
	fileCount := len(files) + 1 // +1 for config.yaml
	if verbose {
		fmt.Printf("OK (%d files, %s)\n", fileCount, formatSize(int64(bundleSize)))
	} else {
		fmt.Println("OK")
	}

	// 3. Stream the bundle to the remote
	stepStart = time.Now()
	fmt.Print("  Pushing bundle... ")
	stdout, stderr, err := client.ExecStream(sshd.BundleCommand(), &bundle)
	if err != nil {
		fmt.Println("FAILED")
		slog.Debug("bundle push failed", "stdout", string(stdout), "stderr", string(stderr))
		return fmt.Errorf("pushing bundle: %w", err)
	}
	if verbose {
		fmt.Printf("OK (%s, %s)\n", strings.TrimSpace(string(stdout)), time.Since(stepStart).Truncate(time.Millisecond))
	} else {
		fmt.Println("OK")
	}

	// 4. Run jumpgate setup ssh on the remote
	stepStart = time.Now()
	fmt.Print("  Running 'jumpgate setup ssh' on remote... ")
	out, err := client.Exec("jumpgate setup ssh")
	if err != nil {
		slog.Debug("jumpgate not in PATH, trying alternatives", "output", string(out))
		out, err = client.Exec(`& "$HOME\bin\jumpgate" setup ssh`)
	}
	if err != nil {
		out, err = client.Exec("$HOME/bin/jumpgate setup ssh")
	}
	if err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("remote setup ssh failed: %w (output: %s)", err, string(out))
	}
	printStepDone(verbose, stepStart)

	// 5. Windows shortcuts (non-fatal)
	_, err = client.Exec("test -f jumpgate/windows/install-shortcut.ps1")
	if err == nil {
		stepStart = time.Now()
		fmt.Print("  Installing Windows shortcuts... ")
		installCmd := `winpath=$(wslpath -w ~/jumpgate/windows/install-shortcut.ps1) && /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -ExecutionPolicy Bypass -File "$winpath"`
		if _, err := client.Exec(installCmd); err != nil {
			slog.Warn("Windows shortcut installation failed (non-fatal)", "error", err)
			fmt.Println("SKIPPED")
		} else {
			printStepDone(verbose, stepStart)
		}
	}

	// 6. WSL setup via the native client
	if err := setupRemoteWSLNative(client, verbose); err != nil {
		slog.Warn("WSL setup failed (non-fatal)", "error", err)
	}

	fmt.Println()
	fmt.Printf("Remote [%s] is fully set up.\n", rc.Name)
	return nil
}

// isBootstrapServer probes whether the remote is running the bootstrap
// embedded server by checking the SSH banner via verbose ssh output.
func isBootstrapServer(ctx context.Context, remoteHost, expectedUID string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	probe := exec.CommandContext(probeCtx, "ssh",
		"-v",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "UserKnownHostsFile="+internalssh.KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
		remoteHost, "true",
	)
	var stderr bytes.Buffer
	probe.Stderr = &stderr
	_ = probe.Run()

	expectedBanner := sshd.BannerPrefix + "_" + expectedUID
	return strings.Contains(stderr.String(), expectedBanner)
}

// setupRemoteWSLNative performs WSL setup using the native SSH client
// connection instead of shelling out to ssh.
func setupRemoteWSLNative(client *sshclient.Client, verbose bool) error {
	fmt.Print("  Checking for WSL... ")

	out, err := client.Exec("wsl.exe --status >nul 2>&1")
	if err != nil {
		if _, err2 := client.Exec("command -v wsl.exe >/dev/null 2>&1"); err2 != nil {
			fmt.Println("no")
			return nil
		}
	}
	_ = out

	distroOut, _ := client.Exec(`wsl.exe --list --quiet`)
	distro := firstNonEmptyLine(string(distroOut))
	if distro == "" {
		fmt.Println("no distro installed (skipping)")
		return nil
	}
	fmt.Printf("found (%s)\n", distro)

	// Check if jumpgate is installed in WSL
	fmt.Print("  Checking jumpgate in WSL... ")
	if _, err := client.Exec(`wsl.exe -e sh -c "command -v jumpgate >/dev/null 2>&1"`); err != nil {
		fmt.Println("not installed")
		fmt.Println("    Hint: install jumpgate in WSL to enable WSL access:")
		fmt.Println("      wsl -e sh -c 'curl -fsSL https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.sh | sh'")
		fmt.Println("    or: wsl -e sh -c 'brew install cloudygreybeard/tap/jumpgate'")
		return nil
	}
	fmt.Println("OK")

	stepStart := time.Now()
	fmt.Print("  Copying config to WSL... ")
	copyScript := `wsl.exe -e sh -c "mkdir -p ~/.config/jumpgate && cp /mnt/c/Users/$USER/.config/jumpgate/config.yaml ~/.config/jumpgate/config.yaml"`
	if _, err := client.Exec(copyScript); err != nil {
		copyScript = `wsl.exe -e sh -c "WIN_HOME=$(wslpath -u \"$(cmd.exe /C 'echo %USERPROFILE%' 2>/dev/null | tr -d '\r')\") && mkdir -p ~/.config/jumpgate && cp \"$WIN_HOME/.config/jumpgate/config.yaml\" ~/.config/jumpgate/config.yaml"`
		if _, err := client.Exec(copyScript); err != nil {
			return fmt.Errorf("copying config to WSL: %w", err)
		}
	}
	printStepDone(verbose, stepStart)

	stepStart = time.Now()
	fmt.Print("  Copying hooks to WSL... ")
	hooksScript := `wsl.exe -e sh -c "WIN_HOME=$(wslpath -u \"$(cmd.exe /C 'echo %USERPROFILE%' 2>/dev/null | tr -d '\r')\") && [ -d \"$WIN_HOME/.config/jumpgate/hooks\" ] && mkdir -p ~/.config/jumpgate/hooks && cp -r \"$WIN_HOME/.config/jumpgate/hooks/\"* ~/.config/jumpgate/hooks/ && chmod +x ~/.config/jumpgate/hooks/* 2>/dev/null; true"`
	if _, err := client.Exec(hooksScript); err != nil {
		slog.Debug("WSL hooks copy failed (non-fatal)", "error", err)
		fmt.Println("SKIPPED")
	} else {
		printStepDone(verbose, stepStart)
	}

	stepStart = time.Now()
	fmt.Print("  Copying SSH snippets to WSL... ")
	snippetsScript := `wsl.exe -e sh -c "WIN_HOME=$(wslpath -u \"$(cmd.exe /C 'echo %USERPROFILE%' 2>/dev/null | tr -d '\r')\") && [ -d \"$WIN_HOME/.config/jumpgate/ssh/snippets\" ] && mkdir -p ~/.config/jumpgate/ssh/snippets && cp -r \"$WIN_HOME/.config/jumpgate/ssh/snippets/\"* ~/.config/jumpgate/ssh/snippets/; true"`
	if _, err := client.Exec(snippetsScript); err != nil {
		slog.Debug("WSL snippets copy failed (non-fatal)", "error", err)
		fmt.Println("SKIPPED")
	} else {
		printStepDone(verbose, stepStart)
	}

	stepStart = time.Now()
	fmt.Print("  Running 'jumpgate setup ssh' in WSL... ")
	if _, err := client.Exec(`wsl.exe -e jumpgate setup ssh`); err != nil {
		if _, err2 := client.Exec(`wsl.exe -e sh -c "$HOME/bin/jumpgate setup ssh"`); err2 != nil {
			return fmt.Errorf("WSL setup ssh failed: %w", err2)
		}
	}
	printStepDone(verbose, stepStart)

	return nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func setupRemoteWSL(ctx context.Context, remoteHost string, verbose bool) error {
	fmt.Print("  Checking for WSL... ")

	// Check if wsl.exe exists on the remote
	if err := sshRun(ctx, remoteHost, "wsl.exe --status >nul 2>&1"); err != nil {
		// Try sh-compatible syntax as well (if connected to WSL already)
		if err2 := sshRun(ctx, remoteHost, "command -v wsl.exe >/dev/null 2>&1"); err2 != nil {
			fmt.Println("no")
			return nil
		}
	}

	// Get the WSL distro name
	distroOut, _ := sshRunOutput(ctx, remoteHost, `wsl.exe --list --quiet`)
	distro := firstNonEmptyLine(distroOut)
	if distro == "" {
		fmt.Println("no distro installed (skipping)")
		return nil
	}
	fmt.Printf("found (%s)\n", distro)

	// Check if jumpgate is installed in WSL
	fmt.Print("  Checking jumpgate in WSL... ")
	if err := sshRun(ctx, remoteHost, `wsl.exe -e sh -c "command -v jumpgate >/dev/null 2>&1"`); err != nil {
		fmt.Println("not installed")
		fmt.Println("    Hint: install jumpgate in WSL to enable WSL access:")
		fmt.Println("      wsl -e sh -c 'curl -fsSL https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.sh | sh'")
		fmt.Println("    or: wsl -e sh -c 'brew install cloudygreybeard/tap/jumpgate'")
		return nil
	}
	fmt.Println("OK")

	// Copy config.yaml to WSL
	stepStart := time.Now()
	fmt.Print("  Copying config to WSL... ")
	copyScript := `wsl.exe -e sh -c "mkdir -p ~/.config/jumpgate && cp /mnt/c/Users/$USER/.config/jumpgate/config.yaml ~/.config/jumpgate/config.yaml"`
	if err := sshRun(ctx, remoteHost, copyScript); err != nil {
		// Try with explicit Windows username from USERPROFILE
		copyScript = `wsl.exe -e sh -c "WIN_HOME=$(wslpath -u \"$(cmd.exe /C 'echo %USERPROFILE%' 2>/dev/null | tr -d '\r')\") && mkdir -p ~/.config/jumpgate && cp \"$WIN_HOME/.config/jumpgate/config.yaml\" ~/.config/jumpgate/config.yaml"`
		if err := sshRun(ctx, remoteHost, copyScript); err != nil {
			return fmt.Errorf("copying config to WSL: %w", err)
		}
	}
	printStepDone(verbose, stepStart)

	// Copy hooks to WSL (if they exist)
	stepStart = time.Now()
	fmt.Print("  Copying hooks to WSL... ")
	hooksScript := `wsl.exe -e sh -c "WIN_HOME=$(wslpath -u \"$(cmd.exe /C 'echo %USERPROFILE%' 2>/dev/null | tr -d '\r')\") && [ -d \"$WIN_HOME/.config/jumpgate/hooks\" ] && mkdir -p ~/.config/jumpgate/hooks && cp -r \"$WIN_HOME/.config/jumpgate/hooks/\"* ~/.config/jumpgate/hooks/ && chmod +x ~/.config/jumpgate/hooks/* 2>/dev/null; true"`
	if err := sshRun(ctx, remoteHost, hooksScript); err != nil {
		slog.Debug("WSL hooks copy failed (non-fatal)", "error", err)
		fmt.Println("SKIPPED")
	} else {
		printStepDone(verbose, stepStart)
	}

	// Copy SSH snippets to WSL (if they exist)
	stepStart = time.Now()
	fmt.Print("  Copying SSH snippets to WSL... ")
	snippetsScript := `wsl.exe -e sh -c "WIN_HOME=$(wslpath -u \"$(cmd.exe /C 'echo %USERPROFILE%' 2>/dev/null | tr -d '\r')\") && [ -d \"$WIN_HOME/.config/jumpgate/ssh/snippets\" ] && mkdir -p ~/.config/jumpgate/ssh/snippets && cp -r \"$WIN_HOME/.config/jumpgate/ssh/snippets/\"* ~/.config/jumpgate/ssh/snippets/; true"`
	if err := sshRun(ctx, remoteHost, snippetsScript); err != nil {
		slog.Debug("WSL snippets copy failed (non-fatal)", "error", err)
		fmt.Println("SKIPPED")
	} else {
		printStepDone(verbose, stepStart)
	}

	// Run jumpgate setup ssh inside WSL
	stepStart = time.Now()
	fmt.Print("  Running 'jumpgate setup ssh' in WSL... ")
	if err := sshRun(ctx, remoteHost, `wsl.exe -e jumpgate setup ssh`); err != nil {
		if err2 := sshRun(ctx, remoteHost, `wsl.exe -e sh -c "$HOME/bin/jumpgate setup ssh"`); err2 != nil {
			return fmt.Errorf("WSL setup ssh failed: %w", err2)
		}
	}
	printStepDone(verbose, stepStart)

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
	cmd := exec.CommandContext(ctx, "scp",
		"-O", "-q",
		"-o", "UserKnownHostsFile="+internalssh.KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
		localPath, remoteHost+":"+remotePath,
	)
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
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "UserKnownHostsFile="+internalssh.KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
		remoteHost, command,
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			slog.Debug("ssh remote command failed", "command", command, "stderr", stderr.String())
		}
		return err
	}
	return nil
}

func sshRunOutput(ctx context.Context, remoteHost, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "UserKnownHostsFile="+internalssh.KnownHostsFile(),
		"-o", "StrictHostKeyChecking=accept-new",
		remoteHost, command,
	)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		// wsl.exe output is sometimes UTF-16LE; strip NUL bytes
		line = strings.ReplaceAll(line, "\x00", "")
		if line != "" {
			return line
		}
	}
	return ""
}
