package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func TestGenerateSSHConfigLocal(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	socketDir := filepath.Join(dir, "sockets")

	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth: config.AuthConfig{User: "alice", Type: "kerberos", Realm: "EXAMPLE.COM"},
				Remote: config.RemoteConfig{
					User: "alice-remote",
					Key:  "~/.ssh/id_ed25519",
				},
				Relay: config.RelayConfig{RemotePort: 55555},
			},
		},
	}

	snippets := map[string]string{
		"gate-local.conf.tpl": `Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}
  HostName {{.Hostname}}
  User {{.User}}
  Port {{.Port}}
  ControlPath {{.SocketDir}}/{{.Context}}-gate.sock
`,
		"remote-local.conf.tpl": `Host {{.Context}}{{if .IsDefault}} remote{{end}}
  HostName localhost
  User {{.RemoteUser}}
  Port {{.RelayPort}}
  IdentityFile {{.RemoteKey}}
  ProxyJump {{.Context}}-gate
`,
	}

	if err := GenerateSSHConfig(cfg, configDir, socketDir, "local", snippets); err != nil {
		t.Fatal(err)
	}

	output, err := os.ReadFile(filepath.Join(configDir, "ssh", "config.local"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(output)

	if !strings.Contains(content, "Host work-gate gate") {
		t.Error("missing work-gate gate combined host entry")
	}
	if !strings.Contains(content, "HostName gw.example.com") {
		t.Error("missing hostname substitution")
	}
	if !strings.Contains(content, "User alice") {
		t.Error("missing user substitution")
	}
	if !strings.Contains(content, "Host work remote") {
		t.Error("missing work remote combined host entry")
	}
}
