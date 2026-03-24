package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

func makeTestContext(t *testing.T) (*config.ResolvedContext, string) {
	t.Helper()
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0755)

	return &config.ResolvedContext{
		Name: "test",
		Context: config.Context{
			Gate: config.GateConfig{Hostname: "gw.example.com", Port: 22},
			Auth: config.AuthConfig{User: "alice", Type: "kerberos", Realm: "EXAMPLE.COM"},
		},
		Derived: config.Derived{
			ContextName:   "test",
			AuthPrincipal: "alice@EXAMPLE.COM",
			ConfigDir:     dir,
		},
	}, dir
}

func TestRunRequired_Success(t *testing.T) {
	rc, dir := makeTestContext(t)
	hookPath := filepath.Join(dir, "hooks", "get-gate-token")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nprintf 'my-token'"), 0755)

	ctx := context.Background()
	out, err := RunRequired(ctx, rc, "get-gate-token")
	if err != nil {
		t.Fatalf("RunRequired: %v", err)
	}
	if out != "my-token" {
		t.Errorf("output = %q, want %q", out, "my-token")
	}
}

func TestRunRequired_MissingHook(t *testing.T) {
	rc, _ := makeTestContext(t)
	ctx := context.Background()
	_, err := RunRequired(ctx, rc, "nonexistent-hook")
	if err == nil {
		t.Fatal("expected error for missing required hook")
	}
}

func TestRunRequired_ScriptFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	hookPath := filepath.Join(dir, "hooks", "bad-hook")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1"), 0755)

	ctx := context.Background()
	_, err := RunRequired(ctx, rc, "bad-hook")
	if err == nil {
		t.Fatal("expected error from failing hook script")
	}
}

func TestRunRequired_TrimsTrailingNewlines(t *testing.T) {
	rc, dir := makeTestContext(t)
	hookPath := filepath.Join(dir, "hooks", "with-newlines")
	os.WriteFile(hookPath, []byte("#!/bin/sh\necho 'hello'"), 0755)

	ctx := context.Background()
	out, err := RunRequired(ctx, rc, "with-newlines")
	if err != nil {
		t.Fatalf("RunRequired: %v", err)
	}
	if out != "hello" {
		t.Errorf("output = %q, want %q (trailing newline should be trimmed)", out, "hello")
	}
}

func TestRunOptional_Found(t *testing.T) {
	rc, dir := makeTestContext(t)
	hookPath := filepath.Join(dir, "hooks", "on-connect")
	os.WriteFile(hookPath, []byte("#!/bin/sh\ntrue"), 0755)

	ctx := context.Background()
	skipped, err := RunOptional(ctx, rc, "on-connect")
	if err != nil {
		t.Fatalf("RunOptional: %v", err)
	}
	if skipped {
		t.Error("expected skipped=false when hook is found")
	}
}

func TestRunOptional_NotFound(t *testing.T) {
	rc, _ := makeTestContext(t)
	ctx := context.Background()
	skipped, err := RunOptional(ctx, rc, "nonexistent")
	if err != nil {
		t.Fatalf("RunOptional: %v", err)
	}
	if !skipped {
		t.Error("expected skipped=true when hook is not found")
	}
}

func TestRunOptional_ScriptFails(t *testing.T) {
	rc, dir := makeTestContext(t)
	hookPath := filepath.Join(dir, "hooks", "failing-hook")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1"), 0755)

	ctx := context.Background()
	_, err := RunOptional(ctx, rc, "failing-hook")
	if err == nil {
		t.Fatal("expected error from failing optional hook")
	}
}

func TestRunOptionalCheck_Success(t *testing.T) {
	rc, dir := makeTestContext(t)
	hookPath := filepath.Join(dir, "hooks", "check-credentials")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 0"), 0755)

	ctx := context.Background()
	ok := RunOptionalCheck(ctx, rc, "check-credentials")
	if !ok {
		t.Error("expected RunOptionalCheck to return true on exit 0")
	}
}

func TestRunOptionalCheck_Failure(t *testing.T) {
	rc, dir := makeTestContext(t)
	hookPath := filepath.Join(dir, "hooks", "check-credentials")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1"), 0755)

	ctx := context.Background()
	ok := RunOptionalCheck(ctx, rc, "check-credentials")
	if ok {
		t.Error("expected RunOptionalCheck to return false on exit 1")
	}
}

func TestRunOptionalCheck_NotFound(t *testing.T) {
	rc, _ := makeTestContext(t)
	ctx := context.Background()
	ok := RunOptionalCheck(ctx, rc, "nonexistent")
	if !ok {
		t.Error("expected RunOptionalCheck to return true when hook not found")
	}
}
