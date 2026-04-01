package connect

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func makeTestEnv(t *testing.T) (*config.ResolvedContext, string) {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(binDir, 0755)
	os.MkdirAll(hooksDir, 0755)

	// fake ssh: succeeds for everything. For Probe-like calls with "true", exit 0.
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kinit"), []byte("#!/bin/sh\nread _pw; exit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc := &config.ResolvedContext{
		Name: "test",
		Context: config.Context{
			Role: "local",
			Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
			Auth: config.AuthConfig{
				Type:          "key",
				User:          "alice",
				Realm:         "EXAMPLE.COM",
				CCFile:        filepath.Join(dir, "krb5cc_test"),
				KDC:           "kdc.example.com",
				KDCLocalPort:  8888,
				KDCRemotePort: 88,
				Kinit:         "kinit",
			},
			Remote: config.RemoteConfig{User: "alice-remote", Key: "~/.ssh/id_ed25519"},
			Relay:  config.RelayConfig{RemotePort: 55555},
		},
		Derived: config.Derived{
			ContextName:   "test",
			AuthPrincipal: "alice@EXAMPLE.COM",
			GateHost:      "test-gate",
			RemoteHost:    "test",
			RelayHost:     "test-relay",
			GateSocket:    filepath.Join(dir, "test-gate.sock"),
			RelaySocket:   filepath.Join(dir, "test-relay.sock"),
			ConfigDir:     dir,
		},
	}

	return rc, dir
}

func TestConnectLocal_Reachable(t *testing.T) {
	rc, dir := makeTestEnv(t)

	// Create gate socket so EnsureGate sees existing connection
	sockDir, err := os.MkdirTemp("/tmp", "jg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "s.sock")
	rc.Derived.GateSocket = sockPath
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("creating socket: %v", err)
	}
	defer l.Close()

	// Create get-gate-token hook for gate session
	os.WriteFile(filepath.Join(dir, "hooks", "get-gate-token"),
		[]byte("#!/bin/sh\nprintf 'token'"), 0755)

	cfg := &config.Config{
		DefaultContext: "test",
		Contexts:       map[string]config.Context{"test": rc.Context},
	}

	err = Connect(context.Background(), rc, cfg)
	if err != nil {
		t.Errorf("Connect: %v", err)
	}
}

func TestConnectRemote_AlreadyActive(t *testing.T) {
	rc, _ := makeTestEnv(t)
	rc.Derived.GateSocket = ""

	sockDir, err := os.MkdirTemp("/tmp", "jg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "s.sock")
	rc.Derived.RelaySocket = sockPath

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("creating socket: %v", err)
	}
	defer l.Close()

	err = connectRemote(context.Background(), rc, nil)
	if err != nil {
		t.Errorf("connectRemote already active: %v", err)
	}
}

func TestConnectRemote_New(t *testing.T) {
	rc, _ := makeTestEnv(t)
	rc.Derived.GateSocket = ""
	rc.Derived.RelaySocket = filepath.Join(t.TempDir(), "r.sock")

	err := connectRemote(context.Background(), rc, nil)
	if err != nil {
		t.Errorf("connectRemote new: %v", err)
	}
}

func TestConnectLocal_GateFails(t *testing.T) {
	rc, _ := makeTestEnv(t)
	// No gate socket, auth type is key, but get-gate-token hook is missing
	// so EnsureGate should fail with "not found" error

	err := Connect(context.Background(), rc, nil)
	if err == nil {
		t.Error("expected error when get-gate-token hook is missing")
	}
}

func TestConnectLocal_ContextCancelled(t *testing.T) {
	rc, dir := makeTestEnv(t)

	sockDir, err := os.MkdirTemp("/tmp", "jg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "s.sock")
	rc.Derived.GateSocket = sockPath
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("creating socket: %v", err)
	}
	defer l.Close()

	os.WriteFile(filepath.Join(dir, "hooks", "get-gate-token"),
		[]byte("#!/bin/sh\nprintf 'token'"), 0755)

	// Pre-cancel the context so connect fails immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = Connect(ctx, rc, nil)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestCollectStatus_LocalViaConnect(t *testing.T) {
	rc, _ := makeTestEnv(t)
	rc.Derived.GateSocket = "/tmp/nonexistent.sock"

	info := CollectStatus(context.Background(), rc)
	if info.ContextName != "test" {
		t.Errorf("context = %q", info.ContextName)
	}
	if info.Gate.Connected {
		t.Error("gate should not be connected")
	}
}

