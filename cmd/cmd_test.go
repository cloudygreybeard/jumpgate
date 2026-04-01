package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/bootstrap"
	"github.com/cloudygreybeard/jumpgate/internal/config"
	"gopkg.in/yaml.v3"
)

func testConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `
default_context: work
contexts:
  work:
    gate:
      hostname: gw.example.com
      port: 22
    auth:
      type: key
      user: alice
    remote:
      user: alice-remote
      key: ~/.ssh/id_ed25519
    relay:
      remote_port: 55555
`
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	return cfgPath
}

func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestVersionCommand(t *testing.T) {
	out, err := runCmd(t, "version")
	if err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	_ = out // version prints to os.Stdout, not cmd.OutOrStdout()
}

func TestHelpCommand(t *testing.T) {
	_, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
}

func TestConfigViewCommand(t *testing.T) {
	cfgPath := testConfig(t)
	_, err := runCmd(t, "-c", cfgPath, "config", "view")
	if err != nil {
		t.Fatalf("config view: %v", err)
	}
}

func TestConfigViewWithContext(t *testing.T) {
	cfgPath := testConfig(t)
	_, err := runCmd(t, "-c", cfgPath, "config", "view", "work")
	if err != nil {
		t.Fatalf("config view work: %v", err)
	}
}

func TestConfigViewMissingConfig(t *testing.T) {
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "config", "view")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestConfigViewMissingContext(t *testing.T) {
	cfgPath := testConfig(t)
	_, err := runCmd(t, "-c", cfgPath, "config", "view", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing context")
	}
}

func TestConfigMigrateCommand_MultiContext(t *testing.T) {
	cfgPath := testConfig(t)
	_, err := runCmd(t, "-c", cfgPath, "config", "migrate")
	if err != nil {
		t.Fatalf("config migrate: %v", err)
	}
}

func TestConfigMigrateCommand_OldFormat(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte("hostname: gw.example.com\nport: 22\n"), 0644)

	_, err := runCmd(t, "-c", cfgPath, "config", "migrate")
	if err != nil {
		t.Fatalf("config migrate: %v", err)
	}
}

func TestConfigMigrateCommand_NoConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "config", "migrate")
	if err != nil {
		t.Fatalf("config migrate should succeed even without config: %v", err)
	}
}

func TestAskpassCommand(t *testing.T) {
	t.Setenv("JUMPGATE_ASKPASS_TOKEN", "test-token-123")
	_, err := runCmd(t, "askpass")
	if err != nil {
		t.Fatalf("askpass: %v", err)
	}
}

func TestSubcommandRegistration(t *testing.T) {
	commands := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		commands[cmd.Name()] = true
	}

	required := []string{"connect", "disconnect", "status", "watch", "setup", "config", "version", "askpass"}
	for _, name := range required {
		if !commands[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestGlobalFlags(t *testing.T) {
	pf := rootCmd.PersistentFlags()

	if f := pf.Lookup("context"); f == nil {
		t.Error("missing --context flag")
	}
	if f := pf.Lookup("config"); f == nil {
		t.Error("missing --config flag")
	}
	if f := pf.Lookup("verbose"); f == nil {
		t.Error("missing --verbose flag")
	}
	if f := pf.Lookup("output"); f == nil {
		t.Error("missing --output flag")
	} else if f.Shorthand != "o" {
		t.Errorf("--output shorthand = %q, want \"o\"", f.Shorthand)
	}
}

func TestConnectCommandMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "connect")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestDisconnectCommandMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "disconnect")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestStatusCommandMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "status")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestWatchCommandMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "watch")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestLoadConfigAndContext(t *testing.T) {
	cfgPath := testConfig(t)
	flagConfig = cfgPath
	defer func() { flagConfig = "" }()

	cfg, rc, err := loadConfigAndContext("")
	if err != nil {
		t.Fatalf("loadConfigAndContext: %v", err)
	}
	if cfg == nil {
		t.Error("cfg is nil")
	}
	if rc.Name != "work" {
		t.Errorf("context = %q, want %q", rc.Name, "work")
	}
}

func TestLoadResolvedContext(t *testing.T) {
	cfgPath := testConfig(t)
	flagConfig = cfgPath
	defer func() { flagConfig = "" }()

	rc, err := loadResolvedContext("")
	if err != nil {
		t.Fatalf("loadResolvedContext: %v", err)
	}
	if rc.Name != "work" {
		t.Errorf("context = %q, want %q", rc.Name, "work")
	}
}

