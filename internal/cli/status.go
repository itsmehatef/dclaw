package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon health and fleet overview",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			s, err := c.DaemonStatus(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintStatus(cmd.OutOrStdout(), s)
		})
	},
}
