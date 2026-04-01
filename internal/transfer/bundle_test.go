package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	files := map[string]string{
		"config.yaml": "default_context: test\n",
		"hooks/pre":   "#!/bin/sh\necho pre\n",
		"hooks/post":  "#!/bin/sh\necho post\n",
		"ssh/snippet": "Host test\n  Port 22\n",
	}
	for name, content := range files {
		p := filepath.Join(srcDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var entries []FileEntry
	for name := range files {
		entries = append(entries, FileEntry{
			LocalPath:  filepath.Join(srcDir, name),
			RemotePath: ".config/jumpgate/" + name,
			Mode:       0644,
		})
	}

	var buf bytes.Buffer
	if err := CreateBundle(&buf, entries); err != nil {
		t.Fatalf("CreateBundle: %v", err)
	}

	t.Logf("bundle size: %d bytes for %d files", buf.Len(), len(entries))

	count, err := ExtractBundle(&buf, dstDir)
	if err != nil {
		t.Fatalf("ExtractBundle: %v", err)
	}
	if count != len(files) {
		t.Errorf("extracted %d files, want %d", count, len(files))
	}

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dstDir, ".config/jumpgate", name))
		if err != nil {
			t.Errorf("missing extracted file %s: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("file %s: got %q, want %q", name, got, want)
		}
	}
}

func TestBundleFromBytes(t *testing.T) {
	dstDir := t.TempDir()

	entries := map[string][]byte{
		".config/jumpgate/config.yaml": []byte("test: true\n"),
	}

	var buf bytes.Buffer
	if err := CreateBundleFromBytes(&buf, entries, 0644); err != nil {
		t.Fatalf("CreateBundleFromBytes: %v", err)
	}

	count, err := ExtractBundle(&buf, dstDir)
	if err != nil {
		t.Fatalf("ExtractBundle: %v", err)
	}
	if count != 1 {
		t.Errorf("extracted %d files, want 1", count)
	}

	got, err := os.ReadFile(filepath.Join(dstDir, ".config/jumpgate/config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "test: true\n" {
		t.Errorf("got %q", got)
	}
}

func TestExtractBlocksTraversal(t *testing.T) {
	var buf bytes.Buffer

	entries := map[string][]byte{
		"../../etc/passwd": []byte("root:x:0:0\n"),
	}
	if err := CreateBundleFromBytes(&buf, entries, 0644); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	_, err := ExtractBundle(&buf, dstDir)
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}