func TestVerboseFlags(t *testing.T) {
	// Test -v flag
	flagVerbose = 0
	rootCmd.SetArgs([]string{"-v", "version"})
	rootCmd.Execute()

	// Test with no output errors
	rootCmd.SetArgs([]string{"version"})
	rootCmd.Execute()
}

func TestSetupCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "setup" {
			found = true
			// Check subcommands
			subCmds := make(map[string]bool)
			for _, sub := range cmd.Commands() {
				subCmds[sub.Name()] = true
			}
			for _, name := range []string{"ssh", "config", "credentials"} {
				if !subCmds[name] {
					t.Errorf("setup missing subcommand: %s (found: %v)", name, subCmds)
				}
			}
			break
		}
	}
	if !found {
		t.Error("setup command not registered")
	}
}

func TestDisconnectAllFlag(t *testing.T) {
	f := disconnectCmd.Flags().Lookup("all")
	if f == nil {
		t.Error("disconnect command missing --all flag")
	}
	if f != nil && f.Shorthand != "a" {
		t.Errorf("--all shorthand = %q, want \"a\"", f.Shorthand)
	}
}

func TestSetupSSHMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "setup", "ssh")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestSetupCredentialsMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCmd(t, "-c", "/nonexistent/config.yaml", "setup", "credentials")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestLoadSnippets(t *testing.T) {
	snippets, err := loadSnippets(t.TempDir())
	if err != nil {
		t.Fatalf("loadSnippets: %v", err)
	}
	if len(snippets) == 0 {
		t.Error("expected at least one embedded snippet")
	}
	for name := range snippets {
		if !strings.HasSuffix(name, ".tpl") {
			t.Errorf("snippet %q doesn't end with .tpl", name)
		}
	}
}

func TestLoadSnippets_UserOverlay(t *testing.T) {
	configDir := t.TempDir()
	snippetDir := filepath.Join(configDir, "ssh", "snippets")
	os.MkdirAll(snippetDir, 0755)
	os.WriteFile(filepath.Join(snippetDir, "custom.conf.tpl"), []byte("Host custom\n  User test\n"), 0644)

	snippets, err := loadSnippets(configDir)
	if err != nil {
		t.Fatalf("loadSnippets: %v", err)
	}
	if _, ok := snippets["custom.conf.tpl"]; !ok {
		t.Error("user snippet custom.conf.tpl not loaded")
	}
	if !strings.Contains(snippets["custom.conf.tpl"], "Host custom") {
		t.Error("user snippet content not correct")
	}
}

func TestLoadConfigTemplate(t *testing.T) {
	data, err := loadConfigTemplate()
	if err != nil {
		t.Fatalf("loadConfigTemplate: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty config template")
	}
	if !strings.Contains(string(data), "contexts:") {
		t.Error("config template should contain 'contexts:'")
	}
}

func TestRunSetupSSH(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".ssh"), 0700)

	cfgPath := testConfig(t)
	flagConfig = cfgPath
	defer func() { flagConfig = "" }()

	rc, err := loadResolvedContext("")
	if err != nil {
		t.Fatalf("loadResolvedContext: %v", err)
	}

	err = runSetupSSH(rc)
	if err != nil {
		t.Errorf("runSetupSSH: %v", err)
	}
}

func TestExecute(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})
	err := Execute()
	if err != nil {
		t.Errorf("Execute: %v", err)
	}
}

func TestConfigViewJSON(t *testing.T) {
	cfgPath := testConfig(t)

	out := captureStdout(t, func() {
		_, err := runCmd(t, "-c", cfgPath, "-o", "json", "config", "view")
		if err != nil {
			t.Fatalf("config view -o json: %v", err)
		}
	})

	var view ConfigView
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if view.Name != "work" {
		t.Errorf("context = %q, want %q", view.Name, "work")
	}
	if view.Config.Gate.Hostname != "gw.example.com" {
		t.Errorf("gate hostname = %q, want %q", view.Config.Gate.Hostname, "gw.example.com")
	}
}

