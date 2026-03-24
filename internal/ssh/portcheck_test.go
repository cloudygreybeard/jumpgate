package ssh

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPortAvailable_Free(t *testing.T) {
	dir := t.TempDir()
	// Fake ssh that outputs ss -tln output without the port
	script := `#!/bin/sh
cat <<'EOF'
State  Recv-Q Send-Q Local Address:Port  Peer Address:Port
LISTEN 0      128    0.0.0.0:22          0.0.0.0:*
EOF
`
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	err := CheckPortAvailable(context.Background(), "test-gate", 55000)
	if err != nil {
		t.Errorf("expected port to be free: %v", err)
	}
}

func TestCheckPortAvailable_InUse(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
cat <<'EOF'
State  Recv-Q Send-Q Local Address:Port  Peer Address:Port
LISTEN 0      128    0.0.0.0:22          0.0.0.0:*
LISTEN 0      128    0.0.0.0:55000       0.0.0.0:*
EOF
`
	os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	err := CheckPortAvailable(context.Background(), "test-gate", 55000)
	if err == nil {
		t.Error("expected error for port in use")
	}
}

func TestCheckPortAvailable_SSHFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	err := CheckPortAvailable(context.Background(), "test-gate", 55000)
	if err != nil {
		t.Error("should return nil (skip) when ssh fails")
	}
}
