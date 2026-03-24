package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func TestResolveGlobal(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0755)

	hookFile := filepath.Join(hooksDir, "get-gate-token")
	os.WriteFile(hookFile, []byte("#!/bin/sh\necho token"), 0755)

	path, err := Resolve(dir, "work", "get-gate-token")
	if err != nil {
		t.Fatal(err)
	}
	if path != hookFile {
		t.Errorf("got %q, want %q", path, hookFile)
	}
}

func TestResolveContextOverride(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "hooks"), 0755)
	os.MkdirAll(filepath.Join(dir, "contexts", "work", "hooks"), 0755)

	globalHook := filepath.Join(dir, "hooks", "get-gate-token")
	os.WriteFile(globalHook, []byte("#!/bin/sh\necho global"), 0755)

	contextHook := filepath.Join(dir, "contexts", "work", "hooks", "get-gate-token")
	os.WriteFile(contextHook, []byte("#!/bin/sh\necho context"), 0755)

	path, err := Resolve(dir, "work", "get-gate-token")
	if err != nil {
		t.Fatal(err)
	}
	if path != contextHook {
		t.Errorf("got %q, want context hook %q", path, contextHook)
	}
}

func TestResolveNotFound(t *testing.T) {
	dir := t.TempDir()
	path, err := Resolve(dir, "work", "nonexistent-hook")
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestResolveRequiredMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveRequired(dir, "work", "get-gate-token")
	if err == nil {
		t.Fatal("expected error for missing required hook")
	}
}

func TestSplitEnv_NoEquals(t *testing.T) {
	k, v := splitEnv("NOEQUALS")
	if k != "NOEQUALS" || v != "" {
		t.Errorf("splitEnv(NOEQUALS) = (%q, %q), want (NOEQUALS, \"\")", k, v)
	}
}

func TestMergeEnv_Override(t *testing.T) {
	base := []string{"A=1", "B=2"}
	overrides := []string{"B=3", "C=4"}
	merged := mergeEnv(base, overrides)

	m := make(map[string]string)
	for _, e := range merged {
		k, v := splitEnv(e)
		m[k] = v
	}
	if m["A"] != "1" {
		t.Errorf("A = %q, want 1", m["A"])
	}
	if m["B"] != "3" {
		t.Errorf("B = %q, want 3 (should be overridden)", m["B"])
	}
	if m["C"] != "4" {
		t.Errorf("C = %q, want 4", m["C"])
	}
}

func TestBuildEnv(t *testing.T) {
	rc := &config.ResolvedContext{
		Name: "work",
		Context: config.Context{
			Gate: config.GateConfig{
				Hostname: "gw.example.com",
				Port:     22,
			},
			Auth: config.AuthConfig{
				Type:  "kerberos",
				Realm: "EXAMPLE.COM",
				User:  "alice",
			},
			Remote: config.RemoteConfig{
				User: "alice-remote",
				Key:  "~/.ssh/id_ed25519",
			},
			Relay: config.RelayConfig{
				RemotePort: 55555,
			},
		},
		Derived: config.Derived{
			ContextName:   "work",
			AuthPrincipal: "alice@EXAMPLE.COM",
			ConfigDir:     "/tmp/test-config",
		},
	}

	env := BuildEnv(rc)

	want := map[string]string{
		"JUMPGATE_CONTEXT":    "work",
		"JUMPGATE_CONFIG_DIR": "/tmp/test-config",
		"GATE_HOSTNAME":       "gw.example.com",
		"AUTH_TYPE":           "kerberos",
		"AUTH_REALM":          "EXAMPLE.COM",
		"AUTH_USER":           "alice",
		"AUTH_PRINCIPAL":      "alice@EXAMPLE.COM",
		"REMOTE_USER":         "alice-remote",
		"REMOTE_KEY":          "~/.ssh/id_ed25519",
		"RELAY_PORT":          "55555",
	}

	envMap := make(map[string]string)
	for _, e := range env {
		k, v := splitEnv(e)
		envMap[k] = v
	}

	for k, v := range want {
		if envMap[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, envMap[k], v)
		}
	}
}
