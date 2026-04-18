package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

// wellKnownEnvKeys is the allowlist of environment variable names that are
// automatically inherited from the shell when the user does not pass them
// explicitly via --env. This list is intentionally small and explicit —
// we do NOT inherit arbitrary shell environment. To extend the list, add
// entries here only for credentials that every dclaw user is expected to
// supply to their agents.
//
// Behaviour: for each key in this list, if the key is NOT already present in
// the --env slice AND os.Getenv(key) != "", the key=value pair is prepended
// to the slice as a lowest-priority default. Explicit --env always wins.
var wellKnownEnvKeys = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_OAUTH_TOKEN",
}

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
	agentCreateEnvFile   string
	agentCreateLabel     []string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		env, err := composeEnv(agentCreateEnvFile, agentCreateEnv)
		if err != nil {
			return err
		}
		return withClient(ctx, func(c *client.RPCClient) error {
			a := client.Agent{
				Name:      args[0],
				Image:     agentCreateImage,
				Channel:   agentCreateChannel,
				Workspace: agentCreateWorkspace,
				Env:       env,
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
	agentUpdateImage   string
	agentUpdateEnv     []string
	agentUpdateEnvFile string
	agentUpdateLabel   []string
)

var agentUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an agent's image, env, or labels",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentUpdateImage == "" && len(agentUpdateEnv) == 0 && agentUpdateEnvFile == "" && len(agentUpdateLabel) == 0 {
			return fmt.Errorf("at least one of --image, --env, --env-file, --label must be provided")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		env, err := composeEnv(agentUpdateEnvFile, agentUpdateEnv)
		if err != nil {
			return err
		}
		return withClient(ctx, func(c *client.RPCClient) error {
			a := client.Agent{
				Name:   args[0],
				Image:  agentUpdateImage,
				Env:    env,
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
	agentCreateCmd.Flags().StringArrayVar(&agentCreateEnv, "env", nil,
		"set env var KEY=VAL (repeatable); ANTHROPIC_API_KEY and ANTHROPIC_OAUTH_TOKEN\n"+
			"\t\t\tare inherited from the shell if not specified")
	agentCreateCmd.Flags().StringVar(&agentCreateEnvFile, "env-file", "",
		"Path to dotenv-style file (KEY=VAL per line, # comments OK) to load env vars from. "+
			"Values from --env take precedence.")
	agentCreateCmd.Flags().StringArrayVar(&agentCreateLabel, "label", nil, "set label KEY=VAL (repeatable)")
	_ = agentCreateCmd.MarkFlagRequired("image")

	agentUpdateCmd.Flags().StringVar(&agentUpdateImage, "image", "", "new container image")
	agentUpdateCmd.Flags().StringArrayVar(&agentUpdateEnv, "env", nil,
		"set env var KEY=VAL (repeatable); ANTHROPIC_API_KEY and ANTHROPIC_OAUTH_TOKEN\n"+
			"\t\t\tare inherited from the shell if not specified")
	agentUpdateCmd.Flags().StringVar(&agentUpdateEnvFile, "env-file", "",
		"Path to dotenv-style file (KEY=VAL per line, # comments OK) to load env vars from. "+
			"Values from --env take precedence.")
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
		agentChatCmd,   // alpha.4 --one-shot
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

// mergeShellEnv takes an --env slice and returns a new slice that includes
// any well-known keys missing from the original. Keys already present in
// explicit are left unchanged (explicit always wins). Keys present in
// wellKnownEnvKeys but not in explicit are appended from os.Getenv if non-empty.
//
// The merge is done by name-presence check only (O(n*m), n=len(explicit),
// m=len(wellKnownEnvKeys)). Both lists are tiny (≤10 items each), so this
// is fine.
func mergeShellEnv(explicit []string) []string {
	// Build a set of key names already present in the explicit slice.
	present := make(map[string]bool, len(explicit))
	for _, kv := range explicit {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				present[kv[:i]] = true
				break
			}
		}
		// If there's no '=', treat the whole thing as a key with empty value.
		if !present[kv] {
			present[kv] = true
		}
	}

	out := make([]string, len(explicit))
	copy(out, explicit)

	for _, key := range wellKnownEnvKeys {
		if present[key] {
			continue // user-supplied value wins; do not override
		}
		if val := os.Getenv(key); val != "" {
			out = append(out, key+"="+val)
		}
	}
	return out
}

// parseDotenv reads a dotenv-style file at path and returns a []string of
// "KEY=VAL" pairs. Blank lines and lines starting with '#' are skipped.
// Lines without '=' are rejected with an error. An empty path returns an
// empty slice and nil error.
//
// Surrounding whitespace around keys and values is trimmed. Values are NOT
// unquoted — if the file contains `KEY="val"`, the stored value is literally
// `"val"`. This mirrors the simplest dotenv semantics and avoids surprise.
func parseDotenv(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read --env-file %q: %w", path, err)
	}
	defer f.Close()

	var out []string
	scan := bufio.NewScanner(f)
	lineNo := 0
	for scan.Scan() {
		lineNo++
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("--env-file %q: line %d: missing '=' (expected KEY=VAL)", path, lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf("--env-file %q: line %d: empty key", path, lineNo)
		}
		val := line[eq+1:]
		out = append(out, key+"="+val)
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("read --env-file %q: %w", path, err)
	}
	return out, nil
}

// composeEnv builds the final env map for an agent.create / agent.update
// request honoring the precedence order (lowest → highest; later overrides
// earlier):
//
//  1. Shell inheritance (wellKnownEnvKeys allowlist via mergeShellEnv)
//  2. --env-file values
//  3. --env explicit flags
//
// Implementation: concatenate [envFile, explicit] then hand to mergeShellEnv
// (which only fills keys NOT already present). kvSliceToMap walks the slice
// in order and later writes overwrite earlier ones — so explicit values
// naturally win over file values.
func composeEnv(envFilePath string, explicitEnv []string) (map[string]string, error) {
	fileEnv, err := parseDotenv(envFilePath)
	if err != nil {
		return nil, err
	}
	// Append in precedence order: file first (lowest), explicit second (wins).
	// mergeShellEnv then prepends any missing well-known shell keys as the
	// lowest-priority defaults.
	combined := make([]string, 0, len(fileEnv)+len(explicitEnv))
	combined = append(combined, fileEnv...)
	combined = append(combined, explicitEnv...)
	return kvSliceToMap(mergeShellEnv(combined)), nil
}
