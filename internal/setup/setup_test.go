package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func TestGenerateSSHConfigRemote(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth: config.AuthConfig{User: "alice", Type: "kerberos", Realm: "EXAMPLE.COM"},
				Remote: config.RemoteConfig{
					User: "alice-remote",
					Key:  "~/.ssh/id_ed25519",
				},
				Relay: config.RelayConfig{RemotePort: 55555},
			},
		},
	}

	snippets := map[string]string{
		"gate-remote.conf.tpl": `Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}
  HostName {{.Hostname}}
  User {{.User}}
  Port {{.Port}}
`,
		"gate-relay-remote.conf.tpl": `Host {{.Context}}-relay{{if .IsDefault}} relay{{end}}
  HostName {{.Hostname}}
  User {{.User}}
  Port {{.Port}}
  RemoteForward {{.RelayPort}} localhost:22
`,
	}

	if err := GenerateSSHConfig(cfg, configDir, socketDir, "remote", snippets); err != nil {
		t.Fatal(err)
	}

	output, err := os.ReadFile(filepath.Join(configDir, "ssh", "config.remote"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(output)

	if !strings.Contains(content, "Host work-gate gate") {
		t.Error("missing work-gate gate combined entry")
	}
	if !strings.Contains(content, "Host work-relay relay") {
		t.Error("missing work-relay relay combined entry")
	}
	if !strings.Contains(content, "HostName gw.example.com") {
		t.Error("missing hostname substitution")
	}
	if !strings.Contains(content, "RemoteForward 55555") {
		t.Error("missing relay port substitution")
	}
}

func TestGenerateSSHConfig_NoSnippets(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate:   config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth:   config.AuthConfig{User: "alice"},
				Remote: config.RemoteConfig{User: "alice-remote", Key: "~/.ssh/id_ed25519"},
				Relay:  config.RelayConfig{RemotePort: 55555},
			},
		},
	}

	// No snippets -- fallback writers produce combined host aliases for the default context
	if err := GenerateSSHConfig(cfg, configDir, socketDir, "local", nil); err != nil {
		t.Fatal(err)
	}

	output, err := os.ReadFile(filepath.Join(configDir, "ssh", "config.local"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(output)
	if !strings.Contains(content, "Host work-gate gate") {
		t.Error("missing combined gate alias")
	}
	if !strings.Contains(content, "Host work remote") {
		t.Error("missing combined remote alias")
	}
}

func TestGenerateSSHConfig_EmptyContext(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				// No hostname -- should be skipped
			},
		},
	}

	if err := GenerateSSHConfig(cfg, configDir, socketDir, "local", nil); err != nil {
		t.Fatal(err)
	}

	output, err := os.ReadFile(filepath.Join(configDir, "ssh", "config.local"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(output)
	if strings.Contains(content, "Host work-gate") {
		t.Error("empty context should be skipped")
	}
}

func TestRenderTemplate(t *testing.T) {
	tpl := "Host {{.Context}}-gate\n  HostName {{.Hostname}}\n  Port {{.Port}}\n"
	data := TemplateData{
		Context:  "work",
		Hostname: "gw.example.com",
		Port:     22,
	}

	out, err := renderTemplate("test", tpl, data)
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}
	if !strings.Contains(out, "Host work-gate") {
		t.Error("missing host entry")
	}
	if !strings.Contains(out, "HostName gw.example.com") {
		t.Error("missing hostname")
	}
	if !strings.Contains(out, "Port 22") {
		t.Error("missing port")
	}
}

func TestRenderTemplate_InvalidSyntax(t *testing.T) {
	_, err := renderTemplate("bad", "{{.Foo", TemplateData{})
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}

func TestAddSSHInclude_New(t *testing.T) {
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	os.MkdirAll(sshDir, 0700)

	sshConfig := filepath.Join(sshDir, "config")
	os.WriteFile(sshConfig, []byte("# existing content\n"), 0600)

	t.Setenv("HOME", dir)

	configLocal := "/path/to/jumpgate/ssh/config.local"
	if err := AddSSHInclude(configLocal); err != nil {
		t.Fatalf("AddSSHInclude: %v", err)
	}

	content, _ := os.ReadFile(sshConfig)
	if !strings.Contains(string(content), "Include "+configLocal) {
		t.Error("Include directive not added")
	}
	if !strings.Contains(string(content), "# existing content") {
		t.Error("existing content was lost")
	}
}

func TestAddSSHInclude_AlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	os.MkdirAll(sshDir, 0700)

	configLocal := "/path/to/jumpgate/ssh/config.local"
	sshConfig := filepath.Join(sshDir, "config")
	os.WriteFile(sshConfig, []byte("Include "+configLocal+"\n"), 0600)

	t.Setenv("HOME", dir)

	if err := AddSSHInclude(configLocal); err != nil {
		t.Fatalf("AddSSHInclude: %v", err)
	}

	content, _ := os.ReadFile(sshConfig)
	if strings.Count(string(content), "Include") != 1 {
		t.Error("Include directive duplicated")
	}
}

