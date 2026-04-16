package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/daemon"
)

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

Run 'dclaw' with no arguments on an interactive terminal to open the TUI
dashboard.`,
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
		&daemonSocket, "daemon-socket", defaultSocketPath(),
		"path to the dclaw daemon Unix socket",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false,
		"verbose logging to stderr",
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

// defaultSocketPath resolves the default daemon socket path at process start.
// Mirrors daemon.DefaultSocketPath so CLI and daemon agree.
func defaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return daemon.DefaultSocketPath(filepath.Join(home, ".dclaw"))
}

// newClient constructs an RPCClient at the resolved socket path. If the
// daemon isn't listening, Dial will return an error that the caller can map
// to exit 69.
func newClient(ctx context.Context) (*client.RPCClient, error) {
	c := client.NewRPCClient(daemonSocket)
	if err := c.Dial(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// withClient is a helper: opens a client, runs fn, closes the client.
func withClient(ctx context.Context, fn func(c *client.RPCClient) error) error {
	c, err := newClient(ctx)
	if err != nil {
		return DaemonUnreachable(err)
	}
	defer c.Close()
	return fn(c)
}
