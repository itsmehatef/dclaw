# Phase 2 CLI Plan — v0.2.0-cli

**Goal:** Ship the `dclaw` CLI binary — the full command surface, wired up with cobra, stubs everywhere the daemon isn't — so Phase 3 can fill in implementations behind an already-locked UX.

**Scope:** The CLI bones only. No daemon, no Unix socket server, no SQLite, no Docker API calls, no channel plugins actually working. Every subcommand that would require the daemon exits 69 (EX_UNAVAILABLE) with a standardized message. `dclaw version` is the only fully-wired command.

**Timeline:** 1-2 days of focused work.

**Out of scope:** see Section 2 for the explicit list.

---

## 0. Status

| Field              | Value                                                             |
|--------------------|-------------------------------------------------------------------|
| **Milestone tag**  | `v0.2.0-cli`                                                      |
| **Est. duration**  | 1-2 days                                                          |
| **Prereqs**        | Phase 1 complete (`v0.1.0` tagged). Go 1.22+ installed.           |
| **Next milestone** | `v0.3.0-daemon` — daemon + SQLite + real CRUD behind the stubs.   |
| **Binary**         | `dclaw` (installed to `$GOPATH/bin` or `/usr/local/bin`).         |
| **Module path**    | `github.com/itsmehatef/dclaw`                                     |

---

## 1. Definition of Done

The following commands work end-to-end:

```bash
# Fully wired
dclaw version
# prints: dclaw version 0.2.0-cli (commit abc1234, built 2026-04-14, go1.26.2)

dclaw --help
# prints: full command tree rooted at dclaw

dclaw agent --help
dclaw channel --help
dclaw daemon --help

# Stubs (exit 69)
dclaw agent list
# stderr: dclaw agent list requires the dclaw daemon, which is not yet implemented — see v0.3+
# exit code: 69

dclaw agent list -o json
# stdout (valid JSON): {"error":"feature_not_ready","message":"...","exit_code":69,"milestone":"v0.3.0-daemon"}
# exit code: 69

dclaw agent create foo --image=dclaw-agent:v0.1
# stderr: ... requires the dclaw daemon ...
# exit code: 69
```

Build + test infrastructure:

```bash
make build       # produces ./bin/dclaw with version info stamped
make test        # go test ./... passes
make lint        # golangci-lint run passes (or is a no-op if binary missing)
make fmt         # gofmt -s -w
make install     # go install into $GOPATH/bin with ldflags
make clean       # rm -rf bin/
```

CI (`.github/workflows/build.yml`) runs `go vet`, `go build`, `go test` on push and pull request.

**Non-goals for v0.2.0-cli:** see Section 2.

---

## 2. Explicitly Out of Scope for v0.2.0-cli

Call-outs for the reviewer. Each of these is deliberate, not oversight.

| Out of scope                                          | Deferred to   | Reason                                                                    |
|-------------------------------------------------------|---------------|---------------------------------------------------------------------------|
| `dclaw run <image>` quick-create shortcut             | Never         | Dilutes the agent-as-resource framing. CRUD only.                         |
| `fleet.yaml` + `dclaw apply` + `dclaw export`         | v0.4+         | Hybrid model: CRUD first, yaml layered on top later.                      |
| Daemon / Unix socket server / SQLite storage          | v0.3+         | Phase 3.                                                                  |
| Docker API calls from the CLI                         | v0.3+         | Phase 3.                                                                  |
| Channel plugins actually routing messages             | v0.3+         | Phase 3.                                                                  |
| `dclaw agent exec` real exec path                     | v0.3+         | Flag parsing only in this phase.                                          |
| `dclaw agent logs` real log streaming                 | v0.3+         | Flag parsing only.                                                        |
| Auth / multi-tenant / RBAC                            | Post-v1       | Not a v1 concern.                                                         |
| Windows support                                       | Post-v1       | Unix sockets are mandatory.                                               |

---

## 3. Philosophy (why the command surface looks like this)