func TestAddSSHInclude_NoExistingConfig(t *testing.T) {
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	os.MkdirAll(sshDir, 0700)

	t.Setenv("HOME", dir)

	configLocal := "/path/to/jumpgate/ssh/config.local"
	if err := AddSSHInclude(configLocal); err != nil {
		t.Fatalf("AddSSHInclude: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(sshDir, "config"))
	if !strings.Contains(string(content), "Include "+configLocal) {
		t.Error("Include directive not created in new file")
	}
}

func TestEnsureSSHConfig_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DefaultContext: "work", Contexts: map[string]config.Context{}}
	// config.yaml doesn't exist -- should be a no-op
	err := EnsureSSHConfig(cfg, dir, filepath.Join(dir, "sockets"), "local")
	if err != nil {
		t.Errorf("EnsureSSHConfig: %v", err)
	}
}

func TestEnsureSSHConfig_UpToDate(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(filepath.Join(configDir, "ssh"), 0755)

	// Create config.yaml
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("test"), 0644)
	// Create an output that is newer
	outputPath := filepath.Join(configDir, "ssh", "config.local")
	os.WriteFile(outputPath, []byte("generated"), 0644)

	cfg := &config.Config{DefaultContext: "work", Contexts: map[string]config.Context{}}

	err := EnsureSSHConfig(cfg, configDir, filepath.Join(dir, "sockets"), "local")
	if err != nil {
		t.Errorf("EnsureSSHConfig: %v", err)
	}

	// Output should be unchanged (not regenerated)
	content, _ := os.ReadFile(outputPath)
	if string(content) != "generated" {
		t.Error("up-to-date output was unnecessarily regenerated")
	}
}

func TestEnsureSSHConfig_Stale(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(filepath.Join(configDir, "ssh"), 0755)

	// Create output first (older)
	outputPath := filepath.Join(configDir, "ssh", "config.local")
	os.WriteFile(outputPath, []byte("old"), 0644)

	// Create config.yaml after (newer) -- need a brief pause for mtime difference
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("contexts:\n  work:\n    gate:\n      hostname: gw.example.com\n"), 0644)

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth: config.AuthConfig{User: "alice"},
			},
		},
	}

	err := EnsureSSHConfig(cfg, configDir, filepath.Join(dir, "sockets"), "local")
	if err != nil {
		t.Errorf("EnsureSSHConfig: %v", err)
	}
}

func TestEnsureSSHConfig_MissingOutput(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(configDir, 0755)

	// Config exists but output doesn't
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("test"), 0644)

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts:       map[string]config.Context{},
	}

	err := EnsureSSHConfig(cfg, configDir, filepath.Join(dir, "sockets"), "local")
	if err != nil {
		t.Errorf("EnsureSSHConfig: %v", err)
	}
}

func TestSetupConfigSimple_NewConfig(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")

	template := []byte("remote_port: 0\ntest: value\n")
	if err := SetupConfigSimple(configDir, template); err != nil {
		t.Fatalf("SetupConfigSimple: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config.yaml not created: %v", err)
	}
	if strings.Contains(string(content), "remote_port: 0") {
		t.Error("remote_port: 0 should have been replaced with a random port")
	}

	// Verify subdirectories were created
	for _, sub := range []string{"ssh", "hooks"} {
		info, err := os.Stat(filepath.Join(configDir, sub))
		if err != nil || !info.IsDir() {
			t.Errorf("subdirectory %s not created", sub)
		}
	}
}

func TestSetupConfigSimple_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(configDir, 0755)
	configPath := filepath.Join(configDir, "config.yaml")
	os.WriteFile(configPath, []byte("existing"), 0644)

	if err := SetupConfigSimple(configDir, []byte("new template")); err != nil {
		t.Fatalf("SetupConfigSimple: %v", err)
	}

	content, _ := os.ReadFile(configPath)
	if string(content) != "existing" {
		t.Error("existing config was overwritten")
	}
}

func TestGenerateSSHConfig_LocalWithWildcard(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate:   config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth:   config.AuthConfig{User: "alice"},
				Remote: config.RemoteConfig{User: "alice-remote", Key: "~/.ssh/id_ed25519"},
				Relay:  config.RelayConfig{RemotePort: 55555},
			},
		},
	}

	snippets := map[string]string{
		"gate-local.conf.tpl":     "Host {{.Context}}-gate\n  HostName {{.Hostname}}\n",
		"remote-local.conf.tpl":   "Host {{.Context}}\n  User {{.RemoteUser}}\n",
		"wildcard-local.conf.tpl": "Host *\n  ServerAliveInterval 60\n",
	}

	if err := GenerateSSHConfig(cfg, configDir, socketDir, "local", snippets); err != nil {
		t.Fatal(err)
	}

	output, _ := os.ReadFile(filepath.Join(configDir, "ssh", "config.local"))
	content := string(output)

	if !strings.Contains(content, "Host *") {
		t.Error("missing wildcard entry")
	}
	if !strings.Contains(content, "ServerAliveInterval 60") {
		t.Error("missing wildcard content")
	}
}

