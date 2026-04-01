package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigDir_XDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	got := DefaultConfigDir()
	want := filepath.Join(dir, "jumpgate")
	if got != want {
		t.Errorf("DefaultConfigDir() = %q, want %q", got, want)
	}
}

func TestDefaultConfigDir_NoXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := DefaultConfigDir()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "jumpgate")
	if got != want {
		t.Errorf("DefaultConfigDir() = %q, want %q", got, want)
	}
}

func TestDefaultConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	got := DefaultConfigFile()
	want := filepath.Join(dir, "jumpgate", "config.yaml")
	if got != want {
		t.Errorf("DefaultConfigFile() = %q, want %q", got, want)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTestConfig(t, "{{invalid yaml}")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadEmptyConfig(t *testing.T) {
	path := writeTestConfig(t, "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultContext != "default" {
		t.Errorf("DefaultContext = %q, want 'default'", cfg.DefaultContext)
	}
	if cfg.Contexts == nil {
		t.Error("Contexts should not be nil")
	}
}

func TestResolveNoContexts(t *testing.T) {
	path := writeTestConfig(t, "default_context: work\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.Resolve("")
	if err == nil {
		t.Fatal("expected error when no contexts exist")
	}
}

func TestKDCForwardSpec_CustomPorts(t *testing.T) {
	rc := &ResolvedContext{
		Context: Context{
			Auth: AuthConfig{
				KDC:           "mykdc.example.com",
				KDCLocalPort:  9999,
				KDCRemotePort: 188,
			},
		},
	}
	want := "127.0.0.1:9999:mykdc.example.com:188"
	if got := rc.KDCForwardSpec(); got != want {
		t.Errorf("KDCForwardSpec() = %q, want %q", got, want)
	}
}

func TestApplyDefaults_AllEmpty(t *testing.T) {
	p := &Context{}
	applyDefaults(p, "test")

	if p.Gate.Port != 22 {
		t.Errorf("Port = %d, want 22", p.Gate.Port)
	}
	if p.Auth.Type != "key" {
		t.Errorf("Auth.Type = %q, want 'key'", p.Auth.Type)
	}
	if p.Auth.KDCLocalPort != 8888 {
		t.Errorf("KDCLocalPort = %d, want 8888", p.Auth.KDCLocalPort)
	}
	if p.Auth.KDCRemotePort != 88 {
		t.Errorf("KDCRemotePort = %d, want 88", p.Auth.KDCRemotePort)
	}
	if p.Auth.Kinit != "kinit" {
		t.Errorf("Kinit = %q, want 'kinit'", p.Auth.Kinit)
	}
	home, _ := os.UserHomeDir()
	wantPrefix := filepath.Join(home, ".cache", "jumpgate", "krb5cc_")
	if !strings.HasPrefix(p.Auth.CCFile, wantPrefix) {
		t.Errorf("CCFile = %q, want prefix %q", p.Auth.CCFile, wantPrefix)
	}
	if p.Remote.RemoteDir != "~/jumpgate" {
		t.Errorf("RemoteDir = %q, want '~/jumpgate'", p.Remote.RemoteDir)
	}
	if p.Container.Runtime != "podman" {
		t.Errorf("Runtime = %q, want 'podman'", p.Container.Runtime)
	}
	if p.Container.Image != "jumpgate-kinit" {
		t.Errorf("Image = %q, want 'jumpgate-kinit'", p.Container.Image)
	}
	if p.WindowsApp != "Windows App" {
		t.Errorf("WindowsApp = %q, want 'Windows App'", p.WindowsApp)
	}
}

func TestApplyDefaults_PreservedWhenSet(t *testing.T) {
	p := &Context{
		Gate:      GateConfig{Port: 2222},
		Auth:      AuthConfig{Type: "kerberos", KDCLocalPort: 7777, KDCRemotePort: 99, Kinit: "/usr/bin/kinit", CCFile: "/custom/cc"},
		Remote:    RemoteConfig{RemoteDir: "~/custom"},
		Container: ContainerConfig{Runtime: "docker", Image: "myimage"},
	}
	applyDefaults(p, "test")

	if p.Gate.Port != 2222 {
		t.Errorf("Port = %d, want 2222 (should be preserved)", p.Gate.Port)
	}
	if p.Auth.Type != "kerberos" {
		t.Errorf("Auth.Type = %q, want 'kerberos'", p.Auth.Type)
	}
	if p.Auth.KDCLocalPort != 7777 {
		t.Errorf("KDCLocalPort = %d, want 7777", p.Auth.KDCLocalPort)
	}
	if p.Auth.CCFile != "/custom/cc" {
		t.Errorf("CCFile = %q, want '/custom/cc'", p.Auth.CCFile)
	}
	if p.Container.Runtime != "docker" {
		t.Errorf("Runtime = %q, want 'docker'", p.Container.Runtime)
	}
}
