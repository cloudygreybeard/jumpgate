package sitepack

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func writeTempFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPack(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "site.yaml", `
name: test-pack
description: A test site pack
platform: linux
values:
  - key: hostname
    prompt: "Gate hostname"
    default: gw.example.com
  - key: user
    prompt: "Username"
`)

	pack, err := LoadPack(dir)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}
	if pack.Name != "test-pack" {
		t.Errorf("name = %q, want test-pack", pack.Name)
	}
	if pack.Description != "A test site pack" {
		t.Errorf("description = %q", pack.Description)
	}
	if len(pack.Values) != 2 {
		t.Errorf("values count = %d, want 2", len(pack.Values))
	}
	if pack.Values[0].Default != "gw.example.com" {
		t.Errorf("values[0].default = %q", pack.Values[0].Default)
	}
	if pack.Dir != dir {
		t.Errorf("dir = %q, want %q", pack.Dir, dir)
	}
}

func TestLoadPack_Missing(t *testing.T) {
	_, err := LoadPack("/nonexistent")
	if err == nil {
		t.Fatal("expected error for missing site.yaml")
	}
}

func TestLoadValues(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "values.yaml", `
hostname: bastion.example.com
user: alice
port: 330
`)

	schema := []ValueDef{
		{Key: "hostname"},
		{Key: "user"},
		{Key: "port"},
		{Key: "realm", Default: "EXAMPLE.COM"},
	}

	vals, err := LoadValues(dir, schema)
	if err != nil {
		t.Fatalf("LoadValues: %v", err)
	}

	if vals["hostname"] != "bastion.example.com" {
		t.Errorf("hostname = %q", vals["hostname"])
	}
	if vals["user"] != "alice" {
		t.Errorf("user = %q", vals["user"])
	}
	if vals["port"] != "330" {
		t.Errorf("port = %q", vals["port"])
	}
	if vals["realm"] != "EXAMPLE.COM" {
		t.Errorf("realm (default) = %q, want EXAMPLE.COM", vals["realm"])
	}
}

func TestLoadValues_MissingFile(t *testing.T) {
	dir := t.TempDir()
	schema := []ValueDef{
		{Key: "hostname", Default: "gw.example.com"},
	}

	vals, err := LoadValues(dir, schema)
	if err != nil {
		t.Fatalf("LoadValues: %v", err)
	}
	if vals["hostname"] != "gw.example.com" {
		t.Errorf("hostname = %q, want default gw.example.com", vals["hostname"])
	}
}

func TestLoadValues_AutoRelayPort(t *testing.T) {
	dir := t.TempDir()
	schema := []ValueDef{
		{Key: "relay_port", Default: "auto"},
	}

	vals, err := LoadValues(dir, schema)
	if err != nil {
		t.Fatalf("LoadValues: %v", err)
	}
	port := vals["relay_port"]
	if port == "" || port == "auto" {
		t.Errorf("relay_port should be a generated number, got %q", port)
	}
}

func TestPromptMissing(t *testing.T) {
	vals := map[string]string{
		"hostname": "bastion.example.com",
	}
	schema := []ValueDef{
		{Key: "hostname", Prompt: "Gate hostname"},
		{Key: "user", Prompt: "Username"},
	}

	input := "alice\n"
	reader := bufio.NewReader(strings.NewReader(input))

	if err := PromptMissing(vals, schema, reader); err != nil {
		t.Fatalf("PromptMissing: %v", err)
	}

	if vals["hostname"] != "bastion.example.com" {
		t.Errorf("hostname should be unchanged, got %q", vals["hostname"])
	}
	if vals["user"] != "alice" {
		t.Errorf("user = %q, want alice", vals["user"])
	}
}

func TestPromptMissing_Default(t *testing.T) {
	vals := map[string]string{}
	schema := []ValueDef{
		{Key: "port", Prompt: "SSH port", Default: "22"},
	}

	input := "\n"
	reader := bufio.NewReader(strings.NewReader(input))

	if err := PromptMissing(vals, schema, reader); err != nil {
		t.Fatalf("PromptMissing: %v", err)
	}
	if vals["port"] != "22" {
		t.Errorf("port = %q, want 22", vals["port"])
	}
}

func TestRender(t *testing.T) {
	packDir := t.TempDir()
	configDir := t.TempDir()

	writeTempFile(t, packDir, "site.yaml", `
name: test
values: []
`)

	writeTempFile(t, packDir, "templates/config.yaml.tpl",
		"host: {{.hostname}}\nuser: {{.user}}\n")

	writeTempFile(t, packDir, "hooks/my-hook", "#!/bin/bash\necho hook\n")
	writeTempFile(t, packDir, "snippets/extra.conf.tpl", "Host *.example.com\n")

	pack, err := LoadPack(packDir)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}

	vals := map[string]string{
		"hostname": "gw.example.com",
		"user":     "alice",
	}

	if err := Render(pack, vals, configDir); err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Check rendered config
	data, err := os.ReadFile(filepath.Join(configDir, "config.yaml"))
	if err != nil {
		t.Fatalf("reading rendered config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "host: gw.example.com") {
		t.Errorf("rendered config missing hostname:\n%s", content)
	}
	if !strings.Contains(content, "user: alice") {
		t.Errorf("rendered config missing user:\n%s", content)
	}

	// Check hooks copied and executable
	hookPath := filepath.Join(configDir, "hooks", "my-hook")
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("hook not copied: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("hook not executable: %v", info.Mode())
	}

	// Check snippets copied
	snippetPath := filepath.Join(configDir, "ssh", "snippets", "extra.conf.tpl")
	if _, err := os.Stat(snippetPath); err != nil {
		t.Fatalf("snippet not copied: %v", err)
	}
}

