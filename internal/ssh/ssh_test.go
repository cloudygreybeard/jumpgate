package ssh

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFakeBin(t *testing.T, dir, name string, exitCode int) {
	t.Helper()
	script := "#!/bin/sh\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

func setupFakeSSH(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	writeFakeBin(t, dir, "ssh", exitCode)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func TestRunSSH_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	ctx := context.Background()
	if err := runSSH(ctx, "test", "-O", "check", "host"); err != nil {
		t.Errorf("runSSH: %v", err)
	}
}

func TestRunSSH_Failure(t *testing.T) {
	setupFakeSSH(t, 1)
	ctx := context.Background()
	if err := runSSH(ctx, "test", "-O", "check", "host"); err == nil {
		t.Error("expected error from failing ssh")
	}
}

func TestCheck_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if err := Check(context.Background(), "host"); err != nil {
		t.Errorf("Check: %v", err)
	}
}

func TestCheck_Failure(t *testing.T) {
	setupFakeSSH(t, 1)
	if err := Check(context.Background(), "host"); err == nil {
		t.Error("expected error from Check")
	}
}

func TestCheckSocket_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if err := CheckSocket(context.Background(), "host", "/tmp/test.sock"); err != nil {
		t.Errorf("CheckSocket: %v", err)
	}
}

func TestCheckSocket_Failure(t *testing.T) {
	setupFakeSSH(t, 1)
	if err := CheckSocket(context.Background(), "host", "/tmp/test.sock"); err == nil {
		t.Error("expected error from CheckSocket")
	}
}

func TestExit_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if err := Exit(context.Background(), "host"); err != nil {
		t.Errorf("Exit: %v", err)
	}
}

func TestExitSocket_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if err := ExitSocket(context.Background(), "host", "/tmp/test.sock"); err != nil {
		t.Errorf("ExitSocket: %v", err)
	}
}

func TestForward_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if err := Forward(context.Background(), "host", "8080:localhost:80"); err != nil {
		t.Errorf("Forward: %v", err)
	}
}

func TestCancelForward_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if err := CancelForward(context.Background(), "host", "8080:localhost:80"); err != nil {
		t.Errorf("CancelForward: %v", err)
	}
}

func TestOpenControlMaster_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if err := OpenControlMaster(context.Background(), "host"); err != nil {
		t.Errorf("OpenControlMaster: %v", err)
	}
}

func TestOpenRelay_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	socketPath := filepath.Join(t.TempDir(), "relay.sock")
	if err := OpenRelay(context.Background(), "host", socketPath); err != nil {
		t.Errorf("OpenRelay: %v", err)
	}
}

func TestOpenRelay_StaleSocket(t *testing.T) {
	setupFakeSSH(t, 0)
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "relay.sock")
	os.WriteFile(socketPath, []byte("stale"), 0644)
	if err := OpenRelay(context.Background(), "host", socketPath); err != nil {
		t.Errorf("OpenRelay with stale socket: %v", err)
	}
}

func TestOpenRelay_RemoveError(t *testing.T) {
	setupFakeSSH(t, 0)
	// Create a directory at the socket path that can't be removed by os.Remove
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "relay.sock")
	os.MkdirAll(socketPath, 0755)
	// Create a file inside so os.Remove fails
	os.WriteFile(filepath.Join(socketPath, "file"), []byte("x"), 0644)

	err := OpenRelay(context.Background(), "host", socketPath)
	if err == nil {
		t.Error("expected error from remove of non-empty directory")
	}
}

func TestProbe_Success(t *testing.T) {
	setupFakeSSH(t, 0)
	if !Probe(context.Background(), "host", "") {
		t.Error("expected Probe to succeed")
	}
}

func TestProbe_Failure(t *testing.T) {
	setupFakeSSH(t, 1)
	if Probe(context.Background(), "host", "") {
		t.Error("expected Probe to fail")
	}
}

func TestProbe_WithCCFile(t *testing.T) {
	setupFakeSSH(t, 0)
	if !Probe(context.Background(), "host", "/tmp/krb5cc_test") {
		t.Error("expected Probe with ccFile to succeed")
	}
}

func TestProbeHostname_Success(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'testhost'\n"
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	hostname, ok := ProbeHostname(context.Background(), "host", "")
	if !ok {
		t.Fatal("expected ProbeHostname to succeed")
	}
	if hostname != "testhost" {
		t.Errorf("hostname = %q, want %q", hostname, "testhost")
	}
}

func TestProbeHostname_WithCCFile(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'testhost'\n"
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	hostname, ok := ProbeHostname(context.Background(), "host", "/tmp/krb5cc_test")
	if !ok {
		t.Fatal("expected ProbeHostname to succeed")
	}
	if hostname != "testhost" {
		t.Errorf("hostname = %q, want %q", hostname, "testhost")
	}
}

func TestProbeHostname_Failure(t *testing.T) {
	setupFakeSSH(t, 1)
	_, ok := ProbeHostname(context.Background(), "host", "")
	if ok {
		t.Error("expected ProbeHostname to fail")
	}
}

func TestContextCancellation(t *testing.T) {
	setupFakeSSH(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Check(ctx, "host"); err == nil {
		t.Error("expected error from cancelled context")
	}
}
