package ssh

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRelayMarker(t *testing.T) {
	dir := t.TempDir()
	// Fake ssh that captures the command and writes a file to simulate the write
	script := `#!/bin/sh
eval "$2"
`
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("HOME", dir)

	err := WriteRelayMarker(context.Background(), "work-gate", "work", 55000)
	if err != nil {
		t.Fatalf("WriteRelayMarker: %v", err)
	}

	// Check that the marker file was created
	data, err := os.ReadFile(filepath.Join(dir, ".jumpgate", "relay-work.port"))
	if err != nil {
		t.Fatalf("marker file not created: %v", err)
	}
	if got := string(data); got != "55000\n" {
		t.Errorf("marker content = %q, want %q", got, "55000\n")
	}
}

func TestReadRelayMarker(t *testing.T) {
	dir := t.TempDir()
	// Fake ssh that outputs a port number
	script := `#!/bin/sh
echo 55000
`
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	port, err := ReadRelayMarker(context.Background(), "work-gate", "work")
	if err != nil {
		t.Fatalf("ReadRelayMarker: %v", err)
	}
	if port != 55000 {
		t.Errorf("port = %d, want 55000", port)
	}
}

func TestReadRelayMarker_NoFile(t *testing.T) {
	dir := t.TempDir()
	// Fake ssh that outputs nothing (file doesn't exist, cat fails silently)
	script := "#!/bin/sh\n"
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	port, err := ReadRelayMarker(context.Background(), "work-gate", "work")
	if err != nil {
		t.Fatalf("ReadRelayMarker: %v", err)
	}
	if port != 0 {
		t.Errorf("port = %d, want 0 (no marker)", port)
	}
}

func TestReadRelayMarker_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	// Fake ssh that outputs non-numeric content
	script := "#!/bin/sh\necho 'not-a-port'\n"
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	_, err := ReadRelayMarker(context.Background(), "work-gate", "work")
	if err == nil {
		t.Error("expected error for invalid marker content")
	}
}

func TestReadRelayMarker_SSHFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	port, err := ReadRelayMarker(context.Background(), "work-gate", "work")
	if err != nil {
		t.Fatalf("should return 0, nil when SSH fails: %v", err)
	}
	if port != 0 {
		t.Errorf("port = %d, want 0", port)
	}
}

func TestRemoveRelayMarker(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	err := RemoveRelayMarker(context.Background(), "work-gate", "work")
	if err != nil {
		t.Errorf("RemoveRelayMarker: %v", err)
	}
}

func TestRemoveRelayMarker_SSHFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\necho 'error' >&2; exit 1\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	err := RemoveRelayMarker(context.Background(), "work-gate", "work")
	if err == nil {
		t.Error("expected error when SSH fails")
	}
}

func TestMarkerPath(t *testing.T) {
	p := markerPath("my-context")
	if p != "$HOME/.jumpgate/relay-my-context.port" {
		t.Errorf("markerPath = %q", p)
	}
}
