package cli

import (
	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/tui"
)

// agentAttachCmd opens the TUI pre-focused on the detail view for the named
// agent. Chat mode is alpha.3 scope; for alpha.2 attach lands on ViewDetail.
var agentAttachCmd = &cobra.Command{
	Use:   "attach <name>",
	Short: "Open the TUI focused on a specific agent (detail view)",
	Long: `Attach opens the dclaw TUI pre-focused on the named agent's detail view.

In alpha.3, 'c' from detail will open the chat pane.
For alpha.2, this is equivalent to: dclaw; then navigate to the agent; then press enter.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.RunAttached(daemonSocket, args[0])
	},
}
