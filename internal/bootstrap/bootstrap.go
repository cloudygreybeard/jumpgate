package bootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"gopkg.in/yaml.v3"
)

// RemoteConfig builds a minimal remote-role config from a local context.
// If relay remote_port is 0 (auto), a random ephemeral port is assigned
// and written back to the source context so both sides agree on the port.
func RemoteConfig(contextName string, ctx *config.Context) *config.Config {
	uid := ctx.UID
	if uid == "" {
		uid = config.GenerateUID()
	}

	relayPort := ctx.Relay.RemotePort
	if relayPort == 0 {
		relayPort = rand.Intn(16384) + 49152
		ctx.Relay.RemotePort = relayPort
	}

	return &config.Config{
		DefaultContext: contextName,
		Contexts: map[string]config.Context{
			contextName: {
				UID:  uid,
				Role: "remote",
				Gate: config.GateConfig{
					Hostname: ctx.Gate.Hostname,
					Port:     ctx.Gate.Port,
				},
				Auth: config.AuthConfig{
					Type: "none",
					User: ctx.Auth.User,
				},
				Relay: config.RelayConfig{
					RemotePort: relayPort,
				},
			},
		},
	}
}

// Encode marshals a config to YAML, gzip-compresses it, and returns a
// base64 string suitable for clipboard pasting.
func Encode(cfg *config.Config) (string, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshalling config: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return "", fmt.Errorf("compressing: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("closing gzip: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// Decode takes a base64 string, gunzips it, and returns the raw YAML bytes.
func Decode(b64 string) ([]byte, error) {
	compressed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("invalid gzip data: %w", err)
	}
	defer gz.Close()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("decompressing: %w", err)
	}

	return data, nil
}

// DecodeConfig decodes a base64 bootstrap string and parses it as a Config.
func DecodeConfig(b64 string) (*config.Config, error) {
	data, err := Decode(b64)
	if err != nil {
		return nil, err
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if len(cfg.Contexts) == 0 {
		return nil, fmt.Errorf("bootstrap payload contains no contexts")
	}

	return &cfg, nil
}
