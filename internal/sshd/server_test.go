package sshd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

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
