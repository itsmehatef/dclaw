package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents (create, list, start, stop, ...)",
	Long: `Manage dclaw agents.

An agent is a named, long-lived resource backed by a Docker container running
pi-mono. Agents are the primary unit of work in dclaw.

All subcommands currently require the dclaw daemon, which is not yet
implemented. They exit with code 69 (EX_UNAVAILABLE) in v0.2.0-cli.`,
}

// ---------- create ----------

var (
	agentCreateImage     string
	agentCreateChannel   string
	agentCreateWorkspace string
	agentCreateEnv       []string
	agentCreateLabel     []string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Flag validation would go here in v0.3+. For now, the daemon is missing.
		return RequireDaemon(cmd, "dclaw agent create")
	},
}

// ---------- list ----------

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		return RequireDaemon(cmd, "dclaw agent list")
	},
}

// ---------- get ----------

var agentGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get a single agent by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		return RequireDaemon(cmd, "dclaw agent get")
	},
}

// ---------- describe ----------

var agentDescribeCmd = &cobra.Command{
	Use:   "describe <name>",
	Short: "Describe an agent in human-readable form",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw agent describe")
	},
}

// ---------- update ----------

var (
	agentUpdateImage string
	agentUpdateEnv   []string
	agentUpdateLabel []string
)

var agentUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an agent's image, env, or labels",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentUpdateImage == "" && len(agentUpdateEnv) == 0 && len(agentUpdateLabel) == 0 {
			return fmt.Errorf("at least one of --image, --env, --label must be provided")
		}
		return RequireDaemon(cmd, "dclaw agent update")
	},
}

// ---------- delete ----------

var agentDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete an agent",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw agent delete")
	},
}

// ---------- start / stop / restart ----------

var agentStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw agent start")
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw agent stop")
	},
}

var agentRestartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw agent restart")
	},
}

// ---------- logs ----------

var (
	agentLogsFollow bool
	agentLogsTail   int
)

var agentLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show logs for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw agent logs")
	},
}

// ---------- exec ----------

var agentExecCmd = &cobra.Command{
	Use:                   "exec <name> -- <cmd>...",
	Short:                 "Exec a command inside an agent container",
	Args:                  cobra.MinimumNArgs(1),
	DisableFlagsInUseLine: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dash := cmd.ArgsLenAtDash()
		if dash < 0 || dash >= len(args) {
			return fmt.Errorf("usage: dclaw agent exec <name> -- <cmd>...")
		}
		// args[0..dash-1] is the name; args[dash..] is the command.
		return RequireDaemon(cmd, "dclaw agent exec")
	},
}

func init() {
	// create
	agentCreateCmd.Flags().StringVar(&agentCreateImage, "image", "", "container image for the agent (required)")
	agentCreateCmd.Flags().StringVar(&agentCreateChannel, "channel", "", "channel to bind to")
	agentCreateCmd.Flags().StringVar(&agentCreateWorkspace, "workspace", "", "host path to bind as /workspace")
	agentCreateCmd.Flags().StringArrayVar(&agentCreateEnv, "env", nil, "set env var KEY=VAL (repeatable)")
	agentCreateCmd.Flags().StringArrayVar(&agentCreateLabel, "label", nil, "set label KEY=VAL (repeatable)")
	_ = agentCreateCmd.MarkFlagRequired("image")

	// update
	agentUpdateCmd.Flags().StringVar(&agentUpdateImage, "image", "", "new container image")
	agentUpdateCmd.Flags().StringArrayVar(&agentUpdateEnv, "env", nil, "set env var KEY=VAL (repeatable)")
	agentUpdateCmd.Flags().StringArrayVar(&agentUpdateLabel, "label", nil, "set label KEY=VAL (repeatable)")

	// logs
	agentLogsCmd.Flags().BoolVarP(&agentLogsFollow, "follow", "f", false, "stream new log output")
	agentLogsCmd.Flags().IntVar(&agentLogsTail, "tail", 100, "number of lines to show from the end of the logs")

	agentCmd.AddCommand(
		agentCreateCmd,
		agentListCmd,
		agentGetCmd,
		agentDescribeCmd,
		agentUpdateCmd,
		agentDeleteCmd,
		agentStartCmd,
		agentStopCmd,
		agentRestartCmd,
		agentLogsCmd,
		agentExecCmd,
	)
}
