package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func makeResolvedContext() *config.ResolvedContext {
	return &config.ResolvedContext{
		Name: "test",
		Context: config.Context{
			Role:  "local",
			Gate:  config.GateConfig{Hostname: "gw.example.com", Port: 22},
			Auth:  config.AuthConfig{User: "alice", Type: "kerberos", Realm: "EXAMPLE.COM", CCFile: "/tmp/krb5cc_test"},
			Relay: config.RelayConfig{RemotePort: 55555},
		},
		Derived: config.Derived{
			ContextName: "test",
			GateHost:    "test-gate",
			RemoteHost:  "test",
			RelayHost:   "test-relay",
			GateSocket:  "/tmp/nonexistent-gate.sock",
			RelaySocket: "/tmp/nonexistent-relay.sock",
			ConfigDir:   "/tmp/test-config",
		},
	}
}

func setupFakeBins(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"ssh", "klist", "kdestroy", "kinit", "systemctl"} {
		script := "#!/bin/sh\nexit 1\n"
		os.WriteFile(filepath.Join(dir, name), []byte(script), 0755)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func TestCollectStatusLocal_GateDown(t *testing.T) {
	setupFakeBins(t)
	rc := makeResolvedContext()
	rc.Derived.GateSocket = "/tmp/nonexistent.sock"

	info := CollectStatus(context.Background(), rc)
	if info.Gate.Connected {
		t.Error("expected gate not connected")
	}
	if info.Remote.Reachable {
		t.Error("expected remote not reachable when gate is down")
	}
	if info.Auth.Valid {
		t.Error("expected auth not valid with fake klist")
	}
}

func TestCollectTicketStatus_Valid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "klist"), []byte("#!/bin/sh\nif [ \"$1\" = \"-s\" ]; then exit 0; fi\necho 'Default principal: alice@EXAMPLE.COM'\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	status := collectTicketStatus(context.Background(), "/tmp/krb5cc_test")
	if !status.Valid {
		t.Error("expected ticket to be valid")
	}
}

func TestCollectTicketStatus_Invalid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	status := collectTicketStatus(context.Background(), "/tmp/krb5cc_test")
	if status.Valid {
		t.Error("expected ticket to be invalid")
	}
	if !strings.Contains(status.Detail, "/tmp/krb5cc_test") {
		t.Errorf("expected detail to mention ccache path, got: %q", status.Detail)
	}
}

func TestCollectStatusRemote(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ssh", "klist", "systemctl"} {
		os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0755)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	rc.Derived.GateSocket = ""
	rc.Derived.RelaySocket = "/tmp/nonexistent.sock"

	info := collectStatusRemote(context.Background(), rc)
	if info.ContextName != "test" {
		t.Errorf("context = %q", info.ContextName)
	}
	if !info.IsRemote {
		t.Error("expected IsRemote=true")
	}
	if info.Relay.Active {
		t.Error("relay should not be active")
	}
}

