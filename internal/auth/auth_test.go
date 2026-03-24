package auth

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func makeTestContext(t *testing.T) (*config.ResolvedContext, string) {
	t.Helper()
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0755)

	return &config.ResolvedContext{
		Name: "test",
		Context: config.Context{
			Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
			Auth: config.AuthConfig{
				Type:          "kerberos",
				Realm:         "EXAMPLE.COM",
				User:          "alice",
				KDC:           "kdc.example.com",
				KDCLocalPort:  8888,
				KDCRemotePort: 88,
				Kinit:         "kinit",
				CCFile:        filepath.Join(dir, "krb5cc_test"),
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
			ConfigDir:     dir,
		},
	}, dir
}

func setupFakeBins(t *testing.T, dir string) {
	t.Helper()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)

	// fake ssh that always succeeds
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	// fake klist that fails (no ticket)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	// fake kinit that succeeds
	os.WriteFile(filepath.Join(binDir, "kinit"), []byte("#!/bin/sh\nread _pw; exit 0\n"), 0755)
	// fake kdestroy
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)

	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
}

func TestSocketExists_NoFile(t *testing.T) {
	if socketExists("/nonexistent/path") {
		t.Error("expected false for nonexistent path")
	}
}

func TestSocketExists_RegularFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "regular")
	os.WriteFile(f, []byte("hello"), 0644)
	if socketExists(f) {
		t.Error("expected false for regular file")
	}
}

func TestSocketExists_RealSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("creating socket: %v", err)
	}
	defer l.Close()

	if !socketExists(sockPath) {
		t.Error("expected true for real socket")
	}
}

func TestHasValidTicket_NoTicket(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	if hasValidTicket(context.Background(), "/tmp/test_cc") {
		t.Error("expected no valid ticket")
	}
}

func TestHasValidTicket_ValidTicket(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	if !hasValidTicket(context.Background(), "/tmp/test_cc") {
		t.Error("expected valid ticket")
	}
}

func TestHasValidTicket_NoCCFile(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	if !hasValidTicket(context.Background(), "") {
		t.Error("expected valid ticket with empty ccFile")
	}
}

func TestRunKinit_Success(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "kinit"), []byte("#!/bin/sh\nread _pw; exit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	err := runKinit(context.Background(), "kinit", "alice@EXAMPLE.COM", "secret", filepath.Join(dir, "cc"), "")
	if err != nil {
		t.Errorf("runKinit: %v", err)
	}
}

func TestRunKinit_Failure(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "kinit"), []byte("#!/bin/sh\nread _pw; exit 1\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	err := runKinit(context.Background(), "kinit", "alice@EXAMPLE.COM", "secret", filepath.Join(dir, "cc"), "")
	if err == nil {
		t.Error("expected error from failing kinit")
	}
}

func TestDestroyTicket_Success(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	// Should not panic
	destroyTicket(context.Background(), "/tmp/test_cc")
}

func TestDestroyTicket_Failure(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	// Should not panic even on failure
	destroyTicket(context.Background(), "")
}

func TestOpenGateSession(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc, _ := makeTestContext(t)
	err := openGateSession(context.Background(), rc, "mock-token")
	if err != nil {
		t.Errorf("openGateSession: %v", err)
	}
}

func TestOpenGateSession_SSHFails(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc, _ := makeTestContext(t)
	err := openGateSession(context.Background(), rc, "mock-token")
	if err == nil {
		t.Error("expected error when ssh fails")
	}
}

func TestEnsureGate_AlreadyConnected(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)

	// Use /tmp for short socket paths (macOS 104-byte limit)
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

	err = EnsureGate(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureGate: %v", err)
	}
}

func TestEnsureGate_NewConnection(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)

	// Create get-gate-token hook
	hookPath := filepath.Join(dir, "hooks", "get-gate-token")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nprintf 'mock-token'"), 0755)

	err := EnsureGate(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureGate: %v", err)
	}
}

func TestEnsureKerberos_SkipForKey(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)
	rc.Context.Auth.Type = "key"

	err := EnsureKerberos(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureKerberos: %v", err)
	}
}