**Hybrid persistence, CRUD-first.** The eventual model is: imperative CRUD via the CLI (this phase's surface), a daemon SQLite database as the source of truth (phase 3), and declarative `dclaw apply -f fleet.yaml` layered on top (phase 4+). We are shipping the CRUD surface first because it lets operators click-ops individual agents without authoring YAML, and because it keeps the CLI stable while the declarative tooling evolves. This is why the surface looks like `docker` / `kubectl` subresource verbs and NOT like `kubectl apply -f`.

**Agents and channels are first-class resources.** No `dclaw run <image>` shortcut. If you want an agent, you `dclaw agent create <name> --image=<img>`. Named, addressable, long-lived resources — not one-shot invocations. The command surface reinforces this framing.

**Stub-first, ship the UX.** The command tree, flag parsing, help text, exit codes, and JSON output shape all land in v0.2.0-cli — even for commands that can't actually do their work yet. Downstream tooling (shell completions, scripts, wrappers, docs) can be built against the stable surface while the daemon is being filled in.

**Exit 69 means "not yet implemented, not broken."** Scripts wrapping dclaw can distinguish "daemon feature not ready" from generic failures and from usage errors. Paired with `-o json`, a structured `{"error":"feature_not_ready", ...}` envelope lets consumers key off `error` rather than scraping stderr.

---

## 4. Exit Codes

| Code | Meaning                                                                                   |
|------|-------------------------------------------------------------------------------------------|
| 0    | Success.                                                                                  |
| 1    | Generic error (internal error, I/O failure, parse failure, etc.).                         |
| 2    | Cobra usage error (bad flag, unknown command, missing required arg). Cobra's default.     |
| 69   | EX_UNAVAILABLE — the daemon is required for this command but is not yet implemented.      |

Exit 69 is the BSD `sysexits.h` value for "service unavailable" — widely understood, doesn't collide with anything cobra emits.

---

## 5. Command Surface

Every command supports `--help`. All subcommands in `agent`, `channel`, `daemon` (except `daemon --help` itself), and `status` currently stub to exit 69. `version` is fully wired.

### 5.1 Fully wired (v0.2.0-cli)

| Command                | Description                                                                                |
|------------------------|--------------------------------------------------------------------------------------------|
| `dclaw version`        | Prints `dclaw version <X.Y.Z> (commit <sha>, built <date>, <go version>)` on stdout.        |
| `dclaw help [command]` | Cobra default. Recursive help for any subcommand.                                           |

### 5.2 Agent subtree (stubbed)

All 11 `dclaw agent` commands parse their flags and validate required args, then call `RequireDaemon()` which emits the standard message and exits 69. Flag parsing is real; behavior is not.

| Command                                                                                          | Required args | Flags                                                                              | Notes                                                  |
|--------------------------------------------------------------------------------------------------|---------------|------------------------------------------------------------------------------------|--------------------------------------------------------|
| `dclaw agent create <name>`                                                                      | `<name>`      | `--image=<img>` (required), `--channel=<channel>`, `--workspace=<path>`, `--env=K=V` (repeatable), `--label=K=V` (repeatable) | Validates name is non-empty and image is provided.     |
| `dclaw agent list`                                                                               | —             | `-o table\|json\|yaml` (default table)                                             | Output format parsed; body stubbed.                    |
| `dclaw agent get <name>`                                                                         | `<name>`      | `-o table\|json\|yaml` (default table)                                             |                                                         |
| `dclaw agent describe <name>`                                                                    | `<name>`      | — (always human-readable)                                                          | Never respects `-o`; verbose-by-design.                |
| `dclaw agent update <name>`                                                                      | `<name>`      | `--image=<new>`, `--env=K=V`, `--label=K=V`                                        | At least one flag required; enforced by cobra.         |
| `dclaw agent delete <name>` (alias `rm`)                                                         | `<name>`      | —                                                                                  |                                                         |
| `dclaw agent start <name>`                                                                       | `<name>`      | —                                                                                  |                                                         |
| `dclaw agent stop <name>`                                                                        | `<name>`      | —                                                                                  |                                                         |
| `dclaw agent restart <name>`                                                                     | `<name>`      | —                                                                                  |                                                         |
| `dclaw agent logs <name>`                                                                        | `<name>`      | `--follow` / `-f`, `--tail=N` (default 100)                                        | Flags parsed; no streaming yet.                        |
| `dclaw agent exec <name> -- <cmd>...`                                                            | `<name>`      | — (everything after `--` is the command)                                           | Cobra `ArgsLenAtDash` guards the split.                |

### 5.3 Channel subtree (stubbed)

| Command                                                   | Required args               | Flags                                            |
|-----------------------------------------------------------|-----------------------------|--------------------------------------------------|
| `dclaw channel create <name>`                             | `<name>`                    | `--type=<discord\|slack\|cli\|...>` (required), `--config=<path>` |
| `dclaw channel list`                                      | —                           | `-o table\|json\|yaml` (default table)           |
| `dclaw channel get <name>`                                | `<name>`                    | `-o table\|json\|yaml`                           |
| `dclaw channel delete <name>`                             | `<name>`                    | —                                                |
| `dclaw channel attach <agent-name> <channel-name>`        | both                        | —                                                |
| `dclaw channel detach <agent-name> <channel-name>`        | both                        | —                                                |

### 5.4 System (stubbed except `version`)

| Command                 | Description                                                                                 |
|-------------------------|---------------------------------------------------------------------------------------------|
| `dclaw status`          | Daemon health + fleet overview. Table output default; `-o json\|yaml` supported. Stubbed.   |
| `dclaw daemon start`    | Start the daemon in the background. Stubbed.                                                |
| `dclaw daemon stop`     | Stop the daemon. Stubbed.                                                                   |
| `dclaw daemon status`   | Daemon status. Stubbed.                                                                     |

### 5.5 Global flags

Defined on `rootCmd`, inherited by all subcommands.

| Flag                  | Type     | Default                        | Description                                                                                  |
|-----------------------|----------|--------------------------------|----------------------------------------------------------------------------------------------|
| `-o`, `--output`      | string   | `table`                        | Output format for `list`/`get`/`status`. One of `table`, `json`, `yaml`. Ignored by others.  |
| `--daemon-socket`     | string   | `/var/run/dclaw/dispatcher.sock` | Path to the dispatcher Unix socket. Used by the future daemon client.                         |
| `-v`, `--verbose`     | bool     | `false`                        | Verbose logging to stderr.                                                                   |

---

## 6. Directory Layout

After this phase:

```
dclaw/
├── cmd/
│   └── dclaw/
│       └── main.go
├── internal/
│   ├── cli/
│   │   ├── root.go
│   │   ├── version.go
│   │   ├── agent.go
│   │   ├── channel.go
│   │   ├── daemon.go
│   │   ├── status.go
│   │   └── exit.go
│   ├── client/
│   │   └── client.go
│   ├── version/
│   │   └── version.go
│   ├── daemon/               # pre-existing .gitkeep stub — unchanged this phase
│   ├── protocol/             # pre-existing .gitkeep stub — unchanged this phase
│   └── sandbox/              # pre-existing .gitkeep stub — unchanged this phase
├── agent/                    # Phase 1 artifacts — unchanged
├── configs/                  # Phase 1 artifacts — unchanged
├── docs/
│   ├── architecture.md
│   ├── phase-1-plan.md
│   ├── phase-2-cli-plan.md   # THIS DOC
│   └── wire-protocol-spec.md
├── plugins/discord/          # pre-existing .gitkeep stub — unchanged this phase
├── pkg/mcp/                  # pre-existing .gitkeep stub — unchanged this phase
├── scripts/
│   └── smoke-cli.sh
├── .github/
│   └── workflows/
│       └── build.yml
├── Makefile
├── go.mod
├── go.sum                    # generated by `go mod tidy`
└── README.md                 # updated with CLI install + usage
```

If `internal/cli/agent.go` grows beyond ~300 lines, split into `agent_create.go`, `agent_list.go`, etc. — but only if it does. Start monolithic; split on actual pain.

---

## 7. Exact File Contents

These are copy-paste ready. Anything `REPLACE_ME` is a placeholder the implementer fills in at the step it's referenced.

### 7.1 `go.mod`

```
module github.com/itsmehatef/dclaw

go 1.22

require github.com/spf13/cobra v1.8.1
```

Note: the repo currently has `go 1.23` in `go.mod` with no deps. Step 1 overwrites it. The `toolchain` directive is omitted on purpose — pin the language version, let the host Go decide the toolchain.

The `go.sum` file is regenerated by `go mod tidy` in Step 2.

### 7.2 `cmd/dclaw/main.go`

```go
package main

import (
	"fmt"
	"os"

	"github.com/itsmehatef/dclaw/internal/cli"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "dclaw: panic: %v\n", r)
			os.Exit(1)
		}
	}()

	if err := cli.Execute(); err != nil {
		// cobra already printed the user-facing error; just exit.
		os.Exit(1)
	}
}
```

Rationale: tiny entry point. All real code lives under `internal/`. The recover guard means a panic bottoms out as exit 1 with a readable message rather than a Go stack trace for the user — during development, unset the defer (or run under `DCLAW_DEV=1` if we add that later) to get the stack.

### 7.3 `internal/cli/root.go`

```go
package cli

import (
	"github.com/spf13/cobra"
)

// Global flag values. Populated by cobra at parse time.
var (
	outputFormat string
	daemonSocket string
	verbose      bool
)

var rootCmd = &cobra.Command{
	Use:   "dclaw",
	Short: "dclaw — container-native multi-agent platform",
	Long: `dclaw is a container-native multi-agent platform.

It runs AI agents inside mandatory Docker sandboxes with per-agent isolation,
fleet management, and independently versioned channel plugins.

This is the v0.2.0-cli release: the command surface is wired up, but most
commands that would require the daemon exit 69 (EX_UNAVAILABLE) until v0.3+.`,
	SilenceUsage:  true,
	SilenceErrors: false,
}

// Execute is the main entry point called from cmd/dclaw/main.go.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&outputFormat, "output", "o", "table",
		"output format for list/get/status commands: table, json, yaml",
	)
	rootCmd.PersistentFlags().StringVar(
		&daemonSocket, "daemon-socket", "/var/run/dclaw/dispatcher.sock",
		"path to the dispatcher Unix socket",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false,
		"verbose logging to stderr",
	)

	// Subtrees.
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(channelCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(statusCmd)
}

// validateOutputFormat returns an error if -o is not one of the allowed values.
// Commands that respect -o call this in their RunE.
func validateOutputFormat() error {
	switch outputFormat {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("invalid --output %q: must be one of table, json, yaml", outputFormat)
	}
}
```

Add `"fmt"` to the imports block above. (Kept separate here for readability; the implementer should combine.)

### 7.4 `internal/cli/version.go`

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the dclaw version",
	Long: `Print the dclaw version string, git commit SHA, build date, and Go version.

The version information is stamped at build time via -ldflags. A binary built
without ldflags will report "dev" for all fields.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(),
			"dclaw version %s (commit %s, built %s, %s)\n",
			version.Version, version.Commit, version.BuildDate, version.GoVersion(),
		)
		return nil
	},
}
```

### 7.5 `internal/cli/agent.go`

```go
package cli

import (
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
```

Remember to add `"fmt"` to the imports. Every subcommand's RunE ends with `RequireDaemon(...)` so the stub message is perfectly uniform.

### 7.6 `internal/cli/channel.go`

```go
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
```

### 7.7 `internal/cli/daemon.go`

```go
package cli

import (
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the dclaw daemon (control plane)",
	Long: `Manage the dclaw daemon.

The daemon is the host-side control plane: fleet manager, channel router,
quota enforcer. It is not yet implemented in v0.2.0-cli — these subcommands
exit 69.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw daemon start")
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw daemon stop")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the dclaw daemon status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RequireDaemon(cmd, "dclaw daemon status")
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd)
}
```

### 7.8 `internal/cli/status.go`

```go
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
```

### 7.9 `internal/cli/exit.go`

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// ExitCodeNeedsDaemon is the BSD sysexits.h EX_UNAVAILABLE value, used to
// signal that a command requires the dclaw daemon, which is not yet shipped.
const ExitCodeNeedsDaemon = 69

// NextMilestone is the version at which the daemon-backed commands will start
// working. Keep this in one place so updates ship everywhere.
const NextMilestone = "v0.3.0-daemon"

// NotReadyPayload is the JSON envelope emitted by RequireDaemon when the user
// has selected -o json. Scripts can key off `error == "feature_not_ready"`.
type NotReadyPayload struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	ExitCode  int    `json:"exit_code"`
	Milestone string `json:"milestone"`
	Command   string `json:"command"`
}

// RequireDaemon writes the standardized "daemon required" message to either
// stdout (as structured JSON when -o json is set) or stderr (as human prose),
// then terminates the process with exit code 69.
//
// It calls os.Exit directly rather than returning an error because cobra's
// error-return path would produce its own "Error: ..." prefix; we want a
// clean, predictable message for scripting.
func RequireDaemon(cmd *cobra.Command, commandName string) error {
	msg := fmt.Sprintf(
		"%s requires the dclaw daemon, which is not yet implemented — see %s",
		commandName, NextMilestone,
	)

	if outputFormat == "json" {
		payload := NotReadyPayload{
			Error:     "feature_not_ready",
			Message:   msg,
			ExitCode:  ExitCodeNeedsDaemon,
			Milestone: NextMilestone,
			Command:   commandName,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), msg)
	}

	os.Exit(ExitCodeNeedsDaemon)
	return nil // unreachable
}
```

