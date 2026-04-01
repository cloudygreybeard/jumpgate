package config

import (
	"os"
	"path/filepath"
	"strings"
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
	home, _ := os.UserHomeDir()
	wantPrefix := filepath.Join(home, ".cache", "jumpgate", "krb5cc_")
	if !strings.HasPrefix(p.Auth.CCFile, wantPrefix) {
		t.Errorf("Auth.CCFile = %q, want prefix %q", p.Auth.CCFile, wantPrefix)
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

	want := "127.0.0.1:8888:kdc.example.com:88"
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

func TestGenerateUID(t *testing.T) {
	uid := GenerateUID()
	parts := strings.Split(uid, "-")
	if len(parts) != 5 {
		t.Fatalf("GenerateUID() = %q, want 5 dash-separated parts", uid)
	}
	if len(uid) != 36 {
		t.Errorf("GenerateUID() length = %d, want 36", len(uid))
	}

	uid2 := GenerateUID()
	if uid == uid2 {
		t.Error("two GenerateUID calls returned the same value")
	}
}

func TestResolveCCFileWithUID(t *testing.T) {
	cfg := `
default_context: myctx
contexts:
  myctx:
    uid: abcd1234-5678-4aaa-bbbb-ccccddddeeee
    gate:
      hostname: gw.example.com
    auth:
      type: kerberos
      realm: EXAMPLE.COM
      user: alice
`
	path := writeTestConfig(t, cfg)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := c.Resolve("myctx")
	if err != nil {
		t.Fatal(err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".cache", "jumpgate", "krb5cc_abcd1234-5678-4aaa-bbbb-ccccddddeeee")
	if rc.Context.Auth.CCFile != want {
		t.Errorf("CCFile = %q, want %q", rc.Context.Auth.CCFile, want)
	}
	if rc.Derived.UID != "abcd1234-5678-4aaa-bbbb-ccccddddeeee" {
		t.Errorf("Derived.UID = %q, want uid from context", rc.Derived.UID)
	}
}

func TestBackfillUIDs(t *testing.T) {
	cfg := `
default_context: work
contexts:
  work:
    gate:
      hostname: gw.example.com
  lab:
    gate:
      hostname: lab.example.com
`
	path := writeTestConfig(t, cfg)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if c.Contexts["work"].UID == "" {
		t.Error("expected backfilled UID for work context")
	}
	if c.Contexts["lab"].UID == "" {
		t.Error("expected backfilled UID for lab context")
	}
	if c.Contexts["work"].UID == c.Contexts["lab"].UID {
		t.Error("expected distinct UIDs for work and lab")
	}

	// Verify UIDs were persisted to the file
	c2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Contexts["work"].UID != c.Contexts["work"].UID {
		t.Errorf("persisted work UID %q != original %q", c2.Contexts["work"].UID, c.Contexts["work"].UID)
	}
	if c2.Contexts["lab"].UID != c.Contexts["lab"].UID {
		t.Errorf("persisted lab UID %q != original %q", c2.Contexts["lab"].UID, c.Contexts["lab"].UID)
	}
}

func TestExistingUIDNotOverwritten(t *testing.T) {
	cfg := `
default_context: work
contexts:
  work:
    uid: fixed-uid-1234
    gate:
      hostname: gw.example.com
`
	path := writeTestConfig(t, cfg)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Contexts["work"].UID != "fixed-uid-1234" {
		t.Errorf("UID = %q, want %q (should not be overwritten)", c.Contexts["work"].UID, "fixed-uid-1234")
	}
}

func TestCCFileExplicitNotOverridden(t *testing.T) {
	cfg := `
default_context: work
contexts:
  work:
    uid: some-uid
    gate:
      hostname: gw.example.com
    auth:
      cc_file: /custom/krb5cc
`
	path := writeTestConfig(t, cfg)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := c.Resolve("work")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Context.Auth.CCFile != "/custom/krb5cc" {
		t.Errorf("CCFile = %q, want %q (explicit should not be overridden)", rc.Context.Auth.CCFile, "/custom/krb5cc")
	}
}