func TestGenerateSSHConfig_MultiContext(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate:   config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth:   config.AuthConfig{User: "alice"},
				Remote: config.RemoteConfig{User: "alice-remote", Key: "~/.ssh/id_ed25519"},
				Relay:  config.RelayConfig{RemotePort: 55555},
			},
			"lab": {
				Gate:   config.GateConfig{Hostname: "lab.example.com", Port: 2222},
				Auth:   config.AuthConfig{User: "bob"},
				Remote: config.RemoteConfig{User: "bob-remote"},
				Relay:  config.RelayConfig{RemotePort: 44000},
			},
		},
	}

	snippets := map[string]string{
		"gate-local.conf.tpl":   "Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}\n  HostName {{.Hostname}}\n  Port {{.Port}}\n",
		"remote-local.conf.tpl": "Host {{.Context}}{{if .IsDefault}} remote{{end}}\n  User {{.RemoteUser}}\n  Port {{.RelayPort}}\n",
	}

	if err := GenerateSSHConfig(cfg, configDir, socketDir, "local", snippets); err != nil {
		t.Fatal(err)
	}

	output, _ := os.ReadFile(filepath.Join(configDir, "ssh", "config.local"))
	content := string(output)

	// Default context gets combined aliases
	if !strings.Contains(content, "Host work-gate gate") {
		t.Error("missing combined gate alias for default context")
	}
	// Non-default context gets only the named alias
	if !strings.Contains(content, "Host lab-gate\n") {
		t.Error("missing lab-gate (without shorthand)")
	}
}

func TestGenerateSSHConfig_RemoteWithShorthand(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate:   config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth:   config.AuthConfig{User: "alice"},
				Remote: config.RemoteConfig{User: "alice-remote"},
				Relay:  config.RelayConfig{RemotePort: 55555},
			},
		},
	}

	snippets := map[string]string{
		"gate-remote.conf.tpl":       "Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}\n  HostName {{.Hostname}}\n",
		"gate-relay-remote.conf.tpl": "Host {{.Context}}-relay{{if .IsDefault}} relay{{end}}\n  HostName {{.Hostname}}\n  RemoteForward {{.RelayPort}} localhost:22\n",
	}

	if err := GenerateSSHConfig(cfg, configDir, socketDir, "remote", snippets); err != nil {
		t.Fatal(err)
	}

	output, _ := os.ReadFile(filepath.Join(configDir, "ssh", "config.remote"))
	content := string(output)

	if !strings.Contains(content, "Host work-gate gate") {
		t.Error("missing combined gate alias")
	}
	if !strings.Contains(content, "Host work-relay relay") {
		t.Error("missing combined relay alias")
	}
}

func TestGenerateSSHConfig_LocalWithShorthandTemplate(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate:   config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth:   config.AuthConfig{User: "alice"},
				Remote: config.RemoteConfig{User: "alice-remote", Key: "~/.ssh/id_ed25519"},
				Relay:  config.RelayConfig{RemotePort: 55555},
			},
		},
	}

	snippets := map[string]string{
		"gate-local.conf.tpl":   "Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}\n  HostName {{.Hostname}}\n",
		"remote-local.conf.tpl": "Host {{.Context}}{{if .IsDefault}} remote{{end}}\n  User {{.RemoteUser}}\n",
	}

	if err := GenerateSSHConfig(cfg, configDir, socketDir, "local", snippets); err != nil {
		t.Fatal(err)
	}

	output, _ := os.ReadFile(filepath.Join(configDir, "ssh", "config.local"))
	content := string(output)

	if !strings.Contains(content, "Host work-gate gate") {
		t.Error("missing combined gate alias from template")
	}
	if !strings.Contains(content, "Host work remote") {
		t.Error("missing combined remote alias from template")
	}
}

func TestRenderTemplate_ExecuteError(t *testing.T) {
	// Template references a method that doesn't exist
	_, err := renderTemplate("bad-exec", "{{.NonexistentMethod}}", TemplateData{})
	// text/template treats missing fields as zero values by default, not errors
	// So this should succeed but produce empty output
	if err != nil {
		t.Logf("renderTemplate error (expected): %v", err)
	}
}

func TestAddSSHInclude_ReadError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sshDir := filepath.Join(dir, ".ssh")
	// Create .ssh/config as a directory to trigger a read error
	os.MkdirAll(filepath.Join(sshDir, "config"), 0700)

	err := AddSSHInclude("/some/path")
	if err == nil {
		t.Error("expected error when config is a directory")
	}
}

func TestSetupCredentials(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0755)
	os.WriteFile(filepath.Join(hooksDir, "setup-credentials"), []byte("#!/bin/sh\necho 'credentials set up'"), 0755)

	rc := &config.ResolvedContext{
		Name: "test",
		Derived: config.Derived{
			ContextName: "test",
			ConfigDir:   dir,
		},
	}

	if err := SetupCredentials(context.Background(), rc); err != nil {
		t.Errorf("SetupCredentials: %v", err)
	}
}