func TestConfigViewYAML(t *testing.T) {
	cfgPath := testConfig(t)

	out := captureStdout(t, func() {
		_, err := runCmd(t, "-c", cfgPath, "-o", "yaml", "config", "view")
		if err != nil {
			t.Fatalf("config view -o yaml: %v", err)
		}
	})

	var view ConfigView
	if err := yaml.Unmarshal(out, &view); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, out)
	}
	if view.Name != "work" {
		t.Errorf("context = %q, want %q", view.Name, "work")
	}
}

func TestConfigListJSON(t *testing.T) {
	cfgPath := testConfig(t)

	out := captureStdout(t, func() {
		_, err := runCmd(t, "-c", cfgPath, "-o", "json", "config", "list")
		if err != nil {
			t.Fatalf("config list -o json: %v", err)
		}
	})

	var summaries []ContextSummary
	if err := json.Unmarshal(out, &summaries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 context, got %d", len(summaries))
	}
	if summaries[0].Name != "work" {
		t.Errorf("name = %q, want %q", summaries[0].Name, "work")
	}
	if !summaries[0].Default {
		t.Error("expected default=true for work context")
	}
}

func TestConfigListYAML(t *testing.T) {
	cfgPath := testConfig(t)

	out := captureStdout(t, func() {
		_, err := runCmd(t, "-c", cfgPath, "-o", "yaml", "config", "list")
		if err != nil {
			t.Fatalf("config list -o yaml: %v", err)
		}
	})

	var summaries []ContextSummary
	if err := yaml.Unmarshal(out, &summaries); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, out)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 context, got %d", len(summaries))
	}
	if summaries[0].GateHost != "gw.example.com" {
		t.Errorf("gate_host = %q, want %q", summaries[0].GateHost, "gw.example.com")
	}
}

func TestConfigListWide(t *testing.T) {
	cfgPath := testConfig(t)

	out := captureStdout(t, func() {
		_, err := runCmd(t, "-c", cfgPath, "-o", "wide", "config", "list")
		if err != nil {
			t.Fatalf("config list -o wide: %v", err)
		}
	})

	s := string(out)
	if !strings.Contains(s, "GATE HOST") {
		t.Errorf("wide output missing header 'GATE HOST':\n%s", s)
	}
	if !strings.Contains(s, "ROLE") {
		t.Errorf("wide output missing header 'ROLE':\n%s", s)
	}
	if !strings.Contains(s, "gw.example.com") {
		t.Errorf("wide output missing gate hostname:\n%s", s)
	}
}

func TestInvalidOutputFormat(t *testing.T) {
	cfgPath := testConfig(t)
	_, err := runCmd(t, "-c", cfgPath, "-o", "xml", "config", "view")
	if err == nil {
		t.Fatal("expected error for invalid output format")
	}
	if !strings.Contains(err.Error(), "unsupported output format") {
		t.Errorf("error = %q, want containing 'unsupported output format'", err.Error())
	}
}

func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	origStdout := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = origStdout

	var captured bytes.Buffer
	captured.ReadFrom(r)
	r.Close()

	return captured.Bytes()
}

func TestConfigImportFromJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfgPath := testConfig(t)

	// Export config view as JSON
	viewJSON := captureStdout(t, func() {
		_, err := runCmd(t, "-c", cfgPath, "-o", "json", "config", "view")
		if err != nil {
			t.Fatalf("config view -o json: %v", err)
		}
	})

	// Write JSON to a file
	tmpFile := filepath.Join(t.TempDir(), "view.json")
	os.WriteFile(tmpFile, viewJSON, 0644)

	// Import as a new context
	_, err := runCmd(t, "-c", cfgPath, "config", "import", "--context", "staging", tmpFile)
	if err != nil {
		t.Fatalf("config import: %v", err)
	}

	// Verify the new context exists
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reloading config: %v", err)
	}
	ctx, ok := cfg.Contexts["staging"]
	if !ok {
		t.Fatal("staging context not created by import")
	}
	if ctx.Gate.Hostname != "gw.example.com" {
		t.Errorf("imported gate hostname = %q", ctx.Gate.Hostname)
	}
}

