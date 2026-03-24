package connect

import (
	"net"
	"testing"
)

func createTestSocket(t *testing.T, path string) (net.Listener, error) {
	t.Helper()
	return net.Listen("unix", path)
}
