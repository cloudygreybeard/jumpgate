package connect

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPollDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
		{4, 30 * time.Second},
		{5, 30 * time.Second},
		{10, 30 * time.Second},
		{100, 30 * time.Second},
	}
	for _, tt := range tests {
		got := pollDelay(tt.attempt)
		if got != tt.want {
			t.Errorf("pollDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestSocketExists_NoFile(t *testing.T) {
	if socketExists("/nonexistent/path/to/socket") {
		t.Error("expected socketExists to return false for nonexistent path")
	}
}

func TestSocketExists_RegularFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "notasocket")
	os.WriteFile(f, []byte("hello"), 0644)
	if socketExists(f) {
		t.Error("expected socketExists to return false for regular file")
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
		t.Error("expected socketExists to return true for real socket")
	}
}

func TestIsLocal(t *testing.T) {
	rc := makeResolvedContext()
	rc.Context.Role = "local"
	if !rc.IsLocal() {
		t.Error("expected IsLocal()=true when Role is local")
	}

	rc.Context.Role = "remote"
	if rc.IsLocal() {
		t.Error("expected IsLocal()=false when Role is remote")
	}
}
