package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage channels and bindings to agents",
	Long: `Manage dclaw channels.

A channel is a messaging-platform bridge (Discord, Slack, CLI, etc.) that
routes user messages to a bound agent.

All subcommands currently require the dclaw daemon, which is not yet
implemented. They exit with code 69 (EX_UNAVAILABLE) in v0.2.0-cli.`,
}

// ---------- create ----------

var (
	channelCreateType   string
	channelCreateConfig string
)

var channelCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch channelCreateType {
		case "discord", "slack", "cli":
			// known types
		case "":
			return fmt.Errorf("--type is required")
		default:
			// Unknown types are not rejected here — the daemon will validate.
		}
		return RequireDaemon(cmd, "dclaw channel create")
	},
}

// ---------- list ----------

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		return RequireDaemon(cmd, "dclaw channel list")
	},
}

// ---------- get ----------

var channelGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get a single channel by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		return RequireDaemon(cmd, "dclaw channel get")
	},
}

// ---------- delete ----------

var channelDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw channel delete")
	},
}

// ---------- attach / detach ----------

var channelAttachCmd = &cobra.Command{
	Use:   "attach <agent-name> <channel-name>",
	Short: "Attach a channel to an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw channel attach")
	},
}

var channelDetachCmd = &cobra.Command{
	Use:   "detach <agent-name> <channel-name>",
	Short: "Detach a channel from an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw channel detach")
	},
}

func init() {
	channelCreateCmd.Flags().StringVar(&channelCreateType, "type", "", "channel type: discord, slack, cli (required)")
	channelCreateCmd.Flags().StringVar(&channelCreateConfig, "config", "", "path to channel config file")
	_ = channelCreateCmd.MarkFlagRequired("type")

	channelCmd.AddCommand(
		channelCreateCmd,
		channelListCmd,
		channelGetCmd,
		channelDeleteCmd,
		channelAttachCmd,
		channelDetachCmd,
	)
}
