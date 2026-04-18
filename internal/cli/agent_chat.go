package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

// agentChatOneShotPrompt and agentChatTimeout hold the flag values for the
// agent chat subcommand.
var (
	agentChatOneShotPrompt string
	agentChatTimeout       time.Duration
)

// agentChatCmd implements `dclaw agent chat <name> --one-shot "<prompt>"`.
//
// It sends a single chat message to the named agent, collects all
// agent.chat.chunk notifications until final=true, prints each chunk's text
// to stdout, and exits 0 on success. If the timeout elapses before the stream
// completes, it exits 1. If the agent returns an error chunk (role="error"),
// it prints the error text to stderr and exits 2.
//
// The --one-shot flag is required. Interactive multi-turn chat is a TUI
// feature accessed via `dclaw` or `dclaw agent attach`. The --one-shot flag
// makes the intent explicit and keeps the command scriptable.
//
// Exit codes:
//   - 0: stream completed successfully (final=true, role="agent")
//   - 1: timeout, dial error, daemon RPC error
//   - 2: agent returned an error chunk (role="error")
var agentChatCmd = &cobra.Command{
	Use:   "chat <name>",
	Short: "Send a one-shot message to an agent and print the response",
	Long: `Send a single message to the named agent and print the response to stdout.

The agent.chat.send RPC is used; the daemon streams agent.chat.chunk
notifications which are printed as they arrive (each chunk on its own line).
The command exits after the final chunk.

Example:
  dclaw agent chat alice --one-shot "list the files in /workspace"

Exit codes:
  0 = success
  1 = RPC or network error
  2 = agent returned an error response (container not running, pi failed, etc.)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentChatOneShotPrompt == "" {
			return fmt.Errorf("--one-shot is required")
		}

		agentName := args[0]
		timeout := agentChatTimeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()

		return withClient(ctx, func(c *client.RPCClient) error {
			chunks, err := c.ChatSend(ctx, agentName, agentChatOneShotPrompt, "")
			if err != nil {
				return HandleRPCError(cmd, err)
			}

			exitCode := 0
			for event := range chunks {
				if event.Err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "error: stream error: %v\n", event.Err)
					os.Exit(1)
				}
				if event.Role == "error" {
					fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", event.Text)
					exitCode = 2
				} else {
					fmt.Fprint(cmd.OutOrStdout(), event.Text)
				}
				if event.Final {
					break
				}
			}

			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		})
	},
}

func init() {
	agentChatCmd.Flags().StringVar(&agentChatOneShotPrompt, "one-shot", "",
		"send this prompt to the agent and exit after the response (required)")
	agentChatCmd.Flags().DurationVar(&agentChatTimeout, "timeout", 60*time.Second,
		"maximum time to wait for the agent response")
}