func TestConnectRemote_FailedRelay(t *testing.T) {
	rc, _ := makeTestEnv(t)
	rc.Derived.GateSocket = ""

	binDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(binDir, 0755)
	// ssh -O check fails (relay check fails after open)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\ncase \"$*\" in *-O\\ check*) exit 1;; *) exit 0;; esac\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc.Derived.RelaySocket = filepath.Join(t.TempDir(), "r.sock")
	err := connectRemote(context.Background(), rc, nil)
	if err == nil {
		t.Error("expected error when relay check fails")
	}
}

func TestDisconnect_Remote(t *testing.T) {
	rc, _ := makeTestEnv(t)
	rc.Derived.GateSocket = ""
	rc.Derived.RelaySocket = "/tmp/nonexistent.sock"

	disconnectRemote(context.Background(), rc)
}

func TestConnectRemote_AutoGeneratePort(t *testing.T) {
	rc, dir := makeTestEnv(t)
	rc.Context.Role = "remote"
	rc.Context.Relay.RemotePort = 0
	rc.Derived.GateSocket = ""
	rc.Derived.RelaySocket = filepath.Join(t.TempDir(), "r.sock")

	// Write a minimal config.yaml so persistRelayPort can load and save
	configDir := filepath.Join(dir, "jumpgate-cfg")
	os.MkdirAll(configDir, 0755)
	rc.Derived.ConfigDir = configDir
	cfgPath := filepath.Join(configDir, "config.yaml")
	cfgContent := "default_context: test\ncontexts:\n  test:\n    role: remote\n    gate:\n      hostname: gw.example.com\n    relay:\n      remote_port: 0\n"
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	err := connectRemote(context.Background(), rc, nil)
	if err != nil {
		t.Errorf("connectRemote with port 0: %v", err)
	}
	if rc.Context.Relay.RemotePort == 0 {
		t.Error("expected RemotePort to be auto-generated, still 0")
	}
	if rc.Context.Relay.RemotePort < 49152 || rc.Context.Relay.RemotePort > 65535 {
		t.Errorf("RemotePort %d not in ephemeral range", rc.Context.Relay.RemotePort)
	}

	// Verify the port was persisted
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	saved := reloaded.Contexts["test"]
	if saved.Relay.RemotePort != rc.Context.Relay.RemotePort {
		t.Errorf("persisted port = %d, in-memory = %d", saved.Relay.RemotePort, rc.Context.Relay.RemotePort)
	}
}

func TestConnectRemote_RelayOpenFail(t *testing.T) {
	rc, _ := makeTestEnv(t)
	rc.Context.Role = "remote"
	rc.Context.Relay.RemotePort = 55000
	rc.Derived.GateSocket = ""
	rc.Derived.RelaySocket = filepath.Join(t.TempDir(), "r.sock")

	// Fake ssh: relay open fails (simulates ExitOnForwardFailure)
	binDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(binDir, 0755)
	script := `#!/bin/sh
exit 1
`
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	err := connectRemote(context.Background(), rc, nil)
	if err == nil {
		t.Fatal("expected error when relay open fails")
	}
	if !strings.Contains(err.Error(), "opening relay") {
		t.Errorf("error = %q, want 'opening relay'", err.Error())
	}
}

func TestDiscoverRelayPort_Updated(t *testing.T) {
	rc, dir := makeTestEnv(t)

	// Write config.yaml for persistence
	configDir := filepath.Join(dir, "jumpgate-cfg")
	os.MkdirAll(configDir, 0755)
	rc.Derived.ConfigDir = configDir
	cfgContent := "default_context: test\ncontexts:\n  test:\n    role: local\n    gate:\n      hostname: gw.example.com\n    relay:\n      remote_port: 55555\n"
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(cfgContent), 0644)

	// Fake ssh that outputs a different port (simulating marker read)
	binDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(binDir, 0755)
	script := `#!/bin/sh
case "$*" in
  *cat*)
    echo 55000
    exit 0
    ;;
esac
exit 0
`
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	cfg := &config.Config{
		DefaultContext: "test",
		Contexts:       map[string]config.Context{"test": rc.Context},
	}

	updated := discoverRelayPort(context.Background(), rc, cfg)
	if !updated {
		t.Error("expected discoverRelayPort to return true")
	}
	if rc.Context.Relay.RemotePort != 55000 {
		t.Errorf("port = %d, want 55000", rc.Context.Relay.RemotePort)
	}
	if cfg.Contexts["test"].Relay.RemotePort != 55000 {
		t.Errorf("cfg port = %d, want 55000", cfg.Contexts["test"].Relay.RemotePort)
	}
}