The function signature returns `error` so cobra's `RunE` stays type-consistent and so a future version (once the daemon ships) can swap the `os.Exit` for `return err`. For now it always exits; the `return nil` is unreachable.

### 7.10 `internal/client/client.go`

```go
// Package client defines the interface the CLI uses to talk to the dclaw
// daemon. In v0.2.0-cli the only implementation is NoopClient; in v0.3+ a
// real Unix-socket JSON-RPC client will implement it.
package client

import (
	"context"
	"errors"
)

// ErrDaemonNotImplemented is returned by NoopClient for every method.
var ErrDaemonNotImplemented = errors.New("dclaw daemon not yet implemented — see v0.3.0-daemon")

// Agent is a projection of the daemon's agent record suitable for display.
// Fields are deliberately minimal for v0.2.0-cli; they will grow in v0.3+.
type Agent struct {
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Channel   string            `json:"channel,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Status    string            `json:"status,omitempty"` // running, stopped, ...
}

// Channel is a projection of the daemon's channel record.
type Channel struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Config string `json:"config,omitempty"`
}

// Client is the interface the CLI uses to talk to the daemon. Methods
// intentionally look like the CLI's own subcommands — this keeps the
// mapping from command -> daemon call obvious.
//
// In v0.2.0-cli every method on every implementation returns
// ErrDaemonNotImplemented. The interface exists so phase 3 can drop in a
// real implementation without any CLI changes beyond constructing a
// different concrete type.
type Client interface {
	// Version / health
	DaemonVersion(ctx context.Context) (string, error)

	// Agents
	AgentCreate(ctx context.Context, a Agent) error
	AgentList(ctx context.Context) ([]Agent, error)
	AgentGet(ctx context.Context, name string) (Agent, error)
	AgentUpdate(ctx context.Context, a Agent) error
	AgentDelete(ctx context.Context, name string) error
	AgentStart(ctx context.Context, name string) error
	AgentStop(ctx context.Context, name string) error
	AgentRestart(ctx context.Context, name string) error
	AgentLogs(ctx context.Context, name string, tail int, follow bool) (<-chan string, error)
	AgentExec(ctx context.Context, name string, argv []string) (int, error)

	// Channels
	ChannelCreate(ctx context.Context, c Channel) error
	ChannelList(ctx context.Context) ([]Channel, error)
	ChannelGet(ctx context.Context, name string) (Channel, error)
	ChannelDelete(ctx context.Context, name string) error
	ChannelAttach(ctx context.Context, agentName, channelName string) error
	ChannelDetach(ctx context.Context, agentName, channelName string) error

	// Daemon lifecycle
	DaemonStart(ctx context.Context) error
	DaemonStop(ctx context.Context) error
	DaemonStatus(ctx context.Context) (string, error)
}

