package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage channels and bindings to agents",
	Long: `Manage dclaw channels.

NOTE: in v0.3.0 channel commands persist records in the daemon database but
do NOT route messages. Real message routing lands in v0.4+ (Discord plugin).`,
}

var (
	channelCreateType   string
	channelCreateConfig string
)

var channelCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if channelCreateType == "" {
			return fmt.Errorf("--type is required")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			ch := client.Channel{Name: args[0], Type: channelCreateType, Config: channelCreateConfig}
			if err := c.ChannelCreate(ctx, ch); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s created (record only; no routing)\n", args[0])
			return nil
		})
	},
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			chs, err := c.ChannelList(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintChannels(cmd.OutOrStdout(), chs)
		})
	},
}

var channelGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get a single channel by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			ch, err := c.ChannelGet(ctx, args[0])
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintChannels(cmd.OutOrStdout(), []client.Channel{ch})
		})
	},
}

var channelDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.ChannelDelete(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s deleted\n", args[0])
			return nil
		})
	},
}

var channelAttachCmd = &cobra.Command{
	Use:   "attach <agent-name> <channel-name>",
	Short: "Attach a channel to an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.ChannelAttach(ctx, args[0], args[1]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s attached to %s\n", args[1], args[0])
			return nil
		})
	},
}

var channelDetachCmd = &cobra.Command{
	Use:   "detach <agent-name> <channel-name>",
	Short: "Detach a channel from an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.ChannelDetach(ctx, args[0], args[1]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s detached from %s\n", args[1], args[0])
			return nil
		})
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
