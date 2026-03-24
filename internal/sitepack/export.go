package sitepack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"gopkg.in/yaml.v3"
)

// FieldMapping maps a Context struct path to a flat site-pack key.
type FieldMapping struct {
	Key     string
	Prompt  string
	Default string
	Extract func(ctx *config.Context) string
	Inject  func(ctx *config.Context, val string)
}

var fieldMappings = []FieldMapping{
	{Key: "context", Prompt: "Context name", Default: "work",
		Extract: nil, Inject: nil}, // handled specially -- comes from the context name, not the struct
	{Key: "role", Prompt: "Role (local or remote)", Default: "local",
		Extract: func(c *config.Context) string { return c.Role },
		Inject:  func(c *config.Context, v string) { c.Role = v }},
	{Key: "gate_host", Prompt: "Gate (bastion) hostname",
		Extract: func(c *config.Context) string { return c.Gate.Hostname },
		Inject:  func(c *config.Context, v string) { c.Gate.Hostname = v }},
	{Key: "gate_port", Prompt: "Gate SSH port", Default: "22",
		Extract: func(c *config.Context) string { return fmt.Sprintf("%d", c.Gate.Port) },
		Inject:  nil},
	{Key: "auth_type", Prompt: "Auth type (kerberos, key, none)", Default: "key",
		Extract: func(c *config.Context) string { return c.Auth.Type },
		Inject:  func(c *config.Context, v string) { c.Auth.Type = v }},
	{Key: "auth_user", Prompt: "Auth username",
		Extract: func(c *config.Context) string { return c.Auth.User },
		Inject:  func(c *config.Context, v string) { c.Auth.User = v }},
	{Key: "auth_realm", Prompt: "Kerberos realm",
		Extract: func(c *config.Context) string { return c.Auth.Realm },
		Inject:  func(c *config.Context, v string) { c.Auth.Realm = v }},
	{Key: "auth_kdc", Prompt: "KDC hostname (reachable from gate)",
		Extract: func(c *config.Context) string { return c.Auth.KDC },
		Inject:  func(c *config.Context, v string) { c.Auth.KDC = v }},
	{Key: "kdc_local_port", Prompt: "KDC local forward port", Default: "8888",
		Extract: func(c *config.Context) string { return fmt.Sprintf("%d", c.Auth.KDCLocalPort) },
		Inject:  nil},
	{Key: "kdc_remote_port", Prompt: "KDC service port", Default: "88",
		Extract: func(c *config.Context) string { return fmt.Sprintf("%d", c.Auth.KDCRemotePort) },
		Inject:  nil},
	{Key: "kinit_path", Prompt: "Path to kinit binary", Default: "kinit",
		Extract: func(c *config.Context) string { return c.Auth.Kinit },
		Inject:  func(c *config.Context, v string) { c.Auth.Kinit = v }},
	{Key: "cc_file", Prompt: "Kerberos credential cache file",
		Extract: func(c *config.Context) string { return c.Auth.CCFile },
		Inject:  func(c *config.Context, v string) { c.Auth.CCFile = v }},
	{Key: "remote_user", Prompt: "Remote SSH username",
		Extract: func(c *config.Context) string { return c.Remote.User },
		Inject:  func(c *config.Context, v string) { c.Remote.User = v }},
	{Key: "remote_key", Prompt: "SSH private key path", Default: "~/.ssh/id_ed25519",
		Extract: func(c *config.Context) string { return c.Remote.Key },
		Inject:  func(c *config.Context, v string) { c.Remote.Key = v }},
	{Key: "remote_dir", Prompt: "Remote jumpgate directory", Default: "~/jumpgate",
		Extract: func(c *config.Context) string { return c.Remote.RemoteDir },
		Inject:  func(c *config.Context, v string) { c.Remote.RemoteDir = v }},
	{Key: "relay_port", Prompt: "Relay port (gate → remote)", Default: "auto",
		Extract: func(c *config.Context) string { return fmt.Sprintf("%d", c.Relay.RemotePort) },
		Inject:  nil},
	{Key: "keychain_totp_service", Prompt: "Keychain service for TOTP secret",
		Extract: func(c *config.Context) string { return c.Keychain.TOTPService },
		Inject:  func(c *config.Context, v string) { c.Keychain.TOTPService = v }},
	{Key: "keychain_totp_account", Prompt: "Keychain account for TOTP",
		Extract: func(c *config.Context) string { return c.Keychain.TOTPAccount },
		Inject:  func(c *config.Context, v string) { c.Keychain.TOTPAccount = v }},
	{Key: "keychain_krb_service", Prompt: "Keychain service for Kerberos password",
		Extract: func(c *config.Context) string { return c.Keychain.KRBService },
		Inject:  func(c *config.Context, v string) { c.Keychain.KRBService = v }},
	{Key: "keychain_krb_account", Prompt: "Keychain account for Kerberos password",
		Extract: func(c *config.Context) string { return c.Keychain.KRBAccount },
		Inject:  func(c *config.Context, v string) { c.Keychain.KRBAccount = v }},
	{Key: "totp_cli", Prompt: "Path to totp-cli binary",
		Extract: func(c *config.Context) string { return c.TOTP.CLI },
		Inject:  func(c *config.Context, v string) { c.TOTP.CLI = v }},
	{Key: "totp_namespace", Prompt: "totp-cli namespace",
		Extract: func(c *config.Context) string { return c.TOTP.Namespace },
		Inject:  func(c *config.Context, v string) { c.TOTP.Namespace = v }},
	{Key: "totp_account", Prompt: "totp-cli account",
		Extract: func(c *config.Context) string { return c.TOTP.Account },
		Inject:  func(c *config.Context, v string) { c.TOTP.Account = v }},
	{Key: "container_runtime", Prompt: "Container runtime", Default: "podman",
		Extract: func(c *config.Context) string { return c.Container.Runtime },
		Inject:  func(c *config.Context, v string) { c.Container.Runtime = v }},
	{Key: "container_image", Prompt: "Container image", Default: "jumpgate-kinit",
		Extract: func(c *config.Context) string { return c.Container.Image },
		Inject:  func(c *config.Context, v string) { c.Container.Image = v }},
	{Key: "windows_app", Prompt: "Windows app name", Default: "Windows App",
		Extract: func(c *config.Context) string { return c.WindowsApp },
		Inject:  func(c *config.Context, v string) { c.WindowsApp = v }},
}

