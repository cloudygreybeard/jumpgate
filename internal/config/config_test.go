package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testConfig = `
default_context: work

contexts:
  work:
    gate:
      hostname: gateway.example.com
      port: 22
    auth:
      type: kerberos
      realm: EXAMPLE.COM
      user: alice
      kdc: kdc.example.com
      kdc_local_port: 8888
      kdc_remote_port: 88
      kinit: /opt/homebrew/opt/krb5/bin/kinit
      cc_file: /tmp/krb5cc_work
    remote:
      user: alice-remote
      key: ~/.ssh/id_ed25519
      remote_dir: ~/jumpgate
    relay:
      remote_port: 55555
    keychain:
      totp_service: "totp key"
      totp_account: alice
      krb_service: jumpgate-kerberos
      krb_account: alice
    totp:
      cli: /opt/homebrew/bin/totp-cli
      namespace: myns
      account: alice
    container:
      runtime: podman
      image: jumpgate-kinit
    windows_app: "Windows App"
  lab:
    gate:
      hostname: lab-gateway.example.com
    auth:
      type: key
    remote:
      user: alice
      key: ~/.ssh/id_ed25519
    relay:
      remote_port: 44000
`

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DefaultContext != "work" {
		t.Errorf("DefaultContext = %q, want %q", cfg.DefaultContext, "work")
	}
	if len(cfg.Contexts) != 2 {
		t.Errorf("len(Contexts) = %d, want 2", len(cfg.Contexts))
	}
	if cfg.Contexts["work"].Gate.Hostname != "gateway.example.com" {
		t.Errorf("work gate hostname = %q", cfg.Contexts["work"].Gate.Hostname)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveDefault(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if rc.Name != "work" {
		t.Errorf("Name = %q, want %q", rc.Name, "work")
	}
	if rc.Context.Gate.Hostname != "gateway.example.com" {
		t.Errorf("Gate.Hostname = %q", rc.Context.Gate.Hostname)
	}
	if rc.Derived.AuthPrincipal != "alice@EXAMPLE.COM" {
		t.Errorf("AuthPrincipal = %q", rc.Derived.AuthPrincipal)
	}
	if rc.Derived.GateHost != "work-gate" {
		t.Errorf("GateHost = %q", rc.Derived.GateHost)
	}
	if rc.Derived.RemoteHost != "work" {
		t.Errorf("RemoteHost = %q", rc.Derived.RemoteHost)
	}
	if rc.Derived.RelayHost != "work-relay" {
		t.Errorf("RelayHost = %q", rc.Derived.RelayHost)
	}
}

func TestResolveExplicit(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := cfg.Resolve("lab")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rc.Name != "lab" {
		t.Errorf("Name = %q, want %q", rc.Name, "lab")
	}
	if rc.Context.Gate.Hostname != "lab-gateway.example.com" {
		t.Errorf("Gate.Hostname = %q", rc.Context.Gate.Hostname)
	}
}

func TestResolveMissing(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cfg.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing context")
	}
}

func TestApplyDefaults(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := cfg.Resolve("lab")
	if err != nil {
		t.Fatal(err)
	}

	p := rc.Context
	if p.Gate.Port != 22 {
		t.Errorf("Gate.Port = %d, want 22", p.Gate.Port)
	}
	if p.Auth.Type != "key" {
		t.Errorf("Auth.Type = %q, want %q", p.Auth.Type, "key")
	}
	if p.Auth.KDCLocalPort != 8888 {
		t.Errorf("Auth.KDCLocalPort = %d, want 8888", p.Auth.KDCLocalPort)
	}
	if p.Auth.Kinit != "kinit" {
		t.Errorf("Auth.Kinit = %q, want %q", p.Auth.Kinit, "kinit")
	}
	if p.Auth.CCFile != "/tmp/krb5cc_lab" {
		t.Errorf("Auth.CCFile = %q, want %q", p.Auth.CCFile, "/tmp/krb5cc_lab")
	}
	if p.Remote.RemoteDir != "~/jumpgate" {
		t.Errorf("Remote.RemoteDir = %q, want %q", p.Remote.RemoteDir, "~/jumpgate")
	}
	if p.Container.Runtime != "podman" {
		t.Errorf("Container.Runtime = %q, want %q", p.Container.Runtime, "podman")
	}
	if p.Container.Image != "jumpgate-kinit" {
		t.Errorf("Container.Image = %q, want %q", p.Container.Image, "jumpgate-kinit")
	}
	if p.WindowsApp != "Windows App" {
		t.Errorf("WindowsApp = %q, want %q", p.WindowsApp, "Windows App")
	}
}

func TestKDCForwardSpec(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := cfg.Resolve("work")
	if err != nil {
		t.Fatal(err)
	}

	want := "0.0.0.0:8888:kdc.example.com:88"
	if got := rc.KDCForwardSpec(); got != want {
		t.Errorf("KDCForwardSpec() = %q, want %q", got, want)
	}
}

func TestRelayPortAuto(t *testing.T) {
	cfg := `
default_context: work
contexts:
  work:
    gate:
      hostname: gw.example.com
    relay:
      remote_port: auto
`
	path := writeTestConfig(t, cfg)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load with remote_port: auto failed: %v", err)
	}
	if c.Contexts["work"].Relay.RemotePort != 0 {
		t.Errorf("RemotePort = %d, want 0 for 'auto'", c.Contexts["work"].Relay.RemotePort)
	}
}

func TestDefaultContextFallback(t *testing.T) {
	cfg := `
contexts:
  default:
    gate:
      hostname: gw.example.com
`
	path := writeTestConfig(t, cfg)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultContext != "default" {
		t.Errorf("DefaultContext = %q, want %q", c.DefaultContext, "default")
	}
	rc, err := c.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Context.Gate.Hostname != "gw.example.com" {
		t.Errorf("hostname = %q", rc.Context.Gate.Hostname)
	}
}
