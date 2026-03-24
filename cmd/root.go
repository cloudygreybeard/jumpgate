package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudygreybeard/jumpgate/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagContext string
	flagConfig  string
	flagVerbose int
	flagOutput  string
)

var rootCmd = &cobra.Command{
	Use:   "jumpgate",
	Short: "SSH relay access manager",
	Long: `Jumpgate manages SSH relay connections through a gate (jump host),
with optional Kerberos authentication, multi-context support, and
a hook-based architecture for platform-specific operations.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		level := slog.LevelWarn
		switch {
		case flagVerbose >= 2:
			level = slog.LevelDebug
		case flagVerbose == 1:
			level = slog.LevelInfo
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
		})))

		ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		cmd.SetContext(ctx)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagContext, "context", "", "override default context")
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "override config file path")
	rootCmd.PersistentFlags().CountVarP(&flagVerbose, "verbose", "v", "increase verbosity (-v info, -vv debug)")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "text", "output format: text, json, yaml, wide")
}

func Execute() error {
	return rootCmd.Execute()
}

func outputFormat() (output.Format, error) {
	return output.Parse(flagOutput)
}