// ExtractValues pulls flat key-value pairs from a resolved context.
func ExtractValues(contextName string, ctx *config.Context) map[string]string {
	vals := make(map[string]string)
	for _, m := range fieldMappings {
		if m.Key == "context" {
			vals["context"] = contextName
			continue
		}
		if m.Extract != nil {
			v := m.Extract(ctx)
			if v != "" && v != "0" {
				vals[m.Key] = v
			}
		}
	}
	return vals
}

// BuildSchema builds a ValueDef schema from the field mappings, using
// the extracted values to set defaults where the value matches a known default.
func BuildSchema(vals map[string]string) []ValueDef {
	var schema []ValueDef
	for _, m := range fieldMappings {
		def := ValueDef{
			Key:     m.Key,
			Prompt:  m.Prompt,
			Default: m.Default,
		}
		schema = append(schema, def)
	}
	return schema
}

// Export generates a site pack directory from a resolved context.
func Export(contextName string, ctx *config.Context, configDir, outDir string) error {
	vals := ExtractValues(contextName, ctx)
	schema := BuildSchema(vals)

	if err := os.MkdirAll(filepath.Join(outDir, "templates"), 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	// site.yaml
	pack := Pack{
		Name:        contextName + "-site-pack",
		Description: fmt.Sprintf("Site pack exported from context %q", contextName),
		Values:      schema,
	}
	siteData, err := yaml.Marshal(pack)
	if err != nil {
		return fmt.Errorf("marshalling site.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "site.yaml"), siteData, 0644); err != nil {
		return fmt.Errorf("writing site.yaml: %w", err)
	}
	fmt.Printf("  Wrote site.yaml\n")

	// values.yaml
	valsData, err := yaml.Marshal(vals)
	if err != nil {
		return fmt.Errorf("marshalling values.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "values.yaml"), valsData, 0644); err != nil {
		return fmt.Errorf("writing values.yaml: %w", err)
	}
	fmt.Printf("  Wrote values.yaml\n")

	// values.example.yaml -- same keys with placeholder values
	example := make(map[string]string)
	for _, m := range fieldMappings {
		if m.Default != "" {
			example[m.Key] = m.Default
		} else {
			example[m.Key] = ""
		}
	}
	exData, err := yaml.Marshal(example)
	if err != nil {
		return fmt.Errorf("marshalling values.example.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "values.example.yaml"), exData, 0644); err != nil {
		return fmt.Errorf("writing values.example.yaml: %w", err)
	}
	fmt.Printf("  Wrote values.example.yaml\n")

	// templates/config.yaml.tpl
	if err := writeConfigTemplate(filepath.Join(outDir, "templates", "config.yaml.tpl")); err != nil {
		return err
	}
	fmt.Printf("  Wrote templates/config.yaml.tpl\n")

	// Copy hooks from config dir
	hooksDir := filepath.Join(configDir, "hooks")
	if err := copyDir(hooksDir, filepath.Join(outDir, "hooks"), true); err != nil {
		return err
	}

	// Copy user SSH snippets from config dir
	snippetsDir := filepath.Join(configDir, "ssh", "snippets")
	if err := copyDir(snippetsDir, filepath.Join(outDir, "snippets"), false); err != nil {
		return err
	}

	// Copy windows scripts if present
	windowsDir := filepath.Join(configDir, "windows")
	if info, err := os.Stat(windowsDir); err == nil && info.IsDir() {
		if err := copyDir(windowsDir, filepath.Join(outDir, "windows"), false); err != nil {
			return err
		}
	}

	// .gitignore
	gitignore := "values.yaml\n"
	if err := os.WriteFile(filepath.Join(outDir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	return nil
}

const configTemplateTpl = `# Jumpgate access configuration
# Generated by: jumpgate config export
#
# Template variables are drawn from values.yaml.
# Render with: jumpgate init --from <this-directory>

default_context: {{"{{"}}.context{{"}}"}}

contexts:
  {{"{{"}}.context{{"}}"}}:
    role: {{"{{"}}.role{{"}}"}}

    gate:
      hostname: {{"{{"}}.gate_host{{"}}"}}
      port: {{"{{"}}.gate_port{{"}}"}}

    auth:
      type: {{"{{"}}.auth_type{{"}}"}}
      realm: {{"{{"}}.auth_realm{{"}}"}}
      user: {{"{{"}}.auth_user{{"}}"}}
      kdc: {{"{{"}}.auth_kdc{{"}}"}}
      kdc_local_port: {{"{{"}}.kdc_local_port{{"}}"}}
      kdc_remote_port: {{"{{"}}.kdc_remote_port{{"}}"}}
      kinit: {{"{{"}}.kinit_path{{"}}"}}
      cc_file: {{"{{"}}.cc_file{{"}}"}}

    remote:
      user: {{"{{"}}.remote_user{{"}}"}}
      key: {{"{{"}}.remote_key{{"}}"}}
      remote_dir: {{"{{"}}.remote_dir{{"}}"}}

    relay:
      remote_port: {{"{{"}}.relay_port{{"}}"}}

    keychain:
      totp_service: "{{"{{"}}.keychain_totp_service{{"}}"}}"
      totp_account: {{"{{"}}.keychain_totp_account{{"}}"}}
      krb_service: {{"{{"}}.keychain_krb_service{{"}}"}}
      krb_account: {{"{{"}}.keychain_krb_account{{"}}"}}

    totp:
      cli: {{"{{"}}.totp_cli{{"}}"}}
      namespace: {{"{{"}}.totp_namespace{{"}}"}}
      account: {{"{{"}}.totp_account{{"}}"}}

    container:
      runtime: {{"{{"}}.container_runtime{{"}}"}}
      image: {{"{{"}}.container_image{{"}}"}}

    windows_app: "{{"{{"}}.windows_app{{"}}"}}"
`

func writeConfigTemplate(path string) error {
	// The template literal above uses Go's template escaping to produce
	// actual {{.key}} placeholders in the output file.
	tmpl, err := template.New("config.yaml.tpl").Parse(configTemplateTpl)
	if err != nil {
		return fmt.Errorf("parsing config template: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	return tmpl.Execute(f, nil)
}

// FieldMappings returns the field mapping table for external use (e.g. tests).
func FieldMappings() []FieldMapping {
	return fieldMappings
}

// isZeroValue checks if a string is a zero value for omission purposes.
func isZeroValue(s string) bool {
	return s == "" || s == "0"
}

// FilterNonEmpty returns vals with zero-valued entries removed.
func FilterNonEmpty(vals map[string]string) map[string]string {
	filtered := make(map[string]string)
	for k, v := range vals {
		if !isZeroValue(v) {
			filtered[k] = v
		}
	}
	return filtered
}

// ApplyValues builds a config.Context from flat key-value pairs using the field mappings.
func ApplyValues(vals map[string]string) config.Context {
	var ctx config.Context
	for _, m := range fieldMappings {
		if m.Inject == nil {
			continue
		}
		if v, ok := vals[m.Key]; ok && v != "" {
			m.Inject(&ctx, v)
		}
	}
	// Handle int fields via strconv manually
	if v, ok := vals["gate_port"]; ok && v != "" {
		ctx.Gate.Port = parseInt(v)
	}
	if v, ok := vals["kdc_local_port"]; ok && v != "" {
		ctx.Auth.KDCLocalPort = parseInt(v)
	}
	if v, ok := vals["kdc_remote_port"]; ok && v != "" {
		ctx.Auth.KDCRemotePort = parseInt(v)
	}
	if v, ok := vals["relay_port"]; ok && v != "" {
		ctx.Relay.RemotePort = parseInt(v)
	}
	return ctx
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n
}