// NoopClient is the v0.2.0-cli implementation: every method returns
// ErrDaemonNotImplemented. The CLI does not actually call it yet; the
// RequireDaemon() helper short-circuits first. It exists so downstream
// code can begin wiring against the Client interface today.
type NoopClient struct{}

func (NoopClient) DaemonVersion(context.Context) (string, error) {
	return "", ErrDaemonNotImplemented
}
func (NoopClient) AgentCreate(context.Context, Agent) error { return ErrDaemonNotImplemented }
func (NoopClient) AgentList(context.Context) ([]Agent, error) {
	return nil, ErrDaemonNotImplemented
}
func (NoopClient) AgentGet(context.Context, string) (Agent, error) {
	return Agent{}, ErrDaemonNotImplemented
}
func (NoopClient) AgentUpdate(context.Context, Agent) error  { return ErrDaemonNotImplemented }
func (NoopClient) AgentDelete(context.Context, string) error { return ErrDaemonNotImplemented }
func (NoopClient) AgentStart(context.Context, string) error  { return ErrDaemonNotImplemented }
func (NoopClient) AgentStop(context.Context, string) error   { return ErrDaemonNotImplemented }
func (NoopClient) AgentRestart(context.Context, string) error {
	return ErrDaemonNotImplemented
}
func (NoopClient) AgentLogs(context.Context, string, int, bool) (<-chan string, error) {
	return nil, ErrDaemonNotImplemented
}
func (NoopClient) AgentExec(context.Context, string, []string) (int, error) {
	return 0, ErrDaemonNotImplemented
}
func (NoopClient) ChannelCreate(context.Context, Channel) error { return ErrDaemonNotImplemented }
func (NoopClient) ChannelList(context.Context) ([]Channel, error) {
	return nil, ErrDaemonNotImplemented
}
func (NoopClient) ChannelGet(context.Context, string) (Channel, error) {
	return Channel{}, ErrDaemonNotImplemented
}
func (NoopClient) ChannelDelete(context.Context, string) error { return ErrDaemonNotImplemented }
func (NoopClient) ChannelAttach(context.Context, string, string) error {
	return ErrDaemonNotImplemented
}
func (NoopClient) ChannelDetach(context.Context, string, string) error {
	return ErrDaemonNotImplemented
}
func (NoopClient) DaemonStart(context.Context) error  { return ErrDaemonNotImplemented }
func (NoopClient) DaemonStop(context.Context) error   { return ErrDaemonNotImplemented }
func (NoopClient) DaemonStatus(context.Context) (string, error) {
	return "", ErrDaemonNotImplemented
}

