package setup

import (
	"embed"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/config"
)

// SetupConfig creates CONFIG_DIR, copies config template, and installs hooks.
func SetupConfig(rc *config.ResolvedContext, configTemplate []byte, hooksFS embed.FS) error {
	configDir := rc.Derived.ConfigDir
	fmt.Printf("=== Config directory (%s) ===\n", configDir)

	for _, sub := range []string{"ssh", "hooks"} {
		if err := os.MkdirAll(filepath.Join(configDir, sub), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		port := rand.Intn(16384) + 49152
		content := strings.ReplaceAll(string(configTemplate), "remote_port: 0",
			fmt.Sprintf("remote_port: %d", port))
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Printf("  Created %s (relay port: %d)\n", configPath, port)
		fmt.Printf("  Edit it with your values: $EDITOR %s\n", configPath)
	} else {
		fmt.Println("  config.yaml already exists")
	}

	return nil
}

// SetupConfigSimple creates CONFIG_DIR and config without an embedded FS.
// Used as a simpler fallback.
func SetupConfigSimple(configDir string, configTemplate []byte) error {
	fmt.Printf("=== Config directory (%s) ===\n", configDir)

	for _, sub := range []string{"ssh", "hooks"} {
		if err := os.MkdirAll(filepath.Join(configDir, sub), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		port := rand.Intn(16384) + 49152
		content := strings.ReplaceAll(string(configTemplate), "remote_port: 0",
			fmt.Sprintf("remote_port: %d", port))
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Printf("  Created %s (relay port: %d)\n", configPath, port)
		fmt.Printf("  Edit it with your values: $EDITOR %s\n", configPath)
	} else {
		fmt.Println("  config.yaml already exists")
	}

	return nil
}
