package cli

import (
	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/tui"
)

// agentAttachCmd opens the TUI in chat mode for the named agent.
// As of alpha.3, attach opens ViewChat directly.
var agentAttachCmd = &cobra.Command{
	Use:   "attach <name>",
	Short: "Open the TUI in chat mode for a specific agent",
	Long: `Attach opens the dclaw TUI pre-focused on the named agent's chat view.

Press 'esc' to return to the agent list. Use 'ctrl+c' to cancel a streaming
response. Press 'enter' to send; 'shift+enter' for a newline.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.RunChatAttached(daemonSocket, args[0])
	},
}