// Ensure NoopClient implements Client at compile time.
var _ Client = NoopClient{}
```

### 7.11 `internal/version/version.go`

```go
// Package version exposes build metadata stamped in at link time via -ldflags.
// A binary built without -X flags reports "dev" for every field.
package version

import "runtime"

// These vars are overwritten by -ldflags at build time. Keep them as `var`,
// not `const`, so the linker can set them.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// GoVersion returns the Go version this binary was compiled with.
func GoVersion() string {
	return runtime.Version()
}
```

### 7.12 `Makefile`

```makefile
# dclaw Makefile — v0.2.0-cli

BINARY      := dclaw
PKG         := github.com/itsmehatef/dclaw
CMD         := ./cmd/dclaw
BIN_DIR     := ./bin

# Version info stamped via -ldflags.
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.2.0-cli-dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS     := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildDate=$(BUILD_DATE)

GO          ?= go
GOFLAGS     ?=

.PHONY: all build test lint fmt vet install clean tidy smoke help

all: build

build: ## Build dclaw into ./bin/dclaw
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(CMD)
	@echo "Built $(BIN_DIR)/$(BINARY) ($(VERSION))"

test: ## Run unit tests
	$(GO) test $(GOFLAGS) ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint (no-op if not installed)
	@command -v golangci-lint >/dev/null 2>&1 \
		&& golangci-lint run \
		|| echo "golangci-lint not installed; skipping"

fmt: ## Format code
	$(GO) fmt ./...
	gofmt -s -w .

install: ## go install dclaw with version ldflags
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD)

tidy: ## go mod tidy
	$(GO) mod tidy

smoke: build ## Run the smoke test script against the freshly-built binary
	DCLAW_BIN=$(BIN_DIR)/$(BINARY) ./scripts/smoke-cli.sh

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
```

### 7.13 `.github/workflows/build.yml`

```yaml
name: build

on:
  push:
    branches: [main]
    tags: ['v*']
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: go vet
        run: go vet ./...

      - name: go build
        run: go build ./...

      - name: go test
        run: go test ./...

      - name: smoke-cli
        run: |
          make build
          ./scripts/smoke-cli.sh
```

### 7.14 `scripts/smoke-cli.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

DCLAW_BIN="${DCLAW_BIN:-./bin/dclaw}"

if [[ ! -x "$DCLAW_BIN" ]]; then
  echo "ERROR: $DCLAW_BIN not found or not executable" >&2
  exit 1
fi

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

echo "--- Test 1: dclaw version exits 0 ---"
out="$("$DCLAW_BIN" version)"
echo "  $out"
[[ "$out" == dclaw\ version\ * ]] || fail "unexpected version output: $out"
pass "version output"

echo "--- Test 2: dclaw --help exits 0 ---"
"$DCLAW_BIN" --help >/dev/null
pass "dclaw --help"

echo "--- Test 3: dclaw agent --help exits 0 ---"
"$DCLAW_BIN" agent --help >/dev/null
pass "dclaw agent --help"

echo "--- Test 4: dclaw agent list exits 69 ---"
set +e
"$DCLAW_BIN" agent list >/dev/null 2>/tmp/dclaw-smoke-stderr
code=$?
set -e
(( code == 69 )) || fail "expected exit 69, got $code"
grep -q "dclaw daemon" /tmp/dclaw-smoke-stderr || fail "expected 'dclaw daemon' in stderr"
pass "dclaw agent list exits 69 with daemon message"

