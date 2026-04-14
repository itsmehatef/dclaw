package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Global flag values. Populated by cobra at parse time.
var (
	outputFormat string
	daemonSocket string
	verbose      bool
)

var rootCmd = &cobra.Command{
	Use:   "dclaw",
	Short: "dclaw — container-native multi-agent platform",
	Long: `dclaw is a container-native multi-agent platform.

It runs AI agents inside mandatory Docker sandboxes with per-agent isolation,
fleet management, and independently versioned channel plugins.

This is the v0.2.0-cli release: the command surface is wired up, but most
commands that would require the daemon exit 69 (EX_UNAVAILABLE) until v0.3+.`,
	SilenceUsage:  true,
	SilenceErrors: false,
}

// Execute is the main entry point called from cmd/dclaw/main.go.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&outputFormat, "output", "o", "table",
		"output format for list/get/status commands: table, json, yaml",
	)
	rootCmd.PersistentFlags().StringVar(
		&daemonSocket, "daemon-socket", "/var/run/dclaw/dispatcher.sock",
		"path to the dispatcher Unix socket",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false,
		"verbose logging to stderr",
	)

	// Subtrees.
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(channelCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(statusCmd)
}

// validateOutputFormat returns an error if -o is not one of the allowed values.
// Commands that respect -o call this in their RunE.
func validateOutputFormat() error {
	switch outputFormat {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("invalid --output %q: must be one of table, json, yaml", outputFormat)
	}
}
