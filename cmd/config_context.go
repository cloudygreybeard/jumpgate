package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"text/tabwriter"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/output"
	"github.com/spf13/cobra"
)

// ContextSummary is the structured representation of a context for list output.
type ContextSummary struct {
	Name      string `json:"name" yaml:"name"`
	Default   bool   `json:"default" yaml:"default"`
	Role      string `json:"role" yaml:"role"`
	GateHost  string `json:"gate_host" yaml:"gate_host"`
	GatePort  int    `json:"gate_port,omitempty" yaml:"gate_port,omitempty"`
	RelayPort int    `json:"relay_port,omitempty" yaml:"relay_port,omitempty"`
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all contexts",
	Long: `List all configured contexts. The default context is marked with *.

Supports -o json and -o yaml for structured output, and -o wide for an
extended table showing role, gate host, gate port, and relay port.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		names := cfg.ContextNames()
		sort.Strings(names)

		of, err := outputFormat()
		if err != nil {
			return err
		}

		summaries := make([]ContextSummary, 0, len(names))
		for _, name := range names {
			ctx := cfg.Contexts[name]
			summaries = append(summaries, ContextSummary{
				Name:      name,
				Default:   name == cfg.DefaultContext,
				Role:      ctx.Role,
				GateHost:  ctx.Gate.Hostname,
				GatePort:  ctx.Gate.Port,
				RelayPort: ctx.Relay.RemotePort,
			})
		}

		if output.IsStructured(of) {
			return output.Print(of, summaries)
		}

		if of == output.Wide {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  NAME\tROLE\tGATE HOST\tGATE PORT\tRELAY PORT")
			for _, s := range summaries {
				marker := " "
				if s.Default {
					marker = "*"
				}
				gate := s.GateHost
				if gate == "" {
					gate = "(not set)"
				}
				fmt.Fprintf(w, "%s %s\t%s\t%s\t%d\t%d\n",
					marker, s.Name, s.Role, gate, s.GatePort, s.RelayPort)
			}
			return w.Flush()
		}

		for _, s := range summaries {
			marker := "  "
			if s.Default {
				marker = "* "
			}
			gate := s.GateHost
			if gate == "" {
				gate = "(not set)"
			}
			fmt.Printf("%s%-16s  %s\n", marker, s.Name, gate)
		}
		return nil
	},
}

var configCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current default context",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		fmt.Println(cfg.DefaultContext)
		return nil
	},
}

var configUseCmd = &cobra.Command{
	Use:   "use <CONTEXT>",
	Short: "Set the default context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfgPath := configFilePath()

		cfg, doc, err := config.LoadRaw(cfgPath)
		if err != nil {
			return err
		}

		if _, ok := cfg.Contexts[name]; !ok {
			return fmt.Errorf("context %q not found (available: %s)", name, contextList(cfg))
		}

		if err := config.SetDefaultContext(doc, name); err != nil {
			return err
		}
		if err := config.SaveRaw(cfgPath, doc); err != nil {
			return err
		}

		fmt.Printf("Switched to context %q\n", name)
		return autoRegenSSH()
	},
}

var configCreateFrom string

var configCreateCmd = &cobra.Command{
	Use:   "create <CONTEXT>",
	Short: "Create a new context",
	Long: `Create a new context. Use --from to clone an existing context,
or omit it to create a minimal scaffold.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfgPath := configFilePath()

		cfg, doc, err := config.LoadRaw(cfgPath)
		if err != nil {
			return err
		}

		var ctx config.Context
		if configCreateFrom != "" {
			src, ok := cfg.Contexts[configCreateFrom]
			if !ok {
				return fmt.Errorf("source context %q not found", configCreateFrom)
			}
			ctx = src
		} else {
			ctx = config.Context{
				Gate: config.GateConfig{Port: 22},
				Auth: config.AuthConfig{Type: "key"},
			}
		}
		ctx.UID = config.GenerateUID()

		if err := config.AddContext(doc, name, ctx); err != nil {
			return err
		}
		if err := config.SaveRaw(cfgPath, doc); err != nil {
			return err
		}

		if configCreateFrom != "" {
			fmt.Printf("Created context %q (cloned from %q)\n", name, configCreateFrom)
		} else {
			fmt.Printf("Created context %q\n", name)
		}
		fmt.Printf("Edit with: jumpgate config edit\n")
		return nil
	},
}

var configDeleteCmd = &cobra.Command{
	Use:   "delete <CONTEXT>",
	Short: "Delete a context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfgPath := configFilePath()

		cfg, doc, err := config.LoadRaw(cfgPath)
		if err != nil {
			return err
		}

		if _, ok := cfg.Contexts[name]; !ok {
			return fmt.Errorf("context %q not found", name)
		}

		if name == cfg.DefaultContext {
			return fmt.Errorf("cannot delete the default context %q (use 'jumpgate config use' to switch first)", name)
		}

		if err := config.DeleteContext(doc, name); err != nil {
			return err
		}
		if err := config.SaveRaw(cfgPath, doc); err != nil {
			return err
		}

		fmt.Printf("Deleted context %q\n", name)
		return autoRegenSSH()
	},
}

var configRenameCmd = &cobra.Command{
	Use:   "rename <OLD> <NEW>",
	Short: "Rename a context",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		oldName, newName := args[0], args[1]
		cfgPath := configFilePath()

		_, doc, err := config.LoadRaw(cfgPath)
		if err != nil {
			return err
		}

		if err := config.RenameContext(doc, oldName, newName); err != nil {
			return err
		}
		if err := config.SaveRaw(cfgPath, doc); err != nil {
			return err
		}

		fmt.Printf("Renamed context %q -> %q\n", oldName, newName)
		return autoRegenSSH()
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open config.yaml in $EDITOR",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		cfgPath := configFilePath()

		c := exec.Command(editor, cfgPath)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	},
}

func init() {
	configCreateCmd.Flags().StringVar(&configCreateFrom, "from", "", "clone from an existing context")

	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configCurrentCmd)
	configCmd.AddCommand(configUseCmd)
	configCmd.AddCommand(configCreateCmd)
	configCmd.AddCommand(configDeleteCmd)
	configCmd.AddCommand(configRenameCmd)
	configCmd.AddCommand(configEditCmd)
}

func loadConfig() (*config.Config, error) {
	cfgPath := configFilePath()
	return config.Load(cfgPath)
}

func configFilePath() string {
	if flagConfig != "" {
		return flagConfig
	}
	return config.DefaultConfigFile()
}

func contextList(cfg *config.Config) string {
	names := cfg.ContextNames()
	s := ""
	for i, n := range names {
		if i > 0 {
			s += ", "
		}
		s += n
	}
	return s
}

// autoRegenSSH regenerates SSH config after a context mutation.
func autoRegenSSH() error {
	cfgPath := configFilePath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil // non-fatal
	}

	rc, err := cfg.Resolve("")
	if err != nil {
		return nil // non-fatal
	}

	if err := runSetupSSH(rc); err != nil {
		fmt.Fprintf(os.Stderr, "warning: SSH config regeneration failed: %v\n", err)
	}
	return nil
}
