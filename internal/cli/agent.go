package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents (create, list, start, stop, ...)",
	Long:  `Manage dclaw agents.`,
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
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a := client.Agent{
				Name:      args[0],
				Image:     agentCreateImage,
				Channel:   agentCreateChannel,
				Workspace: agentCreateWorkspace,
				Env:       kvSliceToMap(agentCreateEnv),
				Labels:    kvSliceToMap(agentCreateLabel),
			}
			if err := c.AgentCreate(ctx, a); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s created\n", args[0])
			return nil
		})
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
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			agents, err := c.AgentList(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintAgents(cmd.OutOrStdout(), agents)
		})
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
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a, err := c.AgentGet(ctx, args[0])
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintAgent(cmd.OutOrStdout(), a)
		})
	},
}

// ---------- describe ----------

var agentDescribeCmd = &cobra.Command{
	Use:   "describe <name>",
	Short: "Describe an agent in human-readable form",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a, err := c.AgentGet(ctx, args[0])
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Name:      %s\n", a.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Image:     %s\n", a.Image)
			fmt.Fprintf(cmd.OutOrStdout(), "Status:    %s\n", a.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", a.Workspace)
			if len(a.Env) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Env:")
				for k, v := range a.Env {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s\n", k, v)
				}
			}
			if len(a.Labels) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Labels:")
				for k, v := range a.Labels {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s\n", k, v)
				}
			}
			return nil
		})
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
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a := client.Agent{
				Name:   args[0],
				Image:  agentUpdateImage,
				Env:    kvSliceToMap(agentUpdateEnv),
				Labels: kvSliceToMap(agentUpdateLabel),
			}
			if err := c.AgentUpdate(ctx, a); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s updated\n", args[0])
			return nil
		})
	},
}

// ---------- delete ----------

var agentDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete an agent",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentDelete(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s deleted\n", args[0])
			return nil
		})
	},
}

// ---------- start / stop / restart ----------

var agentStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentStart(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s started\n", args[0])
			return nil
		})
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentStop(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s stopped\n", args[0])
			return nil
		})
	},
}

var agentRestartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentRestart(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s restarted\n", args[0])
			return nil
		})
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
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			ch, err := c.AgentLogs(ctx, args[0], agentLogsTail, agentLogsFollow)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			for line := range ch {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		})
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
		name := args[0]
		argv := args[dash:]

		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			// Wire stdio sinks.
			client.ExecStdoutWriter = cmd.OutOrStdout()
			client.ExecStderrWriter = cmd.ErrOrStderr()
			code, err := c.AgentExec(ctx, name, argv)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			os.Exit(code)
			return nil
		})
	},
}

// ---------- attach (NEW) ----------
// agent_attach.go is deferred to alpha.2 (requires internal/tui).

// ---------- init ----------

func init() {
	agentCreateCmd.Flags().StringVar(&agentCreateImage, "image", "", "container image for the agent (required)")
	agentCreateCmd.Flags().StringVar(&agentCreateChannel, "channel", "", "channel to bind to")
	agentCreateCmd.Flags().StringVar(&agentCreateWorkspace, "workspace", "", "host path to bind as /workspace")
	agentCreateCmd.Flags().StringArrayVar(&agentCreateEnv, "env", nil, "set env var KEY=VAL (repeatable)")
	agentCreateCmd.Flags().StringArrayVar(&agentCreateLabel, "label", nil, "set label KEY=VAL (repeatable)")
	_ = agentCreateCmd.MarkFlagRequired("image")

	agentUpdateCmd.Flags().StringVar(&agentUpdateImage, "image", "", "new container image")
	agentUpdateCmd.Flags().StringArrayVar(&agentUpdateEnv, "env", nil, "set env var KEY=VAL (repeatable)")
	agentUpdateCmd.Flags().StringArrayVar(&agentUpdateLabel, "label", nil, "set label KEY=VAL (repeatable)")

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
		agentAttachCmd, // alpha.2
	)
}

func kvSliceToMap(items []string) map[string]string {
	out := make(map[string]string, len(items))
	for _, kv := range items {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}