echo "--- Test 5: dclaw agent list -o json emits structured error ---"
set +e
json="$("$DCLAW_BIN" agent list -o json 2>/dev/null)"
code=$?
set -e
(( code == 69 )) || fail "expected exit 69, got $code"
echo "$json" | grep -q '"error": *"feature_not_ready"' || fail "expected feature_not_ready in JSON"
echo "$json" | grep -q '"exit_code": *69' || fail "expected exit_code 69 in JSON"
pass "dclaw agent list -o json emits feature_not_ready"

echo "--- Test 6: dclaw agent create without --image fails with exit 2 ---"
set +e
"$DCLAW_BIN" agent create foo >/dev/null 2>&1
code=$?
set -e
(( code == 2 )) || fail "expected cobra usage exit 2, got $code"
pass "dclaw agent create without --image is a usage error"

echo "--- Test 7: dclaw agent list -o bogus fails ---"
set +e
"$DCLAW_BIN" agent list -o bogus >/dev/null 2>&1
code=$?
set -e
(( code == 1 )) || fail "expected exit 1, got $code"
pass "invalid -o rejected"

echo ""
echo "All CLI smoke tests passed."
```

Make executable in Step 3: `chmod +x scripts/smoke-cli.sh`.

### 7.15 `README.md` — section to add

Append this section after "Project Structure" in the existing README.md (do not replace the file). The block below is wrapped in four backticks so the inner ```bash``` fence renders correctly — copy only what's between the four-backtick fences.

````markdown
## Building the CLI (v0.2.0-cli)

Requires Go 1.22+.

```bash
# Build the binary into ./bin/dclaw
make build

# Install into $GOPATH/bin
make install

# Check the build
./bin/dclaw version
# dclaw version 0.2.0-cli (commit abc1234, built 2026-04-14T...Z, go1.22.x)
```

### CLI status in v0.2.0-cli

Only `dclaw version` and `dclaw --help` are fully wired. Every command that
would normally require the dclaw daemon (`agent create`, `agent list`, `channel
attach`, `daemon start`, etc.) exits with code **69 (EX_UNAVAILABLE)** and a
message pointing at the next milestone. Use `-o json` to receive a structured
`{"error": "feature_not_ready", ...}` envelope for scripting.

The daemon ships in `v0.3.0-daemon`.
````

Also update "Status" at the bottom of `README.md` from `Early development — Phase 1 ...` to `Early development — Phase 2 CLI (v0.2.0-cli): CLI bones shipped; daemon next (v0.3.0-daemon).`.

---

## 8. Implementation Steps (in order)

Every step is small enough to execute without re-planning. Run from `/Users/hatef/workspace/agents/atlas/dclaw/` unless otherwise noted.

### Step 1: Overwrite `go.mod`
Edit the existing `go.mod` (currently a single line `module github.com/itsmehatef/dclaw` plus `go 1.23`) to match Section 7.1. Do not run `go mod init`; the module line is already there.

### Step 2: Add the cobra dependency
```bash
go get github.com/spf13/cobra@v1.8.1
go mod tidy
git add go.mod go.sum
```

### Step 3: Create directory scaffolding
```bash
mkdir -p cmd/dclaw internal/cli internal/client internal/version scripts .github/workflows
```

### Step 4: Write `internal/version/version.go`
Copy Section 7.11 verbatim.

### Step 5: Write `internal/client/client.go`
Copy Section 7.10 verbatim.

### Step 6: Write `internal/cli/exit.go`
Copy Section 7.9 verbatim.

### Step 7: Write `internal/cli/root.go`
Copy Section 7.3 verbatim. Make sure `fmt` is in the import list.

### Step 8: Write `internal/cli/version.go`
Copy Section 7.4 verbatim.

### Step 9: Write `internal/cli/agent.go`
Copy Section 7.5 verbatim. Make sure `fmt` is in the import list.

### Step 10: Write `internal/cli/channel.go`
Copy Section 7.6 verbatim.

### Step 11: Write `internal/cli/daemon.go`
Copy Section 7.7 verbatim.

### Step 12: Write `internal/cli/status.go`
Copy Section 7.8 verbatim.

### Step 13: Write `cmd/dclaw/main.go`
Copy Section 7.2 verbatim.

### Step 14: First compile
```bash
go build ./...
```
Fix any import-ordering / unused-import errors surfaced by the compiler. If this doesn't compile on the first try, the root cause is almost always a missing `"fmt"` or `"os"` import.

### Step 15: Build the binary
```bash
make build
```
Expected: `./bin/dclaw` exists. `./bin/dclaw version` prints `dclaw version v0.2.0-cli-dev (commit <sha>, built <date>, go1.22.x)` or equivalent.

### Step 16: Write `scripts/smoke-cli.sh`
Copy Section 7.14 verbatim.
```bash
chmod +x scripts/smoke-cli.sh
```

### Step 17: Run the smoke tests
```bash
./scripts/smoke-cli.sh
```
All 7 tests should pass.

### Step 18: Write `Makefile`
Copy Section 7.12 verbatim. Re-run `make build && make smoke` to confirm parity.

### Step 19: Write `.github/workflows/build.yml`
Copy Section 7.13 verbatim.

### Step 20: Add basic unit tests

Create `internal/cli/cli_test.go`:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpDoesNotError(t *testing.T) {
	cases := []string{
		"--help",
		"version --help",
		"agent --help",
		"agent create --help",
		"agent list --help",
		"channel --help",
		"daemon --help",
		"status --help",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			var out, errb bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errb)
			rootCmd.SetArgs(strings.Fields(c))
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("unexpected error for %q: %v (stderr=%q)", c, err, errb.String())
			}
		})
	}
}

func TestInvalidOutputFormat(t *testing.T) {
	outputFormat = "bogus"
	t.Cleanup(func() { outputFormat = "table" })
	if err := validateOutputFormat(); err == nil {
		t.Fatal("expected error for bogus -o")
	}
}
```