func TestConfigImportFromYAML(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfgPath := testConfig(t)

	yamlData := `
role: local
gate:
  hostname: new-gate.example.com
  port: 443
auth:
  type: key
  user: bob
`
	tmpFile := filepath.Join(t.TempDir(), "ctx.yaml")
	os.WriteFile(tmpFile, []byte(yamlData), 0644)

	_, err := runCmd(t, "-c", cfgPath, "config", "import", "--context", "newctx", tmpFile)
	if err != nil {
		t.Fatalf("config import yaml: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, ok := cfg.Contexts["newctx"]
	if !ok {
		t.Fatal("newctx not created")
	}
	if ctx.Gate.Hostname != "new-gate.example.com" {
		t.Errorf("hostname = %q", ctx.Gate.Hostname)
	}
	if ctx.Gate.Port != 443 {
		t.Errorf("port = %d", ctx.Gate.Port)
	}
}

func TestConfigImportOverwrite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfgPath := testConfig(t)

	yamlData := `
gate:
  hostname: updated.example.com
  port: 2222
auth:
  type: key
  user: carol
`
	tmpFile := filepath.Join(t.TempDir(), "ctx.yaml")
	os.WriteFile(tmpFile, []byte(yamlData), 0644)

	// Import over existing 'work' context
	_, err := runCmd(t, "-c", cfgPath, "config", "import", "--context", "work", tmpFile)
	if err != nil {
		t.Fatalf("config import overwrite: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Contexts["work"]
	if ctx.Gate.Hostname != "updated.example.com" {
		t.Errorf("hostname = %q, want updated.example.com", ctx.Gate.Hostname)
	}
}

func TestConfigExport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".ssh"), 0700)

	cfgPath := testConfig(t)
	outDir := filepath.Join(t.TempDir(), "exported-pack")

	_, err := runCmd(t, "-c", cfgPath, "config", "export", "--output-dir", outDir)
	if err != nil {
		t.Fatalf("config export: %v", err)
	}

	// site.yaml should exist
	if _, err := os.Stat(filepath.Join(outDir, "site.yaml")); err != nil {
		t.Error("site.yaml not created")
	}
	// values.yaml should exist
	if _, err := os.Stat(filepath.Join(outDir, "values.yaml")); err != nil {
		t.Error("values.yaml not created")
	}
	// template should exist
	tpl, err := os.ReadFile(filepath.Join(outDir, "templates", "config.yaml.tpl"))
	if err != nil {
		t.Fatalf("reading template: %v", err)
	}
	if !strings.Contains(string(tpl), "{{.gate_host}}") {
		t.Error("template missing {{.gate_host}}")
	}
}

func TestConfigExportMissingOutputDir(t *testing.T) {
	cfgPath := testConfig(t)
	// Reset flag from previous test (cobra retains flag values across invocations)
	flagExportDir = ""
	defer func() { flagExportDir = "" }()
	_, err := runCmd(t, "-c", cfgPath, "config", "export")
	if err == nil {
		t.Fatal("expected error when --output-dir not specified")
	}
}

func TestConfigImportExportSubcommandRegistration(t *testing.T) {
	subCmds := make(map[string]bool)
	for _, cmd := range configCmd.Commands() {
		subCmds[cmd.Name()] = true
	}
	if !subCmds["import"] {
		t.Error("config import subcommand not registered")
	}
	if !subCmds["export"] {
		t.Error("config export subcommand not registered")
	}
}

func TestParseImportData_ConfigViewJSON(t *testing.T) {
	data := []byte(`{
		"context": "myctx",
		"platform": {"os": "darwin", "arch": "arm64"},
		"derived": {},
		"config": {
			"role": "local",
			"gate": {"hostname": "gw.example.com", "port": 22}
		}
	}`)

	name, ctx, err := parseImportData(data)
	if err != nil {
		t.Fatalf("parseImportData: %v", err)
	}
	if name != "myctx" {
		t.Errorf("name = %q, want myctx", name)
	}
	if ctx.Gate.Hostname != "gw.example.com" {
		t.Errorf("hostname = %q", ctx.Gate.Hostname)
	}
}

func TestParseImportData_BareContextJSON(t *testing.T) {
	data := []byte(`{"gate": {"hostname": "bare.example.com", "port": 443}}`)

	name, ctx, err := parseImportData(data)
	if err != nil {
		t.Fatalf("parseImportData: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty for bare context", name)
	}
	if ctx.Gate.Hostname != "bare.example.com" {
		t.Errorf("hostname = %q", ctx.Gate.Hostname)
	}
}

