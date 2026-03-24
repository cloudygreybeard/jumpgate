package output

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Format represents a supported output format.
type Format string

const (
	Text Format = "text"
	JSON Format = "json"
	YAML Format = "yaml"
	Wide Format = "wide"
)

// Parse validates and returns a Format from a string.
func Parse(s string) (Format, error) {
	switch Format(s) {
	case Text, JSON, YAML, Wide:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unsupported output format %q (use text, json, yaml, or wide)", s)
	}
}

// Print marshals data to the given format and writes to stdout.
// For Text and Wide, callers should handle formatting themselves
// and use IsText/IsStructured to branch.
func Print(format Format, data any) error {
	switch format {
	case JSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case YAML:
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(data); err != nil {
			return err
		}
		return enc.Close()
	default:
		return fmt.Errorf("Print called with non-structured format %q; use IsStructured() to check first", format)
	}
}

// IsStructured returns true if the format expects machine-parseable output.
func IsStructured(f Format) bool {
	return f == JSON || f == YAML
}
