package bootstrap

import (
	"strings"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"gopkg.in/yaml.v3"
)

func TestRemoteConfig(t *testing.T) {
	local := &config.Context{
		Role: "local",
		Gate: config.GateConfig{Hostname: "bastion.example.com", Port: 330},
		Auth: config.AuthConfig{
			Type:  "kerberos",
			User:  "alice",
			Realm: "EXAMPLE.COM",
			KDC:   "kdc.example.com",
		},
		Remote: config.RemoteConfig{User: "b-alice", Key: "~/.ssh/id_ed25519"},
		Relay:  config.RelayConfig{RemotePort: 54321},
	}

	rc := RemoteConfig("devbox", local)

	if rc.DefaultContext != "devbox" {
		t.Errorf("default_context = %q, want devbox", rc.DefaultContext)
	}

	ctx, ok := rc.Contexts["devbox"]
	if !ok {
		t.Fatal("missing devbox context")
	}

	if ctx.Role != "remote" {
		t.Errorf("role = %q, want remote", ctx.Role)
	}
	if ctx.Auth.Type != "none" {
		t.Errorf("auth.type = %q, want none", ctx.Auth.Type)
	}
	if ctx.Auth.User != "alice" {
		t.Errorf("auth.user = %q, want alice", ctx.Auth.User)
	}
	if ctx.Gate.Hostname != "bastion.example.com" {
		t.Errorf("gate.hostname = %q, want bastion.example.com", ctx.Gate.Hostname)
	}
	if ctx.Gate.Port != 330 {
		t.Errorf("gate.port = %d, want 330", ctx.Gate.Port)
	}
	if ctx.Relay.RemotePort != 54321 {
		t.Errorf("relay.remote_port = %d, want 54321", ctx.Relay.RemotePort)
	}
	if ctx.Auth.Realm != "" {
		t.Errorf("auth.realm should be empty for remote, got %q", ctx.Auth.Realm)
	}
	if ctx.UID == "" {
		t.Error("remote context should have a UID")
	}
}

func TestRemoteConfigPreservesSourceUID(t *testing.T) {
	local := &config.Context{
		UID:  "source-uid-1234",
		Role: "local",
		Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
		Auth: config.AuthConfig{User: "alice"},
	}

	rc := RemoteConfig("myctx", local)
	ctx := rc.Contexts["myctx"]
	if ctx.UID != "source-uid-1234" {
		t.Errorf("UID = %q, want source-uid-1234 (should copy from source)", ctx.UID)
	}
}

func TestRemoteConfigGeneratesUIDWhenMissing(t *testing.T) {
	local := &config.Context{
		Role: "local",
		Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
		Auth: config.AuthConfig{User: "alice"},
	}

	rc := RemoteConfig("myctx", local)
	ctx := rc.Contexts["myctx"]
	if ctx.UID == "" {
		t.Error("UID should be auto-generated when source has none")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cfg := &config.Config{
		DefaultContext: "test",
		Contexts: map[string]config.Context{
			"test": {
				Role:  "remote",
				Gate:  config.GateConfig{Hostname: "gw.example.com", Port: 22},
				Auth:  config.AuthConfig{Type: "none", User: "bob"},
				Relay: config.RelayConfig{RemotePort: 50000},
			},
		},
	}

	b64, err := Encode(cfg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if len(b64) == 0 {
		t.Fatal("Encode returned empty string")
	}
	t.Logf("encoded length: %d chars", len(b64))

	decoded, err := DecodeConfig(b64)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}

	if decoded.DefaultContext != "test" {
		t.Errorf("default_context = %q, want test", decoded.DefaultContext)
	}

	ctx := decoded.Contexts["test"]
	if ctx.Gate.Hostname != "gw.example.com" {
		t.Errorf("gate.hostname = %q, want gw.example.com", ctx.Gate.Hostname)
	}
	if ctx.Relay.RemotePort != 50000 {
		t.Errorf("relay.remote_port = %d, want 50000", ctx.Relay.RemotePort)
	}
}

func TestDecodeInvalidBase64(t *testing.T) {
	_, err := Decode("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecodeInvalidGzip(t *testing.T) {
	_, err := DecodeConfig("aGVsbG8=") // "hello" — valid base64 but not gzip
	if err == nil {
		t.Fatal("expected error for non-gzip data")
	}
}

func TestDecodeConfigNoContexts(t *testing.T) {
	cfg := &config.Config{DefaultContext: "x"}
	b64, err := Encode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeConfig(b64)
	if err == nil {
		t.Fatal("expected error for empty contexts")
	}
}

func TestEncodedPayloadIsSmall(t *testing.T) {
	ctx := &config.Context{
		Role:  "local",
		Gate:  config.GateConfig{Hostname: "bastion-rdu2.redhat.com", Port: 330},
		Auth:  config.AuthConfig{Type: "kerberos", User: "phijones"},
		Relay: config.RelayConfig{RemotePort: 55555},
	}

	rc := RemoteConfig("devbox", ctx)
	b64, err := Encode(rc)
	if err != nil {
		t.Fatal(err)
	}

	if len(b64) > 500 {
		t.Errorf("payload too large for clipboard paste: %d chars (want < 500)", len(b64))
	}
	t.Logf("realistic payload size: %d chars", len(b64))
}

func TestDecodedYAMLIsClean(t *testing.T) {
	cfg := &config.Config{
		DefaultContext: "devbox",
		Contexts: map[string]config.Context{
			"devbox": {
				Role:  "remote",
				Gate:  config.GateConfig{Hostname: "gw.example.com", Port: 330},
				Auth:  config.AuthConfig{Type: "none", User: "alice"},
				Relay: config.RelayConfig{RemotePort: 54321},
			},
		},
	}

	b64, _ := Encode(cfg)
	raw, _ := Decode(b64)

	yamlStr := string(raw)
	if !strings.Contains(yamlStr, "role: remote") {
		t.Error("decoded YAML missing role: remote")
	}

	var roundTrip config.Config
	if err := yaml.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("round-trip YAML parse: %v", err)
	}
	if roundTrip.Contexts["devbox"].Gate.Port != 330 {
		t.Error("round-trip lost gate.port")
	}
}