Note: the exit-69 stubs call `os.Exit` directly, so they can't be unit-tested in-process without refactoring. That's acceptable for v0.2.0-cli — the smoke-test script covers those cases end-to-end.

### Step 21: Verify `go test ./...` and `go vet ./...`
```bash
make test
make vet
```

### Step 22: Update `README.md`
Append the section from 7.15; update the Status line. Leave the rest of the file alone.

### Step 23: Commit
```bash
git add \
  go.mod go.sum \
  cmd/dclaw/main.go \
  internal/cli/ \
  internal/client/ \
  internal/version/ \
  Makefile \
  scripts/smoke-cli.sh \
  .github/workflows/build.yml \
  README.md \
  docs/phase-2-cli-plan.md
git commit -m "Phase 2: dclaw CLI bones (v0.2.0-cli)"
```

### Step 24: Tag and push
```bash
git tag -a v0.2.0-cli -m "Phase 2: dclaw CLI surface + stubs"
git push origin main
git push origin v0.2.0-cli
```

### Step 25: Update handoff doc
Edit `/Users/hatef/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md`:
- Change the "Last updated" date to today.
- Mark "CLI bones shipped" in the Phase 2 section.
- Set Phase 2 state to "CLI in place; daemon next".
- Add a pointer to `docs/phase-2-cli-plan.md`.

---

## 9. Testing Strategy

### Unit tests (`internal/cli/*_test.go`)
1. Every `--help` invocation across the tree returns nil error.
2. Every stubbed command with required args fails with exit 2 (cobra usage) when called without them. (Covered by smoke test end-to-end; expanding to Go-level tests is optional.)
3. `validateOutputFormat` accepts `table`, `json`, `yaml` and rejects anything else.

The stubs themselves call `os.Exit(69)`, so direct in-process unit testing of exit behavior requires subprocess execution. `scripts/smoke-cli.sh` owns that path.

### Smoke tests (`scripts/smoke-cli.sh`)
1. `dclaw version` exits 0 and matches `^dclaw version `.
2. `dclaw --help`, `dclaw agent --help` exit 0.
3. `dclaw agent list` exits 69 and stderr contains `dclaw daemon`.
4. `dclaw agent list -o json` exits 69, stdout is valid JSON containing `"error": "feature_not_ready"` and `"exit_code": 69`.
5. `dclaw agent create foo` (no `--image`) exits 2 (cobra usage).
6. `dclaw agent list -o bogus` exits 1 (our validation).

### Non-functional
- Binary size: `./bin/dclaw` should be < 20 MB stripped (cobra is the only dep).
- Startup: `time ./bin/dclaw version` should be < 100 ms cold.

### What we're NOT testing in Phase 2
- Daemon responses (there is no daemon yet).
- Docker API calls (deferred to v0.3+).
- Channel plugin message routing (deferred).

---

## 10. Known Gotchas

1. **`os.Exit` vs `return err`** — `RequireDaemon` calls `os.Exit(69)` because returning an error from `RunE` causes cobra to prefix the output with `Error: ` and to set `SilenceErrors` semantics in odd ways. Direct exit is cleaner for scripting. The signature still returns `error` to stay compatible with `RunE`.

2. **Global flag state** — `outputFormat`, `daemonSocket`, and `verbose` are package-level `var`s. This is the cobra idiom but tests must reset them (see `t.Cleanup` in `TestInvalidOutputFormat`).

3. **`cobra.ArgsLenAtDash` for `exec`** — Cobra treats `--` specially: anything after it is captured as positional args, but `ArgsLenAtDash()` tells you *where* the dash was so you can split `<name>` from `<cmd>...`. Without this, you can't distinguish `dclaw agent exec foo bar baz` (name=foo, cmd=bar baz?) from `dclaw agent exec foo -- bar baz` (name=foo, cmd=bar baz).

4. **`MarkFlagRequired` returns an error** — always wrap with `_ = ` to satisfy `errcheck` lint. A failure means the flag name is wrong, which is a programming error caught in dev.

5. **ldflags paths** — the `-X` path is `github.com/itsmehatef/dclaw/internal/version.Version` exactly. A typo silently leaves `Version = "dev"`. Double-check by running the built binary before tagging.

6. **`go.mod` toolchain** — the spec uses `go 1.22` as the minimum. The existing repo said `go 1.23`; we downgrade to `1.22` to match the CI `setup-go` version and broaden compatibility. Future bumps require updating both `go.mod` and `.github/workflows/build.yml` in the same commit.

7. **`git describe --dirty`** — if the tree has uncommitted changes, the VERSION stamp will include `-dirty`. That's intentional — a binary you built from modified source should say so.