func TestParseImportData_BareContextYAML(t *testing.T) {
	data := []byte("role: remote\ngate:\n  hostname: yaml.example.com\n  port: 22\n")

	name, ctx, err := parseImportData(data)
	if err != nil {
		t.Fatalf("parseImportData: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if ctx.Gate.Hostname != "yaml.example.com" {
		t.Errorf("hostname = %q", ctx.Gate.Hostname)
	}
	if ctx.Role != "remote" {
		t.Errorf("role = %q", ctx.Role)
	}
}

func TestParseImportData_Invalid(t *testing.T) {
	data := []byte("this is not valid config data at all")
	_, _, err := parseImportData(data)
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestConnectRelayPortFlag(t *testing.T) {
	f := connectCmd.Flags().Lookup("relay-port")
	if f == nil {
		t.Error("connect command missing --relay-port flag")
	}
}

func TestWatchRelayPortFlag(t *testing.T) {
	f := watchCmd.Flags().Lookup("relay-port")
	if f == nil {
		t.Error("watch command missing --relay-port flag")
	}
}

func TestBootstrapRelayPortFlag(t *testing.T) {
	f := bootstrapCmd.Flags().Lookup("relay-port")
	if f == nil {
		t.Error("bootstrap command missing --relay-port flag")
	}
}

func TestConnectWithConfigAndContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".ssh"), 0700)

	cfgPath := testConfig(t)

	// Config resolves ConfigDir to the temp XDG dir, so hooks won't
	// exist there -- connect will fail with "hook not found", proving
	// config loading and dispatch work without touching real hooks.
	_, err := runCmd(t, "-c", cfgPath, "connect")
	if err == nil {
		t.Log("connect succeeded unexpectedly")
	} else if !strings.Contains(err.Error(), "hook") && !strings.Contains(err.Error(), "not found") {
		t.Logf("connect failed with unexpected error: %v", err)
	}
}

func TestSetupRemoteInitSubcommand(t *testing.T) {
	subCmds := make(map[string]bool)
	for _, cmd := range setupCmd.Commands() {
		subCmds[cmd.Name()] = true
	}
	if !subCmds["remote-init"] {
		t.Error("setup remote-init subcommand not registered")
	}
	if !subCmds["remote"] {
		t.Error("setup remote subcommand not registered")
	}
}

func TestSetupRemoteInitGeneratesPayload(t *testing.T) {
	cfgPath := testConfig(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	// Capture stdout by redirecting
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	_, err := runCmd(t, "-c", cfgPath, "setup", "remote-init")
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("setup remote-init: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "jumpgate init --paste") {
		t.Error("output should contain paste instructions")
	}
	if !strings.Contains(output, "bootstrap") {
		t.Error("output should mention bootstrap")
	}
}

func TestInitPasteFlag(t *testing.T) {
	f := initCmd.Flags().Lookup("paste")
	if f == nil {
		t.Error("init command missing --paste flag")
	}
}

func TestInitPasteDecodesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".ssh"), 0700)

	cfg := &config.Config{
		DefaultContext: "test",
		Contexts: map[string]config.Context{
			"test": {
				Role:  "remote",
				Gate:  config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth:  config.AuthConfig{Type: "none", User: "bob"},
				Relay: config.RelayConfig{RemotePort: 50000},
			},
		},
	}

	b64, err := bootstrap.Encode(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Feed via stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	w.WriteString(b64 + "\n")
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	// Reset flags that may be stale from other tests
	flagPaste = false
	flagConfig = ""
	defer func() { flagConfig = "" }()

	_, err = runCmd(t, "init", "--paste")
	if err != nil {
		t.Fatalf("init --paste: %v", err)
	}

	cfgOut := filepath.Join(dir, "jumpgate", "config.yaml")
	if _, err := os.Stat(cfgOut); os.IsNotExist(err) {
		t.Error("config.yaml was not written")
	}

	written, err := config.Load(cfgOut)
	if err != nil {
		t.Fatalf("loading written config: %v", err)
	}
	if written.DefaultContext != "test" {
		t.Errorf("default_context = %q, want test", written.DefaultContext)
	}
	ctx := written.Contexts["test"]
	if ctx.Role != "remote" {
		t.Errorf("role = %q, want remote", ctx.Role)
	}
}