func TestRender_NoTemplates(t *testing.T) {
	packDir := t.TempDir()
	configDir := t.TempDir()

	writeTempFile(t, packDir, "site.yaml", "name: minimal\nvalues: []\n")

	pack, err := LoadPack(packDir)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}

	if err := Render(pack, map[string]string{}, configDir); err != nil {
		t.Fatalf("Render with no templates: %v", err)
	}
}

func TestExtractValues(t *testing.T) {
	ctx := config.Context{
		Role: "local",
		Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
		Auth: config.AuthConfig{
			Type:  "kerberos",
			User:  "alice",
			Realm: "EXAMPLE.COM",
			KDC:   "kdc.example.com",
		},
		Remote: config.RemoteConfig{User: "alice-remote", Key: "~/.ssh/id_ed25519"},
		Relay:  config.RelayConfig{RemotePort: 55555},
	}

	vals := ExtractValues("work", &ctx)
	if vals["context"] != "work" {
		t.Errorf("context = %q, want work", vals["context"])
	}
	if vals["gate_host"] != "gw.example.com" {
		t.Errorf("gate_host = %q", vals["gate_host"])
	}
	if vals["auth_user"] != "alice" {
		t.Errorf("auth_user = %q", vals["auth_user"])
	}
	if vals["auth_realm"] != "EXAMPLE.COM" {
		t.Errorf("auth_realm = %q", vals["auth_realm"])
	}
	if vals["relay_port"] != "55555" {
		t.Errorf("relay_port = %q", vals["relay_port"])
	}
	if vals["role"] != "local" {
		t.Errorf("role = %q", vals["role"])
	}
}

func TestBuildSchema(t *testing.T) {
	vals := map[string]string{"gate_host": "gw.example.com"}
	schema := BuildSchema(vals)
	if len(schema) == 0 {
		t.Fatal("schema should not be empty")
	}

	found := false
	for _, s := range schema {
		if s.Key == "gate_host" {
			found = true
			if s.Prompt == "" {
				t.Error("gate_host should have a prompt")
			}
		}
	}
	if !found {
		t.Error("schema missing gate_host")
	}
}