8. **`SilenceUsage: true` on rootCmd** — without this, every `RunE` error causes cobra to reprint the usage screen on top of the error message. Noisy. We silence usage but keep `SilenceErrors: false` so cobra still prints the error text.

9. **`-o yaml` output is parsed but no data path exists** — the flag is accepted, the validator allows it, but nothing serializes to YAML yet (everything exits 69 first). When v0.3+ wires up real data, add a gopkg.in/yaml.v3 dep.

10. **Subcommand help before daemon exit** — `dclaw agent list --help` must work (and return 0) even though `dclaw agent list` exits 69. Cobra handles this automatically because `--help` short-circuits before `RunE` is called. Verify in smoke test.

---

## 11. Error Handling

| Error                                       | Source                                 | Behavior                                                                     |
|---------------------------------------------|----------------------------------------|------------------------------------------------------------------------------|
| Missing required flag (e.g. `--image`)      | cobra                                  | stderr: "Error: required flag(s) \"image\" not set" + usage; exit 1          |
| Unknown subcommand                          | cobra                                  | stderr: "Error: unknown command..." + usage; exit 1                          |
| Invalid `-o` value                          | `validateOutputFormat()`               | stderr: "Error: invalid --output ...; must be one of table, json, yaml"; exit 1 |
| Daemon required (any stubbed command)       | `RequireDaemon()`                      | stderr (or JSON on stdout): standardized message; exit 69                    |
| Internal panic                              | recover() in `main`                    | stderr: "dclaw: panic: ..."; exit 1                                          |
| SIGINT                                      | Go default                             | Process exits; cobra isn't special-cased. Acceptable for v0.2.0-cli.          |

**Exit code summary** (as shipped in v0.2.0-cli):

- **0** — success
- **1** — all cobra errors (runtime + usage); `cmd/dclaw/main.go` hardcodes `os.Exit(1)` for any error cobra surfaces, and cobra does not differentiate usage from runtime by default
- **69** (`EX_UNAVAILABLE`) — daemon required but not implemented, raised via `RequireDaemon()`

Note: usage-vs-runtime differentiation (mapping usage errors like missing flags / unknown subcommands to exit 2) is deferred as a later polish item if we want it.

---

## 12. Release Checklist for v0.2.0-cli

1. [ ] `go build ./...` passes with zero warnings
2. [ ] `go vet ./...` clean
3. [ ] `go test ./...` passes
4. [ ] `golangci-lint run` clean (or skipped with note)
5. [ ] `make build` produces `./bin/dclaw` < 20 MB
6. [ ] `./bin/dclaw version` returns `dclaw version <git-describe> (commit <sha>, built <date>, <go version>)`
7. [ ] `./bin/dclaw --help` renders the full command tree
8. [ ] `./scripts/smoke-cli.sh` passes all 7 tests
9. [ ] `./bin/dclaw agent list` exits 69 with stderr `dclaw agent list requires the dclaw daemon...`
10. [ ] `./bin/dclaw agent list -o json` exits 69 with valid JSON containing `"error":"feature_not_ready"`
11. [ ] `./bin/dclaw agent create foo` (no `--image`) exits 2
12. [ ] README.md updated with build instructions + CLI status
13. [ ] `.github/workflows/build.yml` green on the PR / push
14. [ ] Commit tagged `v0.2.0-cli`
15. [ ] Tag pushed to `github.com/itsmehatef/dclaw`
16. [ ] Handoff doc (`~/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md`) updated: "Last updated" bumped to today, Phase 2 state set to "CLI in place; daemon next", pointer to `docs/phase-2-cli-plan.md` added

---

## 13. What Phase 3 Adds (preview)

- **Daemon binary** (`cmd/dclaw-daemon/main.go` — or the same `cmd/dclaw` binary with a hidden `daemon run` subcommand)
- **SQLite source of truth** for agents and channels
- **Unix socket server** implementing Boundary 2 of the wire protocol
- **Real `client.Client` implementation** (`internal/client/rpc.go`) backed by JSON-RPC over Unix socket
- **Docker API integration** — `internal/sandbox` grows real container management
- **First channel plugin wired end-to-end** — `plugins/discord/` actually routes messages to an agent

Phase 2 CLI is the contract that Phase 3 fills in. No CLI surface changes should be needed to go from v0.2.0-cli to v0.3.0-daemon — only the `NoopClient` gets swapped for a real one, and `RequireDaemon` calls get replaced with actual RPCs.

---

## 14. Open Questions

Flag anything the implementer should escalate instead of guessing.

- **`dclaw agent exec` stdin/tty flags** — the spec doesn't specify `-i`/`-t` yet. For v0.2.0-cli we accept none; `docker`-style `-it` lands in v0.3+ alongside the real exec path. Revisit before v0.3.
- **Completion scripts** — cobra has `completion bash|zsh|fish|powershell` for free. Not included in this phase's surface. Worth adding in a follow-up PR once commands stabilize.
- **Config file** — no `~/.dclaw/config.yaml` / `--config` support yet. All state lives in flags + the daemon (when it exists). Revisit if v0.3 needs per-user defaults.
- **`DCLAW_*` env-var overrides for global flags** — not included. Cobra's `viper` integration would give us this but it's a new dep. Defer to v0.3.