func TestDiscoverRelayPort_NoMarker(t *testing.T) {
	rc, _ := makeTestEnv(t)

	// Fake ssh that outputs nothing (no marker file)
	binDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	cfg := &config.Config{
		DefaultContext: "test",
		Contexts:       map[string]config.Context{"test": rc.Context},
	}

	updated := discoverRelayPort(context.Background(), rc, cfg)
	if updated {
		t.Error("expected discoverRelayPort to return false when no marker")
	}
}

func TestDiscoverRelayPort_SamePort(t *testing.T) {
	rc, _ := makeTestEnv(t)

	// Fake ssh that outputs the same port already in config
	binDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(binDir, 0755)
	script := `#!/bin/sh
case "$*" in
  *cat*)
    echo 55555
    exit 0
    ;;
esac
exit 0
`
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	cfg := &config.Config{
		DefaultContext: "test",
		Contexts:       map[string]config.Context{"test": rc.Context},
	}

	updated := discoverRelayPort(context.Background(), rc, cfg)
	if updated {
		t.Error("expected discoverRelayPort to return false when port matches")
	}
}

func TestDiscoverRelayPort_NilConfig(t *testing.T) {
	rc, _ := makeTestEnv(t)

	updated := discoverRelayPort(context.Background(), rc, nil)
	if updated {
		t.Error("expected false with nil config")
	}
}

func TestConnectRemote_WritesMarker(t *testing.T) {
	rc, dir := makeTestEnv(t)
	rc.Context.Role = "remote"
	rc.Context.Relay.RemotePort = 55555
	rc.Derived.GateSocket = ""
	rc.Derived.RelaySocket = filepath.Join(t.TempDir(), "r.sock")

	// Track what the fake ssh writes
	markerFile := filepath.Join(dir, "marker-written")
	binDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(binDir, 0755)
	script := `#!/bin/sh
case "$*" in
  *mkdir*)
    echo "$2" >> ` + markerFile + `
    exit 0
    ;;
esac
exit 0
`
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	err := connectRemote(context.Background(), rc, nil)
	if err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	// Verify that the marker write SSH command was attempted
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("marker file not written: %v", err)
	}
	if !strings.Contains(string(data), "55555") {
		t.Errorf("marker command = %q, expected port 55555", string(data))
	}
}

func TestEnsureSSHConfig(t *testing.T) {
	rc, _ := makeTestEnv(t)
	cfg := &config.Config{
		DefaultContext: "test",
		Contexts: map[string]config.Context{
			"test": rc.Context,
		},
	}
	ensureSSHConfig(rc, cfg)
}

func TestConnectLocal_GateLostDuringPoll(t *testing.T) {
	rc, dir := makeTestEnv(t)

	// Create gate socket
	sockDir, err := os.MkdirTemp("/tmp", "jg-poll-")
	if err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(sockDir, "s.sock")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	rc.Derived.GateSocket = sockPath
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(dir, "hooks", "get-gate-token"),
		[]byte("#!/bin/sh\nprintf 'token'"), 0755)

	// Make ssh probe fail for remote but succeed for gate check.
	// Then close the gate socket to trigger the circuit breaker.
	binDir := filepath.Join(dir, "bin2")
	os.MkdirAll(binDir, 0755)
	// ssh fails for probe (true command) but succeeds for -O check
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte(`#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    true) exit 1;;
  esac
done
exit 0
`), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	// Close the socket after a brief delay to trigger circuit breaker
	go func() {
		time.Sleep(500 * time.Millisecond)
		l.Close()
		os.Remove(sockPath)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = connectLocal(ctx, rc, nil)
	if err == nil {
		t.Error("expected error from circuit breaker")
	}
}
