package connect

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDisconnect_Local(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0755)

	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	rc.Derived.GateSocket = filepath.Join(dir, "gate.sock")
	rc.Derived.ConfigDir = dir

	// No socket exists, should still run without panic
	Disconnect(context.Background(), rc)
}

func TestDisconnect_LocalWithSocket(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0755)

	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	rc.Derived.ConfigDir = dir
	sockDir, err := os.MkdirTemp("/tmp", "jg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "s.sock")
	rc.Derived.GateSocket = sockPath

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	Disconnect(context.Background(), rc)
}

func TestDisconnectRemote(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)

	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	rc.Derived.GateSocket = ""
	rc.Derived.RelaySocket = filepath.Join(dir, "relay.sock")

	disconnectRemote(context.Background(), rc)
}

func TestDisconnectRemote_WithSocket(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)

	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
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
		t.Fatal(err)
	}
	defer l.Close()

	disconnectRemote(context.Background(), rc)
}

func TestDisconnectRemoteSide(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)

	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	// Should not panic even if ssh fails
	DisconnectRemoteSide(context.Background(), rc)
}

func TestDisconnectAll(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0755)

	os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(binDir, "kdestroy"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	rc := makeResolvedContext()
	rc.Derived.ConfigDir = dir
	rc.Derived.GateSocket = filepath.Join(dir, "nonexistent.sock")

	DisconnectAll(context.Background(), rc)
}
