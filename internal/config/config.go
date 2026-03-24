package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DefaultContext string             `yaml:"default_context"`
	Contexts       map[string]Context `yaml:"contexts"`
}

type Context struct {
	Role       string          `json:"role" yaml:"role"`
	Gate       GateConfig      `json:"gate" yaml:"gate"`
	Auth       AuthConfig      `json:"auth" yaml:"auth"`
	Remote     RemoteConfig    `json:"remote" yaml:"remote"`
	Relay      RelayConfig     `json:"relay" yaml:"relay"`
	Keychain   KeychainConfig  `json:"keychain" yaml:"keychain"`
	TOTP       TOTPConfig      `json:"totp" yaml:"totp"`
	Container  ContainerConfig `json:"container" yaml:"container"`
	WindowsApp string          `json:"windows_app" yaml:"windows_app"`
}

type GateConfig struct {
	Hostname string `json:"hostname" yaml:"hostname"`
	Port     int    `json:"port" yaml:"port"`
}

type AuthConfig struct {
	Type          string `json:"type" yaml:"type"`
	Realm         string `json:"realm" yaml:"realm"`
	User          string `json:"user" yaml:"user"`
	KDC           string `json:"kdc" yaml:"kdc"`
	KDCLocalPort  int    `json:"kdc_local_port" yaml:"kdc_local_port"`
	KDCRemotePort int    `json:"kdc_remote_port" yaml:"kdc_remote_port"`
	Kinit         string `json:"kinit" yaml:"kinit"`
	CCFile        string `json:"cc_file" yaml:"cc_file"`
}

type RemoteConfig struct {
	User      string `json:"user" yaml:"user"`
	Key       string `json:"key" yaml:"key"`
	RemoteDir string `json:"remote_dir" yaml:"remote_dir"`
}

type RelayConfig struct {
	RemotePort int `json:"remote_port" yaml:"-"`
}

