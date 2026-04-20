package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/tui"
)

var (
	outputFormat string
	daemonSocket string
	verbose      bool
	noMouse      bool
)

var rootCmd = &cobra.Command{
	Use:   "dclaw",
	Short: "dclaw — container-native multi-agent platform",
	Long: `dclaw is a container-native multi-agent platform.

It runs AI agents inside mandatory Docker sandboxes with per-agent isolation,
fleet management, and independently versioned channel plugins.

Run 'dclaw' with no arguments on an interactive terminal to open the TUI
dashboard.`,
	SilenceUsage:  true,
	SilenceErrors: false,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if noMouse {
			tui.NoMouse = true
		}
		// Resolve daemonSocket if the user did not supply --daemon-socket.
		// Goes through config.Resolve so env overrides and platform defaults
		// are honored exactly once, in one place.
		if daemonSocket == "" {
			paths, err := config.Resolve("", "")
			if err != nil {
				return err
			}
			daemonSocket = paths.SocketPath
		}
		return nil
	},
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
		&daemonSocket, "daemon-socket", "",
		"path to the dclaw daemon Unix socket (default: resolved via config.Resolve)",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false,
		"verbose logging to stderr",
	)
	rootCmd.PersistentFlags().BoolVar(
		&noMouse, "no-mouse", false,
		"disable mouse support in the TUI (use on stock macOS Terminal.app)",
	)

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(channelCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(statusCmd)
}

func validateOutputFormat() error {
	switch outputFormat {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("invalid --output %q: must be one of table, json, yaml", outputFormat)
	}
}

func newClient(ctx context.Context) (*client.RPCClient, error) {
	c := client.NewRPCClient(daemonSocket)
	if err := c.Dial(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func withClient(ctx context.Context, fn func(c *client.RPCClient) error) error {
	c, err := newClient(ctx)
	if err != nil {
		return DaemonUnreachable(err)
	}
	defer c.Close()
	return fn(c)
}
