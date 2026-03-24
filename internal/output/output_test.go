package output

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		want    Format
		wantErr bool
	}{
		{"text", Text, false},
		{"json", JSON, false},
		{"yaml", YAML, false},
		{"wide", Wide, false},
		{"", "", true},
		{"xml", "", true},
		{"JSON", "", true},
	}

	for _, tt := range tests {
		got, err := Parse(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) expected error, got %q", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsStructured(t *testing.T) {
	tests := []struct {
		f    Format
		want bool
	}{
		{JSON, true},
		{YAML, true},
		{Text, false},
		{Wide, false},
	}
	for _, tt := range tests {
		if got := IsStructured(tt.f); got != tt.want {
			t.Errorf("IsStructured(%q) = %v, want %v", tt.f, got, tt.want)
		}
	}
}

type testData struct {
	Name  string `json:"name" yaml:"name"`
	Count int    `json:"count" yaml:"count"`
}

func TestPrintJSON(t *testing.T) {
	data := testData{Name: "alpha", Count: 42}

	out := captureStdout(t, func() {
		if err := Print(JSON, data); err != nil {
			t.Fatalf("Print(JSON) error: %v", err)
		}
	})

	var decoded testData
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, out)
	}
	if decoded.Name != "alpha" || decoded.Count != 42 {
		t.Errorf("got %+v, want {Name:alpha Count:42}", decoded)
	}
}

func TestPrintYAML(t *testing.T) {
	data := testData{Name: "beta", Count: 7}

	out := captureStdout(t, func() {
		if err := Print(YAML, data); err != nil {
			t.Fatalf("Print(YAML) error: %v", err)
		}
	})

	var decoded testData
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("invalid YAML output: %v\n%s", err, out)
	}
	if decoded.Name != "beta" || decoded.Count != 7 {
		t.Errorf("got %+v, want {Name:beta Count:7}", decoded)
	}
}

func TestPrintTextReturnsError(t *testing.T) {
	err := Print(Text, "anything")
	if err == nil {
		t.Error("expected error calling Print with Text format")
	}
}

func TestPrintSlice(t *testing.T) {
	data := []testData{
		{Name: "one", Count: 1},
		{Name: "two", Count: 2},
	}

	out := captureStdout(t, func() {
		if err := Print(JSON, data); err != nil {
			t.Fatalf("Print(JSON, slice) error: %v", err)
		}
	})

	var decoded []testData
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("invalid JSON array output: %v\n%s", err, out)
	}
	if len(decoded) != 2 {
		t.Errorf("expected 2 items, got %d", len(decoded))
	}
}

func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	origStdout := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	r.Close()

	return buf.Bytes()
}
