package cli

import (
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon health and fleet overview",
	Long: `Show the status of the dclaw daemon and the current fleet.

Not yet implemented in v0.2.0-cli — exits 69.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		return RequireDaemon(cmd, "dclaw status")
	},
}
