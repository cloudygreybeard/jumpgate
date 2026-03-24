package hooks

import (
	"fmt"
	"os"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

// BuildEnv constructs the environment variable slice passed to hook scripts.
func BuildEnv(rc *config.ResolvedContext) []string {
	p := rc.Context
	d := rc.Derived

	hookEnv := []string{
		"JUMPGATE_CONTEXT=" + d.ContextName,
		"JUMPGATE_CONFIG_DIR=" + d.ConfigDir,
		"GATE_HOSTNAME=" + p.Gate.Hostname,
		fmt.Sprintf("GATE_PORT=%d", p.Gate.Port),
		"AUTH_TYPE=" + p.Auth.Type,
		"AUTH_REALM=" + p.Auth.Realm,
		"AUTH_USER=" + p.Auth.User,
		"AUTH_PRINCIPAL=" + d.AuthPrincipal,
		"REMOTE_USER=" + p.Remote.User,
		"REMOTE_KEY=" + p.Remote.Key,
		fmt.Sprintf("RELAY_PORT=%d", p.Relay.RemotePort),
		"KEYCHAIN_TOTP_SERVICE=" + p.Keychain.TOTPService,
		"KEYCHAIN_TOTP_ACCOUNT=" + p.Keychain.TOTPAccount,
		"KEYCHAIN_KRB_SERVICE=" + p.Keychain.KRBService,
		"KEYCHAIN_KRB_ACCOUNT=" + p.Keychain.KRBAccount,
		"TOTP_CLI=" + p.TOTP.CLI,
		"TOTP_NAMESPACE=" + p.TOTP.Namespace,
		"TOTP_ACCOUNT=" + p.TOTP.Account,
		"WINDOWS_APP=" + p.WindowsApp,
	}

	// Merge with current process environment (hook env takes precedence)
	return mergeEnv(os.Environ(), hookEnv)
}

// mergeEnv merges override vars into base, with overrides winning.
func mergeEnv(base, overrides []string) []string {
	env := make(map[string]string, len(base)+len(overrides))
	for _, e := range base {
		k, v := splitEnv(e)
		env[k] = v
	}
	for _, e := range overrides {
		k, v := splitEnv(e)
		env[k] = v
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

func splitEnv(e string) (string, string) {
	for i := range e {
		if e[i] == '=' {
			return e[:i], e[i+1:]
		}
	}
	return e, ""
}
