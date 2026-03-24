package connect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWatch_NoActiveRelay(t *testing.T) {
	setupFakeBins(t)
	rc := makeResolvedContext()
	rc.Derived.RelaySocket = "/tmp/nonexistent.sock"

	err := Watch(context.Background(), rc)
	if err == nil {
		t.Error("expected error for no active relay")
	}
}

func TestWatch_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	// Verify our fake ssh is discoverable
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatalf("ssh not found in PATH: %v", err)
	}
	if sshPath != filepath.Join(binDir, "ssh") {
		t.Logf("ssh resolved to %s (expected %s)", sshPath, filepath.Join(binDir, "ssh"))
	}

	rc := makeResolvedContext()
	sockDir, err := os.MkdirTemp("/tmp", "jg-w-")
	if err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(sockDir, "s.sock")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	rc.Derived.RelaySocket = sockPath

	l, err := createTestSocket(t, sockPath)
	if err != nil {
		t.Fatalf("creating socket: %v", err)
	}
	defer l.Close()

	// Verify socket exists before calling Watch
	if !socketExists(sockPath) {
		t.Fatal("socket should exist before Watch")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err = Watch(ctx, rc)
	if err != nil {
		t.Errorf("Watch with cancelled context: %v", err)
	}
}
