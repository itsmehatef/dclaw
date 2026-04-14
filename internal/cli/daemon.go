package cli

import (
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the dclaw daemon (control plane)",
	Long: `Manage the dclaw daemon.

The daemon is the host-side control plane: fleet manager, channel router,
quota enforcer. It is not yet implemented in v0.2.0-cli — these subcommands
exit 69.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw daemon start")
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw daemon stop")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the dclaw daemon status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw daemon status")
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd)
}