func TestEnsureKerberos_SkipForNone(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)
	rc.Context.Auth.Type = "none"

	err := EnsureKerberos(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureKerberos: %v", err)
	}
}

func TestEnsureKerberos_SkipForEmpty(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)
	rc.Context.Auth.Type = ""

	err := EnsureKerberos(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureKerberos: %v", err)
	}
}

func TestEnsureKerberos_AlreadyValid(t *testing.T) {
	rc, dir := makeTestContext(t)
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	// klist succeeds = ticket is valid
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	err := EnsureKerberos(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureKerberos: %v", err)
	}
}

func TestEnsureKerberos_AcquireTicket(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)

	// Create get-krb-password hook
	hookPath := filepath.Join(dir, "hooks", "get-krb-password")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nprintf 'mock-password'"), 0755)

	err := EnsureKerberos(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureKerberos: %v", err)
	}
}

func TestEnsureGate_CredentialsMissing(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)

	// Create check-credentials hook that fails (credentials missing)
	os.WriteFile(filepath.Join(dir, "hooks", "check-credentials"),
		[]byte("#!/bin/sh\nexit 1"), 0755)
	// Create setup-credentials hook
	os.WriteFile(filepath.Join(dir, "hooks", "setup-credentials"),
		[]byte("#!/bin/sh\ntrue"), 0755)
	// Create get-gate-token hook
	os.WriteFile(filepath.Join(dir, "hooks", "get-gate-token"),
		[]byte("#!/bin/sh\nprintf 'token'"), 0755)

	err := EnsureGate(context.Background(), rc)
	if err != nil {
		t.Errorf("EnsureGate: %v", err)
	}
}

func TestEnsureGate_SetupCredentialsFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)

	// check-credentials fails
	os.WriteFile(filepath.Join(dir, "hooks", "check-credentials"),
		[]byte("#!/bin/sh\nexit 1"), 0755)
	// setup-credentials fails
	os.WriteFile(filepath.Join(dir, "hooks", "setup-credentials"),
		[]byte("#!/bin/sh\nexit 1"), 0755)

	err := EnsureGate(context.Background(), rc)
	if err == nil {
		t.Error("expected error when setup-credentials fails")
	}
}

func TestEnsureGate_GetTokenFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)

	// get-gate-token fails
	os.WriteFile(filepath.Join(dir, "hooks", "get-gate-token"),
		[]byte("#!/bin/sh\nexit 1"), 0755)

	err := EnsureGate(context.Background(), rc)
	if err == nil {
		t.Error("expected error when get-gate-token fails")
	}
}

func TestEnsureKerberos_ForwardFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	// klist fails (no ticket)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	// ssh fails (forward fails)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	err := EnsureKerberos(context.Background(), rc)
	if err == nil {
		t.Error("expected error when KDC forward fails")
	}
}

func TestEnsureKerberos_GetPasswordFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	// No get-krb-password hook

	err := EnsureKerberos(context.Background(), rc)
	if err == nil {
		t.Error("expected error when get-krb-password hook is missing")
	}
}

func TestEnsureKerberos_KinitFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "klist"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kinit"), []byte("#!/bin/sh\nread _pw; exit 1\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	os.WriteFile(filepath.Join(dir, "hooks", "get-krb-password"),
		[]byte("#!/bin/sh\nprintf 'password'"), 0755)

	err := EnsureKerberos(context.Background(), rc)
	if err == nil {
		t.Error("expected error when kinit fails")
	}
}

func TestCloseGate_NotConnected(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)
	rc.Derived.GateSocket = filepath.Join(dir, "nonexistent.sock")

	// Should not panic
	CloseGate(context.Background(), rc)
}

func TestCloseGate_ExitFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	// ssh fails (Exit returns error -> "already gone" branch)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	sockDir, err := os.MkdirTemp("/tmp", "jg-")
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
	defer l.Close()

	CloseGate(context.Background(), rc)
}

func TestCloseGate_Connected(t *testing.T) {
	rc, dir := makeTestContext(t)
	setupFakeBins(t, dir)

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

	CloseGate(context.Background(), rc)
}
