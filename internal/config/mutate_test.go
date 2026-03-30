package config

import (
	"os"
	"path/filepath"
	"testing"
)

const mutateTestConfig = `default_context: alpha
contexts:
  alpha:
    gate:
      hostname: gw-alpha.example.com
      port: 22
    auth:
      type: key
  beta:
    gate:
      hostname: gw-beta.example.com
      port: 330
    auth:
      type: kerberos
`

func writeMutateTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(mutateTestConfig), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRaw(t *testing.T) {
	path := writeMutateTestConfig(t)
	cfg, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if cfg.DefaultContext != "alpha" {
		t.Errorf("DefaultContext = %q, want alpha", cfg.DefaultContext)
	}
	if len(cfg.Contexts) != 2 {
		t.Errorf("len(Contexts) = %d, want 2", len(cfg.Contexts))
	}
	if doc == nil {
		t.Error("doc is nil")
	}
}

func TestSetDefaultContext(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := SetDefaultContext(doc, "beta"); err != nil {
		t.Fatalf("SetDefaultContext: %v", err)
	}
	if err := SaveRaw(path, doc); err != nil {
		t.Fatalf("SaveRaw: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultContext != "beta" {
		t.Errorf("DefaultContext = %q, want beta", cfg.DefaultContext)
	}
}

func TestAddContext(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := Context{
		Gate: GateConfig{Hostname: "gw-gamma.example.com", Port: 22},
		Auth: AuthConfig{Type: "key"},
	}
	if err := AddContext(doc, "gamma", ctx); err != nil {
		t.Fatalf("AddContext: %v", err)
	}
	if err := SaveRaw(path, doc); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Contexts) != 3 {
		t.Errorf("len(Contexts) = %d, want 3", len(cfg.Contexts))
	}
	if cfg.Contexts["gamma"].Gate.Hostname != "gw-gamma.example.com" {
		t.Error("gamma hostname mismatch")
	}
}

func TestAddContext_Duplicate(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	err = AddContext(doc, "alpha", Context{})
	if err == nil {
		t.Error("expected error for duplicate context")
	}
}

func TestDeleteContext(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := DeleteContext(doc, "beta"); err != nil {
		t.Fatalf("DeleteContext: %v", err)
	}
	if err := SaveRaw(path, doc); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Contexts) != 1 {
		t.Errorf("len(Contexts) = %d, want 1", len(cfg.Contexts))
	}
	if _, ok := cfg.Contexts["beta"]; ok {
		t.Error("beta should have been deleted")
	}
}

func TestDeleteContext_NotFound(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	err = DeleteContext(doc, "nonexistent")
	if err == nil {
		t.Error("expected error for missing context")
	}
}

func TestRenameContext(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := RenameContext(doc, "alpha", "primary"); err != nil {
		t.Fatalf("RenameContext: %v", err)
	}
	if err := SaveRaw(path, doc); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cfg.Contexts["alpha"]; ok {
		t.Error("old name 'alpha' should not exist")
	}
	if _, ok := cfg.Contexts["primary"]; !ok {
		t.Error("new name 'primary' not found")
	}
	if cfg.DefaultContext != "primary" {
		t.Errorf("DefaultContext = %q, want primary (should follow rename)", cfg.DefaultContext)
	}
}

func TestRenameContext_PreservesUID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := `default_context: alpha
contexts:
  alpha:
    uid: test-uid-preserved
    gate:
      hostname: gw.example.com
`
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameContext(doc, "alpha", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := SaveRaw(path, doc); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if c.Contexts["renamed"].UID != "test-uid-preserved" {
		t.Errorf("UID = %q, want test-uid-preserved (rename must preserve UID)", c.Contexts["renamed"].UID)
	}
}

func TestRenameContext_DuplicateTarget(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	err = RenameContext(doc, "alpha", "beta")
	if err == nil {
		t.Error("expected error for duplicate target name")
	}
}

func TestRenameContext_NotFound(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	err = RenameContext(doc, "nonexistent", "new")
	if err == nil {
		t.Error("expected error for missing source context")
	}
}

func TestSetContext_Create(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := Context{Gate: GateConfig{Hostname: "new.example.com", Port: 443}}
	if err := SetContext(doc, "newctx", ctx); err != nil {
		t.Fatalf("SetContext (create): %v", err)
	}

	if err := SaveRaw(path, doc); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := cfg.Contexts["newctx"]
	if !ok {
		t.Fatal("newctx not found after SetContext")
	}
	if c.Gate.Hostname != "new.example.com" {
		t.Errorf("hostname = %q, want new.example.com", c.Gate.Hostname)
	}
}

func TestSetContext_Overwrite(t *testing.T) {
	path := writeMutateTestConfig(t)
	_, doc, err := LoadRaw(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := Context{Gate: GateConfig{Hostname: "updated.example.com", Port: 2222}}
	if err := SetContext(doc, "work", ctx); err != nil {
		t.Fatalf("SetContext (overwrite): %v", err)
	}

	if err := SaveRaw(path, doc); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Contexts["work"]
	if c.Gate.Hostname != "updated.example.com" {
		t.Errorf("hostname = %q, want updated.example.com", c.Gate.Hostname)
	}
	if c.Gate.Port != 2222 {
		t.Errorf("port = %d, want 2222", c.Gate.Port)
	}
}

func TestContextNames(t *testing.T) {
	cfg := &Config{
		Contexts: map[string]Context{
			"beta":  {},
			"alpha": {},
			"gamma": {},
		},
	}
	names := cfg.ContextNames()
	if len(names) != 3 {
		t.Fatalf("len(ContextNames) = %d, want 3", len(names))
	}
}