func (r *RelayConfig) UnmarshalYAML(value *yaml.Node) error {
	// Handle the case where remote_port is a string like "auto"
	var raw struct {
		RemotePort yaml.Node `yaml:"remote_port"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.RemotePort.Value == "" || raw.RemotePort.Value == "auto" {
		r.RemotePort = 0
		return nil
	}
	n, err := strconv.Atoi(raw.RemotePort.Value)
	if err != nil {
		return fmt.Errorf("invalid remote_port %q: must be a number or \"auto\"", raw.RemotePort.Value)
	}
	r.RemotePort = n
	return nil
}

func (r RelayConfig) MarshalYAML() (interface{}, error) {
	return struct {
		RemotePort int `yaml:"remote_port"`
	}{RemotePort: r.RemotePort}, nil
}

type KeychainConfig struct {
	TOTPService string `json:"totp_service" yaml:"totp_service"`
	TOTPAccount string `json:"totp_account" yaml:"totp_account"`
	KRBService  string `json:"krb_service" yaml:"krb_service"`
	KRBAccount  string `json:"krb_account" yaml:"krb_account"`
}

type TOTPConfig struct {
	CLI       string `json:"cli" yaml:"cli"`
	Namespace string `json:"namespace" yaml:"namespace"`
	Account   string `json:"account" yaml:"account"`
}

type ContainerConfig struct {
	Runtime string `json:"runtime" yaml:"runtime"`
	Image   string `json:"image" yaml:"image"`
}

// Derived holds values computed from the context config at runtime.
type Derived struct {
	ContextName   string `json:"context_name" yaml:"context_name"`
	AuthPrincipal string `json:"auth_principal" yaml:"auth_principal"`
	GateHost      string `json:"gate_host" yaml:"gate_host"`
	RemoteHost    string `json:"remote_host" yaml:"remote_host"`
	RelayHost     string `json:"relay_host" yaml:"relay_host"`
	GateSocket    string `json:"gate_socket,omitempty" yaml:"gate_socket,omitempty"`
	RelaySocket   string `json:"relay_socket,omitempty" yaml:"relay_socket,omitempty"`
	ConfigDir     string `json:"config_dir" yaml:"config_dir"`
}

// ResolvedContext bundles a Context with its Derived values.
type ResolvedContext struct {
	Name    string
	Context Context
	Derived Derived
}

// DefaultConfigDir returns the platform-appropriate config directory.
func DefaultConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "jumpgate")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "jumpgate")
}

// DefaultConfigFile returns the default config file path.
func DefaultConfigFile() string {
	return filepath.Join(DefaultConfigDir(), "config.yaml")
}

// Load reads and parses the config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]Context)
	}

	if cfg.DefaultContext == "" {
		cfg.DefaultContext = "default"
	}

	return &cfg, nil
}

// Resolve selects a context by name (or the default) and applies defaults.
func (c *Config) Resolve(contextName string) (*ResolvedContext, error) {
	if contextName == "" {
		contextName = c.DefaultContext
	}

	ctx, ok := c.Contexts[contextName]
	if !ok {
		available := make([]string, 0, len(c.Contexts))
		for k := range c.Contexts {
			available = append(available, k)
		}
		return nil, fmt.Errorf("context %q not found (available: %s)",
			contextName, strings.Join(available, ", "))
	}

	applyDefaults(&ctx, contextName)

	configDir := DefaultConfigDir()
	home, _ := os.UserHomeDir()

	d := Derived{
		ContextName:   contextName,
		AuthPrincipal: ctx.Auth.User + "@" + ctx.Auth.Realm,
		GateHost:      contextName + "-gate",
		RemoteHost:    contextName,
		RelayHost:     contextName + "-relay",
		ConfigDir:     configDir,
	}

	socketDir := filepath.Join(home, ".ssh", "sockets")
	if ctx.Role == "local" {
		d.GateSocket = filepath.Join(socketDir, contextName+"-gate.sock")
	} else {
		d.RelaySocket = filepath.Join(socketDir, contextName+"-relay.sock")
	}

	return &ResolvedContext{
		Name:    contextName,
		Context: ctx,
		Derived: d,
	}, nil
}

func applyDefaults(ctx *Context, contextName string) {
	if ctx.Role == "" {
		ctx.Role = "local"
	}
	if ctx.Gate.Port == 0 {
		ctx.Gate.Port = 22
	}
	if ctx.Auth.Type == "" {
		ctx.Auth.Type = "key"
	}
	if ctx.Auth.KDCLocalPort == 0 {
		ctx.Auth.KDCLocalPort = 8888
	}
	if ctx.Auth.KDCRemotePort == 0 {
		ctx.Auth.KDCRemotePort = 88
	}
	if ctx.Auth.Kinit == "" {
		ctx.Auth.Kinit = "kinit"
	}
	if ctx.Auth.CCFile == "" {
		ctx.Auth.CCFile = "/tmp/krb5cc_" + contextName
	}
	if ctx.Remote.RemoteDir == "" {
		ctx.Remote.RemoteDir = "~/jumpgate"
	}
	if ctx.Container.Runtime == "" {
		ctx.Container.Runtime = "podman"
	}
	if ctx.Container.Image == "" {
		ctx.Container.Image = "jumpgate-kinit"
	}
	if ctx.WindowsApp == "" {
		ctx.WindowsApp = "Windows App"
	}
}

// IsLocal returns true if this context is configured for the local (initiating) end.
func (rc *ResolvedContext) IsLocal() bool {
	return rc.Context.Role == "local"
}

// KDCForwardSpec returns the -L forward spec for the ephemeral KDC tunnel.
func (rc *ResolvedContext) KDCForwardSpec() string {
	return fmt.Sprintf("0.0.0.0:%d:%s:%d",
		rc.Context.Auth.KDCLocalPort,
		rc.Context.Auth.KDC,
		rc.Context.Auth.KDCRemotePort,
	)
}

// ContextNames returns a sorted list of context names.
func (c *Config) ContextNames() []string {
	names := make([]string, 0, len(c.Contexts))
	for k := range c.Contexts {
		names = append(names, k)
	}
	return names
}