func TestExport(t *testing.T) {
	configDir := t.TempDir()
	outDir := t.TempDir()

	os.MkdirAll(filepath.Join(configDir, "hooks"), 0755)
	os.WriteFile(filepath.Join(configDir, "hooks", "test-hook"), []byte("#!/bin/sh\necho hi\n"), 0755)
	os.MkdirAll(filepath.Join(configDir, "ssh", "snippets"), 0755)
	os.WriteFile(filepath.Join(configDir, "ssh", "snippets", "custom.conf.tpl"), []byte("Host custom\n"), 0644)

	ctx := config.Context{
		Role: "local",
		Gate: config.GateConfig{Hostname: "gw.example.com", Port: 330},
		Auth: config.AuthConfig{Type: "kerberos", User: "alice", Realm: "EXAMPLE.COM"},
		Relay: config.RelayConfig{RemotePort: 55555},
	}

	if err := Export("work", &ctx, configDir, outDir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// site.yaml should exist and load
	pack, err := LoadPack(outDir)
	if err != nil {
		t.Fatalf("LoadPack of exported pack: %v", err)
	}
	if !strings.Contains(pack.Name, "work") {
		t.Errorf("pack name = %q, want to contain 'work'", pack.Name)
	}
	if len(pack.Values) == 0 {
		t.Error("schema should not be empty")
	}

	// values.yaml should exist
	if _, err := os.Stat(filepath.Join(outDir, "values.yaml")); err != nil {
		t.Errorf("values.yaml not created: %v", err)
	}

	// values.example.yaml should exist
	if _, err := os.Stat(filepath.Join(outDir, "values.example.yaml")); err != nil {
		t.Errorf("values.example.yaml not created: %v", err)
	}

	// Template should exist and contain placeholders
	tpl, err := os.ReadFile(filepath.Join(outDir, "templates", "config.yaml.tpl"))
	if err != nil {
		t.Fatalf("reading template: %v", err)
	}
	tplStr := string(tpl)
	if !strings.Contains(tplStr, "{{.gate_host}}") {
		t.Error("template missing {{.gate_host}} placeholder")
	}
	if !strings.Contains(tplStr, "{{.context}}") {
		t.Error("template missing {{.context}} placeholder")
	}

	// Hooks should be copied
	if _, err := os.Stat(filepath.Join(outDir, "hooks", "test-hook")); err != nil {
		t.Errorf("hook not copied: %v", err)
	}

	// Snippets should be copied
	if _, err := os.Stat(filepath.Join(outDir, "snippets", "custom.conf.tpl")); err != nil {
		t.Errorf("snippet not copied: %v", err)
	}

	// .gitignore should exist
	gi, err := os.ReadFile(filepath.Join(outDir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), "values.yaml") {
		t.Error(".gitignore should contain values.yaml")
	}
}

func TestExportThenImport_RoundTrip(t *testing.T) {
	configDir := t.TempDir()
	outDir := t.TempDir()

	ctx := config.Context{
		Role: "local",
		Gate: config.GateConfig{Hostname: "bastion.example.com", Port: 330},
		Auth: config.AuthConfig{
			Type:          "kerberos",
			User:          "alice",
			Realm:         "EXAMPLE.COM",
			KDC:           "kdc.example.com",
			KDCLocalPort:  8888,
			KDCRemotePort: 88,
			Kinit:         "/usr/bin/kinit",
		},
		Remote: config.RemoteConfig{User: "alice-remote", Key: "~/.ssh/id_ed25519", RemoteDir: "~/jumpgate"},
		Relay:  config.RelayConfig{RemotePort: 55555},
	}

	if err := Export("myctx", &ctx, configDir, outDir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Load the exported pack and values, then render back
	pack, err := LoadPack(outDir)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}

	vals, err := LoadValues(outDir, pack.Values)
	if err != nil {
		t.Fatalf("LoadValues: %v", err)
	}

	// Values should contain the original data
	if vals["gate_host"] != "bastion.example.com" {
		t.Errorf("round-trip gate_host = %q", vals["gate_host"])
	}
	if vals["auth_user"] != "alice" {
		t.Errorf("round-trip auth_user = %q", vals["auth_user"])
	}
	if vals["relay_port"] != "55555" {
		t.Errorf("round-trip relay_port = %q", vals["relay_port"])
	}

	// Render back to a config dir
	renderDir := t.TempDir()
	if err := Render(pack, vals, renderDir); err != nil {
		t.Fatalf("Render: %v", err)
	}

	// The rendered config should contain the original values
	rendered, err := os.ReadFile(filepath.Join(renderDir, "config.yaml"))
	if err != nil {
		t.Fatalf("reading rendered config: %v", err)
	}
	content := string(rendered)
	if !strings.Contains(content, "bastion.example.com") {
		t.Errorf("rendered config missing gate hostname:\n%s", content)
	}
	if !strings.Contains(content, "EXAMPLE.COM") {
		t.Errorf("rendered config missing realm:\n%s", content)
	}
	if !strings.Contains(content, "55555") {
		t.Errorf("rendered config missing relay port:\n%s", content)
	}
}

func TestApplyValues(t *testing.T) {
	vals := map[string]string{
		"role":       "local",
		"gate_host":  "gw.example.com",
		"gate_port":  "330",
		"auth_type":  "kerberos",
		"auth_user":  "alice",
		"auth_realm": "EXAMPLE.COM",
		"relay_port": "55555",
		"remote_user": "alice-remote",
	}

	ctx := ApplyValues(vals)
	if ctx.Role != "local" {
		t.Errorf("role = %q", ctx.Role)
	}
	if ctx.Gate.Hostname != "gw.example.com" {
		t.Errorf("gate.hostname = %q", ctx.Gate.Hostname)
	}
	if ctx.Gate.Port != 330 {
		t.Errorf("gate.port = %d", ctx.Gate.Port)
	}
	if ctx.Auth.Type != "kerberos" {
		t.Errorf("auth.type = %q", ctx.Auth.Type)
	}
	if ctx.Relay.RemotePort != 55555 {
		t.Errorf("relay.remote_port = %d", ctx.Relay.RemotePort)
	}
	if ctx.Remote.User != "alice-remote" {
		t.Errorf("remote.user = %q", ctx.Remote.User)
	}
}

func TestFilterNonEmpty(t *testing.T) {
	vals := map[string]string{
		"a": "hello",
		"b": "",
		"c": "0",
		"d": "world",
	}
	filtered := FilterNonEmpty(vals)
	if _, ok := filtered["b"]; ok {
		t.Error("empty value should be filtered")
	}
	if _, ok := filtered["c"]; ok {
		t.Error("zero value should be filtered")
	}
	if filtered["a"] != "hello" || filtered["d"] != "world" {
		t.Errorf("non-empty values should be kept: %v", filtered)
	}
}

func TestRender_WindowsScripts(t *testing.T) {
	packDir := t.TempDir()
	configDir := t.TempDir()

	writeTempFile(t, packDir, "site.yaml", "name: win-test\nvalues: []\n")
	writeTempFile(t, packDir, "windows/connect.ps1", "wsl.exe echo hi\n")

	pack, err := LoadPack(packDir)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}

	if err := Render(pack, map[string]string{}, configDir); err != nil {
		t.Fatalf("Render: %v", err)
	}

	winPath := filepath.Join(configDir, "windows", "connect.ps1")
	if _, err := os.Stat(winPath); err != nil {
		t.Fatalf("windows script not copied: %v", err)
	}
}