func TestCollectStatusRemote_AllUp(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(dir, "klist"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	rc.Derived.GateSocket = ""

	sockDir, err := os.MkdirTemp("/tmp", "jg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "s.sock")
	rc.Derived.RelaySocket = sockPath
	l, _ := createTestSocket(t, sockPath)
	defer l.Close()

	info := collectStatusRemote(context.Background(), rc)
	if !info.Relay.Active {
		t.Error("relay should be active")
	}
	if !info.SSH.Running {
		t.Error("ssh should be running")
	}
}

func TestCollectStatusLocal_GateUp(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(dir, "klist"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	sockDir, err := os.MkdirTemp("/tmp", "jg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "s.sock")
	rc.Derived.GateSocket = sockPath
	l, _ := createTestSocket(t, sockPath)
	defer l.Close()

	info := collectStatusLocal(context.Background(), rc)
	if !info.Gate.Connected {
		t.Error("gate should be connected")
	}
	if !info.Remote.Reachable {
		t.Error("remote should be reachable")
	}
}

func TestCollectStatusLocal_GateSocketCheckFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	os.WriteFile(filepath.Join(dir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	sockDir, err := os.MkdirTemp("/tmp", "jg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "s.sock")
	rc.Derived.GateSocket = sockPath
	l, _ := createTestSocket(t, sockPath)
	defer l.Close()

	info := collectStatusLocal(context.Background(), rc)
	if info.Gate.Connected {
		t.Error("gate should not be connected (check failed)")
	}
	if !strings.Contains(info.Gate.Detail, "check failed") {
		t.Errorf("expected check failed detail, got: %q", info.Gate.Detail)
	}
}

func TestCollectTicketStatus_NoCCFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	status := collectTicketStatus(context.Background(), "")
	if status.Valid {
		t.Error("expected ticket to be invalid")
	}
	if status.Detail != "no valid ticket" {
		t.Errorf("expected 'no valid ticket', got: %q", status.Detail)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestPrintStatus_LocalGateDown(t *testing.T) {
	info := StatusInfo{
		ContextName: "test",
		IsLocal:     true,
		Gate:        GateStatus{Connected: false},
		Auth:        AuthStatus{Valid: false, Detail: "no valid ticket"},
		Remote:      RemoteStatus{Reachable: false, Detail: "gate not connected"},
	}

	output := captureStdout(t, func() { PrintStatus(info) })

	if !strings.Contains(output, "Session: DOWN") {
		t.Error("expected 'Session: DOWN' in output")
	}
	if !strings.Contains(output, "Ticket: NONE / EXPIRED") {
		t.Error("expected 'Ticket: NONE / EXPIRED' in output")
	}
	if !strings.Contains(output, "Reachable: NO") {
		t.Error("expected 'Reachable: NO' in output")
	}
}

func TestPrintStatus_LocalGateUp(t *testing.T) {
	info := StatusInfo{
		ContextName: "test",
		IsLocal:     true,
		Gate:        GateStatus{Connected: true, Detail: "session active"},
		Auth:        AuthStatus{Valid: true, Detail: "Default principal: alice@EXAMPLE.COM"},
		Remote:      RemoteStatus{Reachable: true},
	}

	output := captureStdout(t, func() { PrintStatus(info) })

	if !strings.Contains(output, "Session: UP") {
		t.Error("expected 'Session: UP' in output")
	}
	if !strings.Contains(output, "Ticket: VALID") {
		t.Error("expected 'Ticket: VALID' in output")
	}
	if !strings.Contains(output, "Reachable: YES") {
		t.Error("expected 'Reachable: YES' in output")
	}
}

func TestPrintStatus_Relay(t *testing.T) {
	info := StatusInfo{
		ContextName: "test",
		IsRemote:    true,
		Relay:       RelayStatus{Active: true, RemotePort: 55555},
		Auth:        AuthStatus{Valid: true},
		SSH:         SSHStatus{Running: true},
	}

	output := captureStdout(t, func() { PrintStatus(info) })

	if !strings.Contains(output, "Status: UP") {
		t.Error("expected relay 'Status: UP' in output")
	}
	if !strings.Contains(output, "55555") {
		t.Error("expected relay port in output")
	}
	if !strings.Contains(output, "sshd: running") {
		t.Error("expected sshd running in output")
	}
}

func TestPrintStatus_RelayDown(t *testing.T) {
	info := StatusInfo{
		ContextName: "test",
		IsRemote:    true,
		Relay:       RelayStatus{Active: false},
		Auth:        AuthStatus{Valid: false, Detail: "no valid ticket"},
		SSH:         SSHStatus{Running: false},
	}

	output := captureStdout(t, func() { PrintStatus(info) })

	if !strings.Contains(output, "Status: DOWN") {
		t.Error("expected relay 'Status: DOWN' in output")
	}
	if !strings.Contains(output, "sshd: stopped") {
		t.Error("expected sshd stopped in output")
	}
}

func TestPrintStatusJSON(t *testing.T) {
	info := StatusInfo{
		ContextName: "test",
		Gate:        GateStatus{Connected: true},
		Auth:        AuthStatus{Valid: true, CCFile: "/tmp/krb5cc_test"},
		Remote:      RemoteStatus{Reachable: true},
	}

	output := captureStdout(t, func() {
		if err := PrintStatusJSON(info); err != nil {
			t.Fatal(err)
		}
	})

	var parsed StatusInfo
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, output)
	}
	if parsed.ContextName != "test" {
		t.Errorf("JSON context = %q, want %q", parsed.ContextName, "test")
	}
	if !parsed.Gate.Connected {
		t.Error("JSON gate.connected should be true")
	}
	if !parsed.Auth.Valid {
		t.Error("JSON auth.valid should be true")
	}
	if parsed.Auth.CCFile != "/tmp/krb5cc_test" {
		t.Errorf("JSON auth.cc_file = %q", parsed.Auth.CCFile)
	}
}
