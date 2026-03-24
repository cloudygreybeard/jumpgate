package sitepack

import (
	"bufio"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Pack represents a site pack's metadata and value schema.
type Pack struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Platform    string     `yaml:"platform"`
	Values      []ValueDef `yaml:"values"`
	Dir         string     `yaml:"-"`
}

// ValueDef defines a single value that the pack expects.
type ValueDef struct {
	Key     string `yaml:"key"`
	Prompt  string `yaml:"prompt"`
	Default string `yaml:"default"`
}

// LoadPack reads site.yaml from the given directory.
func LoadPack(dir string) (*Pack, error) {
	data, err := os.ReadFile(filepath.Join(dir, "site.yaml"))
	if err != nil {
		return nil, fmt.Errorf("reading site.yaml: %w", err)
	}
	var p Pack
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing site.yaml: %w", err)
	}
	p.Dir = dir
	return &p, nil
}

// LoadValues reads values.yaml from the pack directory. Missing keys are
// filled from the schema defaults. Returns an error only on parse failure;
// a missing file is not an error (all values will be empty or defaulted).
func LoadValues(dir string, schema []ValueDef) (map[string]string, error) {
	vals := make(map[string]string)

	data, err := os.ReadFile(filepath.Join(dir, "values.yaml"))
	if err == nil {
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing values.yaml: %w", err)
		}
		for k, v := range raw {
			vals[k] = fmt.Sprintf("%v", v)
		}
	}

	for _, def := range schema {
		if _, ok := vals[def.Key]; !ok && def.Default != "" {
			resolved := def.Default
			if resolved == "auto" && def.Key == "relay_port" {
				resolved = fmt.Sprintf("%d", rand.Intn(16384)+49152)
			}
			vals[def.Key] = resolved
		}
	}

	return vals, nil
}

// PromptMissing interactively asks the user for any values not yet provided.
func PromptMissing(vals map[string]string, schema []ValueDef, reader *bufio.Reader) error {
	for _, def := range schema {
		if v, ok := vals[def.Key]; ok && v != "" {
			continue
		}
		prompt := def.Prompt
		if prompt == "" {
			prompt = def.Key
		}
		if def.Default != "" {
			if def.Default == "auto" && def.Key == "relay_port" {
				vals[def.Key] = fmt.Sprintf("%d", rand.Intn(16384)+49152)
				continue
			}
			prompt += fmt.Sprintf(" [%s]", def.Default)
		}
		fmt.Printf("  %s: ", prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading input for %s: %w", def.Key, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			line = def.Default
		}
		vals[def.Key] = line
	}
	return nil
}

// Render processes the site pack: renders templates and copies hooks, snippets,
// and windows scripts into the target config directory.
func Render(pack *Pack, vals map[string]string, configDir string) error {
	for _, sub := range []string{"hooks", "ssh/snippets"} {
		if err := os.MkdirAll(filepath.Join(configDir, sub), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	if err := renderTemplates(pack.Dir, vals, configDir); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(pack.Dir, "hooks"), filepath.Join(configDir, "hooks"), true); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(pack.Dir, "snippets"), filepath.Join(configDir, "ssh", "snippets"), false); err != nil {
		return err
	}
	// windows/ scripts go alongside the config for potential remote deploy
	winSrc := filepath.Join(pack.Dir, "windows")
	if info, err := os.Stat(winSrc); err == nil && info.IsDir() {
		if err := copyDir(winSrc, filepath.Join(configDir, "windows"), false); err != nil {
			return err
		}
	}
	return nil
}

func renderTemplates(packDir string, vals map[string]string, configDir string) error {
	tplDir := filepath.Join(packDir, "templates")
	entries, err := os.ReadDir(tplDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading templates/: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tpl") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(tplDir, e.Name()))
		if err != nil {
			return fmt.Errorf("reading template %s: %w", e.Name(), err)
		}

		tmpl, err := template.New(e.Name()).Option("missingkey=zero").Parse(string(data))
		if err != nil {
			return fmt.Errorf("parsing template %s: %w", e.Name(), err)
		}

		outName := strings.TrimSuffix(e.Name(), ".tpl")
		outPath := filepath.Join(configDir, outName)

		f, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("creating %s: %w", outPath, err)
		}

		if err := tmpl.Execute(f, vals); err != nil {
			f.Close()
			return fmt.Errorf("rendering %s: %w", e.Name(), err)
		}
		f.Close()

		fmt.Printf("  Rendered %s\n", outPath)
	}
	return nil
}

func copyDir(src, dst string, executable bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", src, err)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	copied := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		perm := fs.FileMode(0644)
		if executable {
			perm = 0755
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, perm); err != nil {
			return fmt.Errorf("writing %s: %w", e.Name(), err)
		}
		copied++
	}
	if copied > 0 {
		what := filepath.Base(src)
		fmt.Printf("  Installed %s (%d files)\n", what, copied)
	}
	return nil
}
