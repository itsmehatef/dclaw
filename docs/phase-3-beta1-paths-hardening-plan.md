# Phase 3 Beta.1 Paths-Hardening Plan — v0.3.0-beta.1 Paths Validation + State-Dir Consolidation

**Goal:** One batched, four-PR series that closes the `--workspace` validation gap discovered during the 2026-04-18 post-wipe RCA, consolidates the five-way duplication of home-dir resolution, fixes the `cmd/dclaw/main.go` vs `daemon.DefaultSocketPath` split-brain as a side effect, wires an append-only audit log for every workspace decision, and rewrites `scripts/smoke-daemon.sh` to stop manipulating `$HOME`. Zero new user features; two new user-visible surfaces (`dclaw config {get,set} workspace-root` and `--workspace-trust=<reason>` on `agent create`). Container-escape hardening (caps/seccomp/user-ns/ReadonlyRootfs/docker.sock-refuse) is deliberately out of scope — it ships later as beta.2.

**Prereq:** `v0.3.0-alpha.4.1` tagged at commit `76405ac`. `docs/phase-3-alpha4-plan.md` is the most recently shipped phase doc. No migrations in flight. Origin `main` at `76405ac`. Only migration on disk is `internal/store/migrations/0001_initial.sql`. `golang.org/x/text v0.35.0` is already in `go.sum` as indirect — beta.1 promotes it to direct. `go 1.25.0` installed. Docker reachable.

---

## 0. Status

**SHIPPED (2026-04-22) as `v0.3.0-beta.1-paths-hardening.2`.** Closed on origin at tag; see `WORKLOG.md` 2026-04-20 and 2026-04-21/22 for the ship notes.

**Commits (on `main`, in order):**

| Hash | Scope |
|------|-------|
| `f23dd70` | docs: plan + WORKLOG |
| `c9ea6c7` | beta.1(A): `config.Resolve` refactor + socket split-brain fix |
| `3155900` | beta.1(B): `--state-dir` flag + `DCLAW_WORKSPACE_ROOT` env stub |
| `3288b0a` | beta.1(C): validator + audit log + `--workspace-trust` + `config` cmd |
| `26ddc96` | beta.1(BC): wire `--state-dir` through PR-C's `internal/cli` sites |
| `bbd85d1` | beta.1(D): smoke-daemon.sh rewrite + workspace-root runbook + shellcheck |
| `0042e98` | docs(WORKLOG): 2026-04-20 build cycle |
| `4423cce` | beta.1(E): review fixes |
| `8dcb275` | beta.1(F): gate macOS-specific validator test rows for Linux CI |
| `29db7b7` | ci: fix stale smoke-cli Test 5 expectation |
| `37c64d8` | hotfix(smoke): export DCLAW_WORKSPACE_ROOT |
| `34367c5` | hotfix(smoke): fix Tests 14/15/16 assertions |

| Field | Value |
|---|---|
| **Target tag** | `v0.3.0-beta.1-paths-hardening.2` (shipped; `.1` and unsuffixed precursors left as historical markers) |
| **Branch** | `main` (single batched review cycle) |
| **Base commit** | `76405ac` (`v0.3.0-alpha.4.1`) |
| **Est. duration** | 2–3 days (4 PRs, sequenced A → B‖C → D) |
| **Actual duration** | 4 days (2026-04-19 plan → 2026-04-22 ship; PR-A through PR-D built 2026-04-20, review + ship + post-ship hotfix saga 2026-04-21/22) |
| **Prereqs** | alpha.4.1 green; smoke-daemon.sh Tests 1-13 green on the alpha.4.1 baseline |
| **Incident trigger** | 2026-04-18 machine-wipe RCA (see `WORKLOG.md` 2026-04-19) |

---

## 1. Overview

The machine-wipe incident on 2026-04-18 triggered a full path-handling audit. Definitive RCA is unrecoverable (logs wiped with the host), but the audit surfaced one live architecture hole and several fragile patterns:

1. `--workspace` has zero validation. `internal/cli/agent.go:334` → `internal/daemon/lifecycle.go:62` → `internal/sandbox/docker.go:90-96` passes the raw string straight to `mount.Mount{Source: spec.Workspace}`. An agent can be created with `--workspace=/` or `--workspace=$HOME`, giving the in-container process write access to host paths.
2. Five-way duplication of `os.UserHomeDir() + "/.dclaw"` across `cmd/dclaw/main.go:65`, `internal/client/rpc.go:322-327`, `internal/cli/root.go:82-88`, `internal/cli/daemon.go:30,91`, `internal/daemon/config.go:26-33`. Each copy is slightly different.
3. `cmd/dclaw/main.go:64-70`'s `resolveSocket` returns `home + "/.dclaw/dclaw.sock"` literally, bypassing `daemon.DefaultSocketPath`'s `$XDG_RUNTIME_DIR` check. On Linux with XDG set, CLI and daemon point at different sockets. The TUI bare-invocation path is affected; `dclaw <subcommand>` hits the correct path via `cli/root.go:defaultSocketPath`. Split-brain by construction.
4. `scripts/smoke-daemon.sh:10-11` reassigns `$HOME=$(mktemp -d)` and `scripts/smoke-daemon.sh:44` does `rm -rf "$HOME"`. Safe as written today (trap registered after the reassignment on line 46), but any refactor that reorders, any `source`-style invocation, any `TMPDIR=$HOME` env injection, and cleanup deletes the real home.
5. `internal/daemon/router.go:347` maps errors via `strings.Contains`. A new `ErrWorkspaceForbidden` cannot be cleanly surfaced through that mechanism; every error-class addition forces an ordering-sensitive edit to the string-substring ladder.
6. `docs/phase-1-plan.md:540` claims "`rm -rf /` inside a container — host is fine." The bind-mounted `/workspace` is host-accessible; `rm -rf /` descends into it.

**What beta.1-paths-hardening delivers (IN SCOPE):**

- **PR-A — State-dir resolution consolidation.** New `internal/config/resolve.go` with one `Resolve(stateDirFlag, socketFlag)` call site. Precedence `flag > env (DCLAW_STATE_DIR) > default (~/.dclaw)`. Five call sites converted. `daemon.LoadConfig` becomes a thin wrapper around `config.Resolve`. `cmd/dclaw/main.go:64-70`'s literal socket path is replaced with the shared resolver — fixing the split-brain as a side effect. Zero user-visible change.
- **PR-B — Flag + env surface.** `--state-dir` persistent flag on `dclaw` rootCmd. `DCLAW_STATE_DIR` env var picked up in `config.Resolve` (PR-A prepared the hook). `DCLAW_WORKSPACE_ROOT` env var picked up by PR-C's config reader. Flag-wins-over-env-wins-over-config precedence applied uniformly.
- **PR-C — Validator + wire + audit log.** New `internal/paths/` package with a pure `Policy.Validate` (no filesystem required for the happy path except `EvalSymlinks`), a `paths.OpenSafe` TOCTOU helper that opens with `O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC` and re-canonicalizes through `/proc/self/fd/N` on Linux or `F_GETPATH` on darwin. New `paths.ErrWorkspaceForbidden` sentinel, wired through `internal/daemon/lifecycle.go` (`AgentCreate` only), `internal/protocol/messages.go` (new `ErrWorkspaceForbidden = -32007`), `internal/daemon/router.go` (router `mapError` rewritten with `errors.Is` switch), and `internal/cli/exit.go` (structured renderer for `workspace_forbidden` mirroring the existing `feature_not_ready` precedent). `--workspace-trust=<reason>` flag with required non-empty reason string, persisted per-agent in `agents.workspace_trust_reason TEXT`, schema migration `0002_workspace_trust.sql`. `$STATE_DIR/audit.log` append-only `O_APPEND|O_CREATE|O_SYNC` mode 0600, one JSON line per validate call. `dclaw config get|set workspace-root` subcommand reading/writing `$STATE_DIR/config.toml`.
- **PR-D — Smoke-script rewrite + docs.** Full rewrite of `scripts/smoke-daemon.sh` — no `$HOME` reassignment anywhere, `SMOKE_STATE` with `mktemp -d` + prefix whitelist guard + trap armed before exports, `DCLAW_STATE_DIR` set to `$SMOKE_STATE`, every dclaw/dclawd invocation also passed `--state-dir "$SMOKE_STATE"` as belt-and-suspenders. `docs/phase-1-plan.md:540` corrected. New `docs/workspace-root.md` runbook. `lint` target gains `shellcheck scripts/*.sh agent/*.sh`.

**What this phase does NOT deliver (NOT IN SCOPE):**

- **Container-escape hardening** (caps drop, seccomp profile, user-namespace remapping, `ReadonlyRootfs: true`, non-root UID in agent image, `PidsLimit`, refuse `-v /var/run/docker.sock`) → separate **beta.2 sandbox-hardening** phase. Rationale: validation and sandbox-break are different attack surfaces; validation is a cheap, high-value win that does not block on the container-escape work.
- **Easier setup for workspace-root** (auto-create on first use, interactive prompt, `--init`-style wizard) → post-beta.1 follow-up. Rationale: the hard requirement — "cannot create an agent with a dangerous workspace without explicit operator trust" — is orthogonal to ergonomics. An explicit error with two one-line remediations is acceptable for beta.1.
- **TOML config file as canonical source for all settings** (socket path, state-dir, log level, workspace-root all in `config.toml`) → deferred. beta.1 creates `config.toml` on first `dclaw config set workspace-root <path>` with exactly one key. Fully-factored config file is a follow-up.
- **XDG-aware state split on Linux** (`$XDG_DATA_HOME/dclaw` for state.db, `$XDG_CONFIG_HOME/dclaw` for config.toml, `$XDG_STATE_HOME/dclaw` for logs) → follow-up. beta.1 keeps everything under `$STATE_DIR` (default `~/.dclaw`) for cross-platform uniformity.
- **Windows denylist** via `runtime.GOOS` switch → follow-up. beta.1 targets darwin + linux; Windows is not currently a supported target.
- **Audit-log rotation** → out of scope. beta.1 keeps `audit.log` forever, unrotated. Size growth is bounded by agent-create frequency (one line per create); file-rotation is a follow-up.
- **`dclaw doctor workspace` subcommand** to pre-flight a path before invoking `agent create` → see §11 Q3 for whether this lands in PR-C or a follow-up.
- **New sandbox features (seccomp, caps drop)**, rootless Docker mode, user-namespace remapping — all beta.2.

**Sequence relative to the product roadmap:**

```
alpha.4 → reliability + ergonomics pass (shipped 2026-04-17)
alpha.4.1 → hotfixes on alpha.4 (shipped 2026-04-18)
                                    ← machine wipe 2026-04-18 ←
beta.1-paths-hardening → --workspace validation + state-dir consolidation ← THIS PLAN
beta.1                 → logs view + toasts + chat history persistence (pre-wipe content, to be re-derived)
beta.2-sandbox-hardening → container escape surface
v0.3.0                 → GA
```

---

## 2. Dependencies

**One new direct dependency** promoted from indirect:

```
golang.org/x/text   v0.35.0   // was indirect, now direct — used by internal/paths for NFC normalization
```

No other `go.mod` changes. `golang.org/x/text` is already in `go.sum`; promotion is a `go mod tidy` side effect.

**No TOML dependency added.** §11 Q2 documents the decision: `config.toml` in beta.1 has exactly one key (`workspace-root`) and is parsed/written with a homegrown ~40-line reader that handles one `key = "value"` shape. If config grows, we revisit and pull `github.com/pelletier/go-toml/v2`.

After any inadvertent `go.mod` touch, run `go mod tidy` from the repo root.

---

## 3. Sequencing

**PR dependency graph:**

```
PR-A  (state-dir consolidation, ~150 lines, pure refactor)
   ↓
   ├── PR-B  (--state-dir flag + DCLAW_STATE_DIR env, ~100 lines)
   └── PR-C  (validator + wire + audit log + migration + --workspace-trust + config cmd, ~400 lines)
                                         ↓ (both merged)
                                       PR-D  (smoke rewrite + docs + shellcheck lint, ~200 lines)
```

- **PR-A is the gate.** It changes no behavior but is the precondition for both B and C — B consumes `config.Resolve`'s env lookup, C depends on knowing where `$STATE_DIR` is without re-implementing the resolver.
- **PR-B and PR-C are independent** after PR-A merges. Different file sets, different reviewers possible. Both target `main`.
- **PR-D is the tail.** Depends on B (to exercise `DCLAW_STATE_DIR` in the smoke script) and C (to exercise the validator and audit log in the smoke script).

All four PRs reviewed together as one batched cycle per Hatef's directive; PRs get merged in the sequence above.

---

## 4. Per-PR Spec

### 4.1 PR-A — State-Dir Resolution Consolidation

**Goal:** One canonical `Resolve(stateDirFlag, socketFlag string) (Paths, error)` function. Five call sites converted. Zero user-visible change. Socket split-brain fixed as a side effect.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/config/resolve.go` | NEW | `Paths{StateDir, SocketPath}` struct + `Resolve(stateDirFlag, socketFlag string) (Paths, error)` + `DefaultSocketPath(stateDir string) string` (moved from `internal/daemon/config.go:69-78`). Precedence: `stateDirFlag != "" → flag; else os.Getenv("DCLAW_STATE_DIR") != "" → env; else filepath.Join(home, ".dclaw")`. `socketFlag != "" → flag; else DefaultSocketPath(stateDir)`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/config/resolve_test.go` | NEW | Table-driven precedence tests using `t.Setenv("DCLAW_STATE_DIR", ...)`. Covers: flag-wins-over-env, env-wins-over-default, default-when-nothing-set, empty-flag-is-not-flag, socket-derived-from-state-dir, explicit-socket-wins. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/config.go` | MODIFIED | `LoadConfig` becomes a thin wrapper: calls `config.Resolve`, populates `Config.DBPath`, `LogDir`, `LogPath`, `PIDPath` via `filepath.Join`. `DefaultSocketPath` removed from this file (re-exported in `internal/config`). Existing callers of `daemon.DefaultSocketPath` transparently redirect to `config.DefaultSocketPath` via a compatibility alias `var DefaultSocketPath = config.DefaultSocketPath` to avoid PR-A touching call sites outside the five listed below. Net lines: ~-30. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/root.go` | MODIFIED | Lines 82-88 `defaultSocketPath()` deleted. Line 54 `daemonSocket` default changed from `defaultSocketPath()` to `""`, with `PersistentPreRunE` resolving via `config.Resolve("", daemonSocket)` if still empty at command-execution time. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/daemon.go` | MODIFIED | Lines 30-34 and 91-92 — drop `os.UserHomeDir` + `filepath.Join(home, ".dclaw")`. `daemonStartCmd.RunE` calls `config.Resolve(stateDirFlag, daemonSocket)` once, passes both resolved paths to `daemon.LoadConfig`. `daemonStopCmd.RunE` same. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/client/rpc.go` | MODIFIED | Lines 321-328 `DefaultSocketPath()` delegates to `config.DefaultSocketPath(stateDir)` where `stateDir` is resolved via `config.Resolve("", "")`. Removes the `internal/daemon` import from `internal/client` (already one-way; preserved by going through `internal/config`, which is below both). |
| `/Users/macmini/workspace/agents/atlas/dclaw/cmd/dclaw/main.go` | MODIFIED | Lines 61-70 `resolveSocket()` deleted. Replaced with one-liner `socket := config.MustResolveSocket()` (new convenience helper on `internal/config` that panics on unresolvable home — matches the current error behavior which returns `/tmp/dclaw.sock`). This is the site of the split-brain bug: the literal `home + "/.dclaw/dclaw.sock"` skipped `XDG_RUNTIME_DIR`. Post-PR-A, main.go goes through the same `DefaultSocketPath` that the rest of the codebase uses. |

**Import boundary:** `internal/config` imports only `os`, `path/filepath`, `runtime`. No reverse imports. `internal/daemon` imports `internal/config`. `internal/cli`, `internal/client`, `cmd/dclaw` all import `internal/config` directly — keeps the dependency DAG acyclic.

**Test plan:**

- `go test ./internal/config/...` — precedence table, must pass.
- `go test ./...` — regression; nothing should break since behavior is preserved.
- `go vet ./...` — clean.
- `go build ./cmd/dclaw ./cmd/dclawd` — both compile.
- Manual smoke (split-brain check): on Linux, `XDG_RUNTIME_DIR=/tmp/xdg-test mkdir -p $XDG_RUNTIME_DIR && ./bin/dclawd --socket $XDG_RUNTIME_DIR/dclaw.sock &`. Then run `./bin/dclaw daemon status` (the CLI path, which works pre-PR-A) and separately run plain `./bin/dclaw` on a TTY (the TUI path, which uses `main.go:resolveSocket`). Both must reach the same socket. Pre-PR-A, the TUI path misses.

**Acceptance criteria:**

1. `go test ./internal/config/...` green with ≥6 precedence rows.
2. `go test ./...` all tests still pass.
3. `DefaultSocketPath` has exactly one implementation in the repo (grep for `XDG_RUNTIME_DIR` returns one location).
4. `os.UserHomeDir` appears in `internal/config/resolve.go` exactly once; appears nowhere else outside tests.
5. On Linux with `XDG_RUNTIME_DIR` set and pointing to a writable dir, CLI `dclaw daemon status` and bare-invocation TUI reach the same socket (both use the XDG path). Pre-PR-A they diverge; post-PR-A they converge.

**Rollout risk:** Low. Pure refactor. Worst case: `config.Resolve` behavior differs subtly from one of the five old sites (e.g., different response when home-dir resolution fails). Mitigated by copying the exact fallback behavior of `internal/client/rpc.go:322-327` which returns `/tmp/dclaw.sock` on home-dir failure.

**Rollback:** Revert the merge commit. `daemon.DefaultSocketPath` compatibility alias means no wire-level rollback consideration.

---

### 4.2 PR-B — Flag + Env Var Surface

**Goal:** Add `--state-dir` persistent flag on `dclaw` rootCmd. Wire `DCLAW_STATE_DIR` through `config.Resolve`. Pre-declare `DCLAW_WORKSPACE_ROOT` env var for PR-C to pick up.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/root.go` | MODIFIED | Add `rootCmd.PersistentFlags().StringVar(&stateDirFlag, "state-dir", "", "override state directory (default: $DCLAW_STATE_DIR or ~/.dclaw)")`. Wire `stateDirFlag` through `config.Resolve(stateDirFlag, daemonSocket)` in the same PersistentPreRunE hook PR-A introduced. Interaction with `--daemon-socket`: explicit socket wins; otherwise the resolver derives it from the resolved state-dir (matching PR-A). |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/daemon.go` | MODIFIED | `daemonStartCmd` inherits persistent `--state-dir` automatically via cobra. `exec.Command(dclawdPath, "--socket", cfg.SocketPath, "--state-dir", cfg.StateDir)` already passes state-dir through; behavior preserved. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/config/resolve.go` | MODIFIED | No code change needed beyond PR-A (env-var lookup already implemented per PR-A spec). PR-B verifies the env-var precedence tests added in PR-A still pass. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/config/resolve_test.go` | MODIFIED | Add `TestResolveFlagEnvRoundTrip` table row that asserts `dclawd --state-dir /tmp/x` and `dclaw --state-dir /tmp/x` resolve to the same paths. |

**No wire protocol changes. No daemon-side changes** — `cmd/dclawd/main.go:32` already has `--state-dir` flag; PR-B only adds it to the CLI side for parity.

**Test plan:**

- Unit: flag-override, env-override, combined (flag wins).
- Integration: `DCLAW_STATE_DIR=/tmp/dctestdir ./bin/dclaw daemon start` and `./bin/dclaw --state-dir /tmp/dctestdir daemon start` — both produce identical `cfg.SocketPath`, `cfg.StateDir`, `cfg.DBPath`.
- Round-trip: `./bin/dclawd --state-dir /tmp/x &` followed by `./bin/dclaw --state-dir /tmp/x daemon status` — status returns. Omit either `--state-dir` → connection refused because daemon listens on different socket.

**Acceptance criteria:**

1. `dclaw --help` shows `--state-dir` flag.
2. `dclaw --state-dir /tmp/foo daemon start` creates `/tmp/foo/dclaw.sock` and `/tmp/foo/state.db`.
3. `DCLAW_STATE_DIR=/tmp/bar dclaw daemon status` connects to `/tmp/bar/dclaw.sock`.
4. Flag wins over env: `DCLAW_STATE_DIR=/tmp/env-wins dclaw --state-dir /tmp/flag-wins daemon status` probes `/tmp/flag-wins/dclaw.sock`.
5. `go test ./internal/config/...` all rows green including new flag-env-combined row.

**Rollout risk:** Low. One new flag; additive. Existing users who do not set the flag or env var observe identical behavior.

**Rollback:** Revert. No state-persistence implications.

---

### 4.3 PR-C — Validator + Wire + Audit Log + Migration + `--workspace-trust` + `config` Command

**Goal:** End-to-end workspace path validation on `agent create`. Structured error wire-through. Per-agent trust-override persisted. Append-only audit log. New CLI subcommand for managing the workspace root.

**New files:**

| File | Purpose |
|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/paths/policy.go` | `Policy{AllowRoot, Denylist, AllowTrust}` struct + `Policy.Validate(raw string) (canonical string, err error)`. Pure function; one filesystem call (`EvalSymlinks`). |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/paths/opensafe.go` | `OpenSafe(path string) (*os.File, string, error)`. Opens with `O_DIRECTORY\|O_NOFOLLOW\|O_CLOEXEC`. On Linux, re-canonicalizes via `os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))`. On darwin, uses `unix.FcntlInt(f.Fd(), F_GETPATH, 0)` via `golang.org/x/sys/unix` (already in `go.sum` as indirect). Re-runs `Policy.Validate` on the canonical result. Returns the open fd (caller passes its canonical path to Docker bind mount; the fd stays open until `AgentCreate` returns, defeating TOCTOU). |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/paths/errors.go` | `var ErrWorkspaceForbidden = errors.New("workspace path forbidden by policy")`. Sentinel used for `errors.Is`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/paths/policy_test.go` | 30+ table rows — see §6. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/paths/opensafe_test.go` | Symlink-after-validate race test using `t.TempDir`. Tests that OpenSafe's re-canonicalization catches a symlink that was created between `Validate` and `open`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/audit/audit.go` | `Logger{file *os.File}` struct. `New(stateDir string) (*Logger, error)` opens `$STATE_DIR/audit.log` with `O_APPEND\|O_CREATE\|O_SYNC`, mode 0600. `LogDecision(agentName, rawInput, canonical string, outcome string, reason string, policyVersion int)` writes one JSON line. `Close()`. No rotation in beta.1. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/audit/audit_test.go` | `t.TempDir`-based test that writes 3 entries, reads back, asserts order + JSON shape + `O_APPEND` atomicity. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/config_cmd.go` | `dclaw config` cobra command + `config get workspace-root` + `config set workspace-root <path>` subcommands. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/config_cmd_test.go` | Round-trip test: `config set workspace-root /tmp/x` → file exists → `config get workspace-root` → prints `/tmp/x`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/config/file.go` | `ReadConfigFile(stateDir string) (FileConfig, error)` + `WriteConfigFile(stateDir string, cfg FileConfig) error`. Homegrown TOML parser for one shape: `key = "value"`, ignores comments starting with `#`, creates file on first write with mode 0600. `FileConfig{WorkspaceRoot string}`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/config/file_test.go` | Parser round-trip tests, invalid-TOML rejection, missing-file returns zero-value FileConfig. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/store/migrations/0002_workspace_trust.sql` | `ALTER TABLE agents ADD COLUMN workspace_trust_reason TEXT;`. Down migration: `ALTER TABLE agents DROP COLUMN workspace_trust_reason;` (SQLite 3.35+). |

**Modified files:**

| File | Change |
|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/lifecycle.go` | `AgentCreate` (line 39): after input validation (lines 40-49), before `docker.CreateAgent` (line 57), resolve policy from `config.FileConfig.WorkspaceRoot` + built-in denylist. If `req.WorkspaceTrustReason == ""`, call `paths.Policy.Validate(req.Workspace)` — on `ErrWorkspaceForbidden`, log audit decision `outcome=forbidden` and return `fmt.Errorf("%w: ...", paths.ErrWorkspaceForbidden)` with policy details. If trust is set, policy `AllowTrust=true`, validator bypasses the denylist check but still runs `Clean`/`Rel` invariants — log audit decision `outcome=trust`. On success, call `paths.OpenSafe(canonical)`, defer `f.Close()`, pass `canonicalFromFd` (not `req.Workspace`) as `spec.Workspace`. `AgentStart` (line 187) is untouched — grandfather rule, see §9. `NewLifecycle` gains new params: `policy paths.Policy`, `audit *audit.Logger`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/protocol/messages.go` | Add `ErrWorkspaceForbidden = -32007` to the error-code block (after `ErrChannelNotReady = -32006` at line 47). Add `WorkspaceTrustReason string ` + "`json:\"workspace_trust_reason,omitempty\"`" + ` to `AgentCreateParams` (its definition lives further down; locate and extend). Add same field to `Agent` struct (line 76) to surface in `agent describe` / `agent get -o json`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/router.go` | Rewrite `mapError` (line 347-362) from `strings.Contains` ladder to `errors.Is` switch. Add first-case `errors.Is(err, paths.ErrWorkspaceForbidden)` → `&protocol.RPCError{Code: protocol.ErrWorkspaceForbidden, Message: err.Error(), Data: map[string]any{"error": "workspace_forbidden", "allow_root": policy.AllowRoot, "resolved": canonical}}`. Preserve existing `not found`/`already exists`/`docker` mappings but switch them to sentinels: `errors.Is(err, store.ErrNotFound)`, `errors.Is(err, store.ErrNameTaken)`, `errors.Is(err, sandbox.ErrDockerFailure)`. Accepts that this requires adding those sentinels in their respective packages — included in PR-C scope. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/store/repo.go` | Add `var ErrNotFound = errors.New("agent not found")`, `var ErrNameTaken = errors.New("agent name already taken")`. Change `GetAgent` to `return AgentRecord{}, ErrNotFound` (wrapped). Add new column to `AgentRecord` + SELECT/INSERT/UPDATE SQL: `WorkspaceTrustReason string`. Schema migration `0002_workspace_trust.sql` is the canonical source. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` | Add `var ErrDockerFailure = errors.New("docker operation failed")` and wrap `ContainerCreate`/`ContainerStart` errors with `fmt.Errorf("%w: %v", ErrDockerFailure, err)`. **Belt-and-suspenders in `CreateAgent` (line 77-114):** before building `mount.Mount`, assert `filepath.IsAbs(spec.Workspace)` and reject any `..` segment via `filepath.Clean(spec.Workspace) != spec.Workspace \|\| strings.Contains(spec.Workspace, "..")`. This is an invariant check — policy lives in `internal/paths`; sandbox only verifies internal callers did not bypass it. Fails with `fmt.Errorf("workspace must be absolute clean path, got %q", spec.Workspace)`, not an `ErrWorkspaceForbidden` (because the policy check is upstream; a sandbox-layer failure here is a dclaw bug). |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/agent.go` | `agentCreateCmd`: add `--workspace-trust` flag (string, default ""). Plumb into `protocol.AgentCreateParams{..., WorkspaceTrustReason: agentCreateWorkspaceTrust}`. Flag help: `"explicit operator trust for a workspace path outside the allow-root; requires a non-empty reason string shown in 'agent describe' and the audit log"`. Client-side pre-check: if flag provided, require non-empty — `cobra.RangeArgs` equivalent: custom validator on the flag string. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/cli/exit.go` | Add `WorkspaceForbiddenPayload` struct and `renderWorkspaceForbidden(cmd, msg, data)` helper. `HandleRPCError` (line 51) checks `RPCError.Code == protocol.ErrWorkspaceForbidden` via `errors.As(err, &protocol.RPCError{})` — if matched, render via `renderWorkspaceForbidden`. Pattern mirrors `feature_not_ready` in `docs/phase-2-cli-plan.md:829`. Error text (exact; see §8): includes resolved absolute path, configured root, remediation commands. |
| `/Users/macmini/workspace/agents/atlas/dclaw/cmd/dclawd/main.go` | Construct `audit.New(cfg.StateDir)`, construct `paths.Policy` from `config.ReadConfigFile(cfg.StateDir)`, pass both to `daemon.NewLifecycle`. On startup, scan `repo.ListAgents` for rows whose `Workspace` is outside current `Policy.AllowRoot` and whose `WorkspaceTrustReason` is empty — log `logger.Warn("legacy agent with unverified workspace", ...)` one line each. Does not block startup. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/views/describe.go` | Add "Workspace Trust" line under Workspace, displayed only if `WorkspaceTrustReason != ""`. |

**Validator specification:** §6 below has the full input table. The essentials:

1. `Clean(Abs(raw))` — reject if `raw` was not absolute and no `AllowRoot` is configured, else resolve relative to cwd.
2. Reject literal `\x00`, control chars (`< 0x20` except `\t`), newlines (`\n\r`).
3. `norm.NFC.String(cleaned)` — defeat NFD-vs-NFC allow-root bypass on macOS.
4. `filepath.EvalSymlinks(nfcClean)` — must succeed; any symlink component pointing outside `AllowRoot` fails.
5. `relFromRoot, err := filepath.Rel(allowRoot, canonical); err != nil || strings.HasPrefix(relFromRoot, "..") || filepath.IsAbs(relFromRoot)` — rejects `$HOME/dclaw-evil` when root is `$HOME/dclaw`. This is the key anti-bypass check.
6. Denylist match via `strings.EqualFold(canonical, entry)` for APFS case-insensitivity. Denylist contents: `/`, `/etc`, `/usr`, `/var`, `/bin`, `/sbin`, `/home`, `/root`, `/private/etc`, `/private/var`, `/private/tmp`, `/Volumes`, `/Library`, `/Applications`, `/opt`, plus exactly `$HOME` of the daemon user (distinct from `AllowRoot`, which may be `$HOME/dclaw-workspaces`).

**Test plan:**

- `go test ./internal/paths/...` — 30+ rows for `Policy.Validate`, 3+ rows for `OpenSafe` TOCTOU, all green.
- `go test ./internal/audit/...` — 3 writes + readback + shape check, green.
- `go test ./internal/cli/...` — `config set` + `config get` round-trip via `t.TempDir`.
- `go test ./internal/daemon/...` — add `TestAgentCreateRejectsForbiddenWorkspace`, `TestAgentCreateAcceptsTrustOverride`, `TestAgentCreateWritesAuditEntry`.
- `go test ./internal/store/...` — migration 0002 applies cleanly against the 0001 schema; rollback works.
- `go vet ./...` clean.
- Integration: manually `./bin/dclaw agent create bad --image=dclaw-agent:v0.1 --workspace=/` → exit 1 with structured error matching §8. `./bin/dclaw agent create risky --image=dclaw-agent:v0.1 --workspace=/Users --workspace-trust="legacy test"` → succeeds, audit.log gains a `trust` entry.

**Acceptance criteria:**

1. `go test ./internal/paths/...` passes 30+ rows including NFC, NUL, APFS, symlink, rel-prefix-bypass.
2. Every create call writes exactly one audit.log line (verified by test).
3. Existing agents in state.db with out-of-root workspaces log a warning on daemon startup but do not block.
4. `--workspace-trust ""` (empty reason) is rejected with `"--workspace-trust requires a non-empty reason string"`.
5. `dclaw agent describe <name>` shows `Workspace Trust: <reason>` only when set.
6. `dclaw config set workspace-root /tmp/foo && dclaw config get workspace-root` prints `/tmp/foo`.
7. Router `mapError` has zero remaining `strings.Contains` calls.
8. `cmd/dclawd/main.go` legacy-scan logs exactly N warnings for N pre-beta.1 agents outside the root.

**Rollout risk:** Medium. New migration, new flag on a mutating command, new error code on the wire. Migration is reversible. Validator errs on the side of rejection — users with existing workflows that pass `--workspace=/tmp` must now either configure `/tmp` as allow-root, deny-list-exempt it, or use `--workspace-trust`.

**Rollback:** Rolling back PR-C requires the migration down path (removes `workspace_trust_reason` column) and reverting to `strings.Contains` mapError. Practical rollback: do not ship this PR as a release; squash-merge then revert. Agents created with `--workspace-trust` would lose their reason on rollback — acceptable because trust-set agents are explicit operator decisions and losing the reason is a cosmetic regression, not a security one.

---

### 4.4 PR-D — Smoke-Script Rewrite + Docs

**Goal:** `scripts/smoke-daemon.sh` stops touching `$HOME`. Phase-1 doc corrected. Workspace-root runbook added. `shellcheck` wired into `make lint`.

**Files changed:**

| File | Change |
|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` | Full rewrite of lines 1-49. See §4.4a below for exact top-of-file contents. No `$HOME` reassignment. `SMOKE_STATE=$(mktemp -d -t dclaw-smoke-state-XXXXXXXX)`. Prefix whitelist guard. Trap armed before exports. `export DCLAW_STATE_DIR="$SMOKE_STATE"`. `--state-dir "$SMOKE_STATE"` passed explicitly to every dclaw/dclawd invocation (redundant with the env var — flag wins — but defensive). No `\|\| true` on `rm -rf "${SMOKE_STATE:?refuse empty}"`. Tests 1-13 bodies unchanged except for the SOCKET/STATE_DIR variable rename. |
| `/Users/macmini/workspace/agents/atlas/dclaw/docs/phase-1-plan.md` | Line 540 replaced. See §4.4b below for exact text. |
| `/Users/macmini/workspace/agents/atlas/dclaw/docs/workspace-root.md` | NEW. Runbook. §4.4c below for skeleton. |
| `/Users/macmini/workspace/agents/atlas/dclaw/README.md` | Grep-sweep for `HOME=$(mktemp`, `export HOME`, `rm -rf "$HOME"`, `rm -rf $HOME`. Fix or remove. Add one-liner pointer to `docs/workspace-root.md`. |
| `/Users/macmini/workspace/agents/atlas/dclaw/agent/README.md` | Same grep-sweep. |
| `/Users/macmini/workspace/agents/atlas/dclaw/docs/phase-2-cli-plan.md`, `docs/phase-3-*.md` | Grep-sweep only; fix if hits. No editorial changes otherwise. |
| `/Users/macmini/workspace/agents/atlas/dclaw/Makefile` | `lint` target gains `shellcheck scripts/*.sh agent/*.sh` (append, keep golangci-lint). Fails build if shellcheck finds issues and is installed; no-ops if shellcheck is absent (matches existing golangci-lint behavior). |

**§4.4a — `scripts/smoke-daemon.sh` new top (lines 1-49):**

```bash
#!/usr/bin/env bash
# Phase 3 integration smoke: spin up dclawd, exercise full CRUD, tear down.
# Requires docker reachable on the host and dclaw-agent:v0.1 built (phase 1).
#
# beta.1-paths-hardening: This script NEVER reassigns $HOME and NEVER runs
# rm -rf against any path it did not create via mktemp. All daemon state is
# isolated via DCLAW_STATE_DIR + --state-dir, which is both belt and suspenders.
set -euo pipefail

# TMPDIR can be injected by parent shells, CI runners, or misconfigured
# environments. Require it to be one of the known-safe prefixes, else unset.
if [ -n "${TMPDIR:-}" ]; then
  case "$TMPDIR" in
    /tmp|/tmp/*|/var/folders|/var/folders/*|/private/tmp|/private/tmp/*) ;;
    *) echo "refuse: TMPDIR=$TMPDIR outside expected prefix" >&2; exit 1;;
  esac
fi

SMOKE_STATE=$(mktemp -d -t dclaw-smoke-state-XXXXXXXX)
# Prefix whitelist: mktemp -d without -t can escape; validate belt-and-suspenders.
case "$SMOKE_STATE" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: SMOKE_STATE=$SMOKE_STATE outside expected prefix" >&2; exit 1;;
esac

# Arm the trap BEFORE exporting anything or running any command that could fail.
# ${SMOKE_STATE:?refuse empty} ensures we never expand to `rm -rf ` on an unset var.
cleanup() {
  "$DCLAW_BIN" --daemon-socket "$SOCKET" daemon stop >/dev/null 2>&1 || true
  wipe_smoke_containers
  rm -rf "${SMOKE_STATE:?refuse empty}"
}
trap cleanup EXIT

export DCLAW_STATE_DIR="$SMOKE_STATE"

SMOKE_AGENT_NAMES=(smoke-daemon smoke-dup smoke-chatbot smoke-chatbot13)

wipe_smoke_containers() {
  for name in "${SMOKE_AGENT_NAMES[@]}"; do
    docker rm -f "dclaw-${name}" >/dev/null 2>&1 || true
  done
}

DCLAW_BIN="${DCLAW_BIN:-./bin/dclaw}"
DCLAWD_BIN="${DCLAWD_BIN:-./bin/dclawd}"
SOCKET="$SMOKE_STATE/dclaw.sock"
export DCLAWD_BIN

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

wipe_smoke_containers  # Pre-run cleanup of stale containers from crashed prior runs.
```

Every existing test body (`Test 1: daemon start` onward) gains `--state-dir "$SMOKE_STATE"` redundantly alongside `--daemon-socket "$SOCKET"`. PR-D also adds Test 14: validator rejection, and Test 15: trust override — exact bodies in PR-D commit.

**§4.4b — `docs/phase-1-plan.md:540` replacement:**

Old:
```
10. **Malicious tool call**: ask the agent to `rm -rf /`. Container dies. Host is fine. Workspace on host is fine (rm happens in container rootfs, not the bind mount).
```

New:
```
10. **Malicious tool call**: ask the agent to `rm -rf /`. The container's rootfs is ephemeral and disappears with the container — that much is fine. However, `/workspace` inside the container is a bind mount from the host path the operator passed via `--workspace`; `rm -rf /` descends into `/workspace` and **deletes host files under the bind source**. Paths on the host *outside* the bind source remain untouched. This is why `--workspace` must point at a path the operator is willing to lose — and why beta.1-paths-hardening requires the path to be inside a configured `workspace-root` or to carry explicit `--workspace-trust`. See `docs/workspace-root.md`.
```

**§4.4c — `docs/workspace-root.md` skeleton:**

```
# workspace-root runbook (beta.1-paths-hardening)

## What is workspace-root?
<one paragraph: the allow-root under which `--workspace` paths must live; outside paths require --workspace-trust>

## Setting it the first time
dclaw config set workspace-root ~/dclaw-agents

## Changing it
dclaw config set workspace-root <new-path>
<note on already-running agents: grandfathered, warning at daemon startup>

## What --workspace-trust means
<one paragraph: reason string required, persisted in state.db, surfaced in describe + audit.log>

## Audit log
Location: $DCLAW_STATE_DIR/audit.log
Format: one JSON line per create decision
Fields: ts, agent_name, raw_input, canonical, outcome, reason, policy_version
Rotation: not rotated in beta.1 (follow-up)

## Common errors + fixes
### workspace_forbidden: path /foo not under /home/user/dclaw-agents
### workspace_forbidden: path /etc in denylist
### workspace_forbidden: symlink traversal detected
<each with the exact error text and the two remediation commands>
```

**Test plan:**

- `make lint` passes with shellcheck green on `scripts/smoke-daemon.sh` + `agent/*.sh`.
- `./scripts/smoke-daemon.sh` runs green on the alpha.4.1 baseline (i.e., without PR-C's validator — proves the script itself is not broken).
- `./scripts/smoke-daemon.sh` runs green on the post-PR-C HEAD (proves the path validation path works end-to-end).
- Grep check: `grep -rE 'HOME=\$\(mktemp|export HOME|rm -rf "\$HOME"|rm -rf \$HOME' scripts/ docs/ README.md agent/README.md` → zero hits.

**Acceptance criteria:**

1. Script runs green under `-euo pipefail`.
2. Grep shows zero stale `HOME=` references.
3. `make lint` runs shellcheck and passes.
4. `docs/phase-1-plan.md:540` claim is factually correct.
5. `docs/workspace-root.md` exists and covers: set, change, trust, audit log, three canonical errors.

**Rollout risk:** Low. Script + docs only.

**Rollback:** Revert.

---

## 5. Modified Files Diff Summary

| File | Change type | Net lines | Phase |
|---|---|---|---|
| `internal/config/resolve.go` | new | +90 | PR-A |
| `internal/config/resolve_test.go` | new | +110 | PR-A/B |
| `internal/daemon/config.go` | refactor | -30 | PR-A |
| `internal/cli/root.go` | consolidate | -5 | PR-A |
| `internal/cli/daemon.go` | consolidate | -10 | PR-A |
| `internal/client/rpc.go` | consolidate | -5 | PR-A |
| `cmd/dclaw/main.go` | fix split-brain | -10 | PR-A |
| `internal/cli/root.go` (again) | `--state-dir` flag | +8 | PR-B |
| `internal/config/file.go` | new | +60 | PR-C |
| `internal/config/file_test.go` | new | +80 | PR-C |
| `internal/paths/policy.go` | new | +140 | PR-C |
| `internal/paths/opensafe.go` | new | +80 | PR-C |
| `internal/paths/errors.go` | new | +5 | PR-C |
| `internal/paths/policy_test.go` | new | +250 | PR-C |
| `internal/paths/opensafe_test.go` | new | +60 | PR-C |
| `internal/audit/audit.go` | new | +70 | PR-C |
| `internal/audit/audit_test.go` | new | +50 | PR-C |
| `internal/cli/config_cmd.go` | new | +80 | PR-C |
| `internal/cli/config_cmd_test.go` | new | +60 | PR-C |
| `internal/protocol/messages.go` | add code + field | +10 | PR-C |
| `internal/daemon/router.go` | mapError rewrite | ~0 net | PR-C |
| `internal/daemon/lifecycle.go` | wire validator | +40 | PR-C |
| `internal/store/repo.go` | new column + sentinels | +25 | PR-C |
| `internal/store/migrations/0002_workspace_trust.sql` | new | +10 | PR-C |
| `internal/sandbox/docker.go` | invariant + sentinel | +20 | PR-C |
| `internal/cli/agent.go` | `--workspace-trust` flag | +15 | PR-C |
| `internal/cli/exit.go` | structured renderer | +45 | PR-C |
| `cmd/dclawd/main.go` | wire audit + policy + legacy scan | +25 | PR-C |
| `internal/tui/views/describe.go` | show trust line | +5 | PR-C |
| `scripts/smoke-daemon.sh` | full rewrite | ~0 net | PR-D |
| `docs/phase-1-plan.md` | line 540 fix | +5 | PR-D |
| `docs/workspace-root.md` | new | +100 | PR-D |
| `Makefile` | `lint` target | +3 | PR-D |

**Total estimated diff: ~+1200 lines across 4 PRs** (target ~150 + ~100 + ~400 + ~200 = 850; actual is larger due to extensive table-driven tests; acceptable).

---

## 6. Validator Specification

`Policy.Validate(raw string) (canonical string, err error)` returns `canonical` = absolute, clean, NFC-normalized, symlink-resolved path. Exhaustive table (at least these rows must exist in `internal/paths/policy_test.go`):

Assume `Policy{AllowRoot: "/Users/alice/dclaw-agents", Denylist: [default-macos-list], AllowTrust: false}` unless noted.

| # | Input | Expected | Rationale |
|---|---|---|---|
| 1 | `""` | error: "workspace path empty" | guard |
| 2 | `/Users/alice/dclaw-agents/p1` | canonical matches; pass | happy path |
| 3 | `/users/alice/dclaw-agents/p1` | pass after NFC+EqualFold on APFS; normalized canonical | APFS case-insensitivity |
| 4 | `/Users/alice/dclaw-agents/../dclaw-agents/p1` | pass; clean normalizes | Clean |
| 5 | `/Users/alice/dclaw-agents/../etc` | forbidden | Rel check catches escape |
| 6 | `/Users/alice/dclaw-agents-evil` | forbidden | allow-root-prefix-bypass: strings.HasPrefix would pass; Rel catches it |
| 7 | `/etc` | forbidden (denylist) | critical system path |
| 8 | `/ETC` | forbidden (EqualFold) | APFS denylist bypass |
| 9 | `/private/etc` | forbidden | macOS canonical /etc |
| 10 | `/private/var` | forbidden | |
| 11 | `/private/tmp` | forbidden | |
| 12 | `/Volumes/External` | forbidden | |
| 13 | `/Library/Preferences` | forbidden | |
| 14 | `/Applications` | forbidden | |
| 15 | `/opt/homebrew` | forbidden | |
| 16 | `/` | forbidden | highest blast-radius |
| 17 | `/Users/alice` (= $HOME) | forbidden | denylist adds $HOME of daemon user |
| 18 | `/Users/alice/.ssh` | forbidden | under $HOME, not under allow-root; Rel fails |
| 19 | `/Users/alice/dclaw-agents` (the root itself) | pass | root is its own valid workspace |
| 20 | `workspace-p1` (relative) | forbidden unless config supplies cwd anchor | relative paths are explicit error for now |
| 21 | `/Users/alice/dclaw-agents/p\x001` | forbidden: NUL byte | injection defense |
| 22 | `/Users/alice/dclaw-agents/p\n1` | forbidden: newline | audit-log poisoning defense |
| 23 | `/Users/alice/dclaw-agents/p\t1` | pass (tab is allowed) | tabs in paths are legal on macOS |
| 24 | `/Users/alice/dclaw-agents/café` (NFC) | pass | Unicode normalization OK |
| 25 | `/Users/alice/dclaw-agents/cafe\u0301` (NFD) | pass after NFC normalization; canonical differs from input | NFC→same bytes as row 24 |
| 26 | Symlink: `/Users/alice/dclaw-agents/p1` → `/etc` (via `t.TempDir` + `os.Symlink`) | forbidden after EvalSymlinks | symlink-out-of-root |
| 27 | Symlink: `/Users/alice/dclaw-agents/p1` → `/Users/alice/dclaw-agents/p2` | pass if p2 resolves inside root | intra-root symlinks OK |
| 28 | With `AllowTrust=true` and `Denylist=[/etc]`: `/etc` | forbidden (denylist is absolute) | trust does not bypass denylist |
| 29 | With `AllowTrust=true`: `/Users/alice/elsewhere` | pass | trust bypasses Rel check |
| 30 | Trust with invalid reason (handled at CLI, not validator) | n/a to validator | separation of concerns |
| 31 | Path longer than `_PC_PATH_MAX` (4096 on darwin) | forbidden: "path too long" | platform limit |
| 32 | Path ending in space `/Users/alice/dclaw-agents/p1 ` | pass (space in dir is legal) but Clean preserves | POSIX permits |

Every row in `policy_test.go` asserts the expected outcome plus the expected canonical string (for pass rows).

---

## 7. Audit Log Specification

**Location:** `$DCLAW_STATE_DIR/audit.log` (follows the resolved state-dir, so `dclawd --state-dir /tmp/x` writes to `/tmp/x/audit.log`).

**Open mode:** `O_APPEND | O_CREATE | O_SYNC`, permission `0600`. `O_SYNC` ensures each write is on disk before the syscall returns — the audit log survives kernel panic between write and fsync, at a ~1ms/write cost. Acceptable because audit writes happen only on `agent create`, which is already a multi-hundred-ms operation.

**Record format:** one JSON object per line (newline-delimited JSON / NDJSON). Example:

```
{"ts":"2026-04-19T14:03:22.104Z","agent_name":"alice","raw_input":"/Users/hatef/projects/alice-ws","canonical":"/Users/hatef/projects/alice-ws","outcome":"pass","reason":"","policy_version":1}
{"ts":"2026-04-19T14:05:11.918Z","agent_name":"risky","raw_input":"/etc","canonical":"/etc","outcome":"forbidden","reason":"","policy_version":1}
{"ts":"2026-04-19T14:08:02.412Z","agent_name":"legacy-migrate","raw_input":"/Users/hatef/old-workspace","canonical":"/Users/hatef/old-workspace","outcome":"trust","reason":"migrating from v0.2 workspace layout","policy_version":1}
```

**Fields:**

- `ts`: RFC3339 UTC with millisecond precision
- `agent_name`: the `--name` argument
- `raw_input`: the `--workspace` string as passed (unvalidated)
- `canonical`: the path returned by `Policy.Validate` when `outcome=pass|trust`; same as `raw_input` when `outcome=forbidden` and validation failed before canonicalization
- `outcome`: `"pass" | "forbidden" | "trust"`
- `reason`: empty unless `outcome=trust`, in which case it's the `--workspace-trust` reason
- `policy_version`: integer, incremented any time we change denylist semantics (beta.1 ships `1`)

**Rotation:** none in beta.1. File grows unbounded. Acceptable because one line per agent-create and typical fleets are < 100 agents. Audit-log rotation deferred to follow-up.

**Retention:** keep forever in beta.1.

**Concurrency:** single `*os.File` held by the daemon process. `O_APPEND` guarantees atomic appends for writes < `PIPE_BUF` (typically 4096 bytes) on POSIX. Audit records are well under that limit. No file locking needed.

---

## 8. Error Contract: `workspace_forbidden`

**Wire shape** (`protocol.RPCError` with Code = -32007):

```json
{
  "jsonrpc": "2.0",
  "id": 42,
  "error": {
    "code": -32007,
    "message": "workspace path /etc is forbidden by policy (denied by system-path denylist)",
    "data": {
      "error": "workspace_forbidden",
      "allow_root": "/Users/hatef/dclaw-agents",
      "resolved": "/etc",
      "reason": "denied by system-path denylist"
    }
  }
}
```

**CLI text (exact):**

```
error: workspace path /etc is forbidden by policy

  Resolved path:         /etc
  Configured allow-root: /Users/hatef/dclaw-agents
  Reason:                denied by system-path denylist

To fix, do one of the following:

  1. Use a path inside the allow-root:
       dclaw agent create <name> --workspace /Users/hatef/dclaw-agents/<subdir> --image=<image>

  2. Change the allow-root:
       dclaw config set workspace-root <new-path>

  3. Override with explicit operator trust (persisted in state.db, shown in
     'agent describe' and written to $DCLAW_STATE_DIR/audit.log):
       dclaw agent create <name> --workspace /etc \
         --workspace-trust "reason string required" --image=<image>

See docs/workspace-root.md for details.
```

**Exit code:** 65 (`ExitDataErr`). Rationale: the user input was semantically invalid (data error), not a syntax error (which would be 64) and not a fatal daemon bug (which would be 70).

**JSON output mode** (`--output json`):

```json
{
  "error": "workspace_forbidden",
  "message": "workspace path /etc is forbidden by policy",
  "exit_code": 65,
  "allow_root": "/Users/hatef/dclaw-agents",
  "resolved": "/etc",
  "reason": "denied by system-path denylist",
  "remediations": [
    {"kind": "use_inside_root", "command": "dclaw agent create ... --workspace <path-inside-root>"},
    {"kind": "change_root", "command": "dclaw config set workspace-root <new-path>"},
    {"kind": "trust_override", "command": "dclaw agent create ... --workspace-trust \"<reason>\""}
  ]
}
```

Mirrors the `feature_not_ready` structure (`docs/phase-2-cli-plan.md:829`).

---

## 9. Migration / Backwards Compatibility

**Schema migration `0002_workspace_trust.sql`:**

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE agents ADD COLUMN workspace_trust_reason TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agents DROP COLUMN workspace_trust_reason;
-- +goose StatementEnd
```

SQLite 3.35.0 (2021) introduced `ALTER TABLE ... DROP COLUMN`. Our minimum is `mattn/go-sqlite3` which is currently at a version that ships SQLite ≥ 3.42 — safe.

Migration number `0002_` is unambiguous because only `0001_initial.sql` exists on disk at `76405ac`. The task brief raised the possibility of a Phase-0 `0003_` collision from the pre-wipe lost work, but `76405ac` is authoritative — that work was never pushed. No renumbering needed. Open question §11 Q1 documents the residual risk.

**Grandfathering rule for existing agents:**

1. On `dclawd` startup, after `repo.Migrate`, scan `repo.ListAgents`. For each `a.Workspace` that is non-empty:
   - Resolve current `Policy` from `config.ReadConfigFile`.
   - If `a.WorkspaceTrustReason != ""`, skip (already explicitly trusted).
   - Run `Policy.Validate(a.Workspace)`. On `ErrWorkspaceForbidden`, log `logger.Warn("legacy agent with unverified workspace path", "name", a.Name, "workspace", a.Workspace, "reason", err.Error())`. Do NOT block startup. Do NOT modify state.
2. `AgentStart` is not validated (task brief decision; validation is on `AgentCreate` only). A legacy agent can be started indefinitely under its original path. The warning exists only to surface the hazard to the operator, not to force a migration.
3. `AgentUpdate` does NOT accept a workspace change (v0.3 constraint — image-only update), so there is no re-validation entry point other than delete + recreate.

**`config.toml` creation semantics:**

- `config.ReadConfigFile(stateDir)` returns zero-value `FileConfig{}` if `$STATE_DIR/config.toml` does not exist. No error.
- `dclaw config set workspace-root <path>` creates the file with mode 0600 if absent, writes exactly one line: `workspace-root = "/path"`. On subsequent `set`, reads, mutates the in-memory struct, rewrites the full file (no append).
- First `agent create` without `workspace-root` configured (zero-value `FileConfig.WorkspaceRoot`) and without `--workspace-trust` returns the `workspace_forbidden` error with the §8 exact text, in which the `Configured allow-root` line reads `(not configured — run 'dclaw config set workspace-root <path>')`.

**Protocol compatibility:**

- `ErrWorkspaceForbidden = -32007` is a new error code in the dclaw custom range (-32001 through -32099). Existing clients on alpha.4.1 do not know this code and will receive it as a generic `*protocol.RPCError`. Forward-compatible.
- `AgentCreateParams.WorkspaceTrustReason` is a new optional field. Existing alpha.4.1 CLIs do not set it; new daemons treat unset as "no trust". Backward-compatible.
- `Agent.WorkspaceTrustReason` is a new optional output field. Existing alpha.4.1 CLIs ignore it; new CLIs render it.

No breaking wire changes. Protocol version does not bump.

---

## 10. Test Strategy

### Automated unit + integration tests

| Test | Location | Exercises | PR |
|---|---|---|---|
| `TestResolvePrecedence` (6+ rows) | `internal/config/resolve_test.go` | flag > env > default | A |
| `TestResolveFlagEnvRoundTrip` | `internal/config/resolve_test.go` | dclaw + dclawd agree on paths | B |
| `TestReadConfigFileMissing` | `internal/config/file_test.go` | zero-value return | C |
| `TestReadConfigFileRoundTrip` | `internal/config/file_test.go` | write → read → write → read | C |
| `TestReadConfigFileInvalidTOML` | `internal/config/file_test.go` | parser rejects garbage | C |
| `TestPolicyValidate` (30+ rows) | `internal/paths/policy_test.go` | see §6 | C |
| `TestOpenSafeTOCTOU` | `internal/paths/opensafe_test.go` | symlink created between Validate and open is caught | C |
| `TestOpenSafeReturnsFd` | `internal/paths/opensafe_test.go` | fd is open + valid | C |
| `TestAuditLogAppendOnly` | `internal/audit/audit_test.go` | three writes, readback order | C |
| `TestAuditLogJSONShape` | `internal/audit/audit_test.go` | every required field present | C |
| `TestAgentCreateRejectsForbiddenWorkspace` | `internal/daemon/lifecycle_test.go` | happy-path rejection | C |
| `TestAgentCreateAcceptsTrustOverride` | `internal/daemon/lifecycle_test.go` | trust bypasses denylist | C |
| `TestAgentCreateWritesAuditEntry` | `internal/daemon/lifecycle_test.go` | one audit line per create | C |
| `TestConfigSetRoundTrip` | `internal/cli/config_cmd_test.go` | set + get through CLI | C |
| `TestMigrate0002` | `internal/store/repo_test.go` | ALTER TABLE applies | C |
| `TestRouterMapErrorWorkspaceForbidden` | `internal/daemon/router_test.go` | error → -32007 wire mapping | C |
| All existing tests | various | regression | A/B/C/D |

### Integration tests via smoke-daemon.sh

- Test 14 (new): `dclaw agent create forbidden --workspace=/etc` → exit 65, stderr contains `workspace_forbidden`.
- Test 15 (new): `dclaw agent create trusted --workspace=/tmp/smoke-trusted --workspace-trust "test"` → exit 0, `dclaw agent describe trusted` shows `Workspace Trust: test`.
- Test 16 (new): After Test 15, `cat $DCLAW_STATE_DIR/audit.log | tail -1 | jq -e '.outcome == "trust"'` → exit 0.
- Tests 1-13 regression under the new state-dir isolation.

### Manual verification matrix

| Check | Pre-PR | Post-PR |
|---|---|---|
| Create agent with `--workspace=/etc` | succeeds (hole) | fails with §8 text |
| Create agent with `--workspace=/tmp/foo`, config set to `/tmp` | succeeds | succeeds, canonical logged |
| Create agent with `--workspace-trust=""` (empty) | n/a | rejected at CLI |
| Daemon startup with legacy out-of-root agent | silent | logs one warning per legacy |
| `dclaw config get workspace-root` before set | n/a | prints "(not configured)" |
| Run smoke-daemon.sh twice in a row | works but touches $HOME | works, no $HOME touch |
| TUI bare invocation on Linux with XDG_RUNTIME_DIR set | split-brain: hits wrong socket | hits correct socket |

---

## 11. Open Questions

### Q1: Migration number — `0002_` vs `0003_`?

**Decided: `0002_workspace_trust.sql`.**

The pre-wipe beta.1 Phase-0 used `0003_` (per `WORKLOG.md` 2026-04-19), but that commit (`d84554d`) was never pushed and the origin tip (`76405ac`) has only `0001_initial.sql`. Since the lost work is unrecoverable, `0002_` is the next free number on the authoritative baseline. If a future developer re-derives the lost Phase-0 work, they would renumber to `0003_` and bump this plan's migration to `0004_`.

**Residual risk:** if anyone has a local SQLite from the wiped machine with `_migrations` rows for `0002_` and `0003_` out of order — no one does (the machine was wiped) — but operators with backups taken pre-wipe may have inconsistent migration rows. Acceptable: a backup restore onto beta.1-paths-hardening HEAD would be a manual operator operation anyway; documenting in `docs/workspace-root.md` that backups taken between 2026-04-17 and 2026-04-18 may need manual `DELETE FROM _migrations WHERE version > '0001'` before running migrations.

### Q2: TOML library — existing dep, new dep, or homegrown?

**Decided: homegrown ~40-line parser in `internal/config/file.go`.**

Reasoning:

- `config.toml` in beta.1 has exactly one key: `workspace-root`. The grammar is `^\s*workspace-root\s*=\s*"([^"]*)"\s*$` plus `#`-comment lines plus blanks. That's under 40 lines of Go with thorough tests.
- Adding `github.com/pelletier/go-toml/v2` is a ~7k-line dependency for one string key — poor cost-benefit in a security-focused PR.
- If `config.toml` grows to 5+ keys in a follow-up, we revisit and pull the library. Migration cost is minimal (replace one function).

Invalid-TOML handling: the parser returns `fmt.Errorf("parse config.toml line %d: %v", lineno, err)` on malformed input. Daemon startup surfaces the error and refuses to start — matches goose migration behavior for corrupt state.

### Q3: `dclaw doctor workspace` subcommand — PR-C or follow-up?

**Decided: follow-up, not in PR-C.**

A `doctor` subcommand that pre-flights a path (running `Policy.Validate` without creating an agent) is a clean affordance — it lets users iterate on their `--workspace` value without polluting audit.log with "forbidden" entries that were never real create attempts.

Not in PR-C because:

- The primary error surface (failed `agent create`) already gives the user the same information.
- Adding it to PR-C expands PR-C from ~400 lines to ~480 lines and adds one more surface area.
- A `doctor` pattern is worth designing properly (covers more than just workspace — daemon reachability, docker daemon, XDG, etc.), so it deserves its own small PR after beta.1 ships.

### Q4: What about `dclaw agent update --workspace`?

**Out of scope.** v0.3 already disallows workspace updates (`internal/daemon/lifecycle.go:AgentUpdate` only handles image/env/labels). No new code path to validate. The rule stays: delete + recreate to change workspace.

### Q5: Should the audit log roll over when the daemon restarts?

**Decided: no roll-over, no new file.** Single continuous `audit.log`. Daemon restarts open with `O_APPEND` which works across process lifetimes. This is the expected pattern for append-only audit logs (mirrors syslog, sudoers). Rotation is a separate concern for a separate PR.

### Q6: Error message for "no workspace root configured" — error or `config.toml` auto-create?

**Decided: hard error.** Matches Hatef's locked-in decision 1 verbatim. `agent create` without `workspace-root` set AND without `--workspace-trust` returns the §8 error with the `(not configured — run 'dclaw config set workspace-root <path>')` message line. Auto-creation of a default is explicit follow-up work, not hidden behavior.

---

## 12. Follow-Ups (Deferred — Not Shipped in beta.1-paths-hardening)

1. **beta.2 sandbox-hardening phase** — caps drop (`CAP_NET_RAW` etc.), seccomp profile, user-namespace remapping (`--userns-remap`), `ReadonlyRootfs: true` with tmpfs overlays, non-root UID in `dclaw-agent:v0.1`, `PidsLimit`, refuse `-v /var/run/docker.sock`. Reason: orthogonal attack surface; validation is a prerequisite because it bounds the file-system blast radius regardless of container-escape posture.
2. **Easier setup for workspace-root** — e.g., `dclaw init` that interactively prompts, or auto-create `$HOME/dclaw-agents` on first unconfigured `agent create`. Reason: explicit error-first is safer for beta.1; ergonomics pass comes after shipping.
3. **Full TOML config file** — socket, state-dir, log level, workspace-root all in `config.toml`, with `flag > env > config > default`. Reason: beta.1 only needs one config key; full factoring is too much surface area for a security-focused phase.
4. **XDG-aware state split on Linux** — `$XDG_DATA_HOME` / `$XDG_CONFIG_HOME` / `$XDG_STATE_HOME`. Reason: beta.1 keeps state co-located under `$STATE_DIR` for cross-platform uniformity; XDG split is a Linux-only polish pass.
5. **Windows denylist** via `runtime.GOOS == "windows"` switch. Reason: Windows is not a current target; no Windows CI.
6. **Audit log rotation** — size-based or time-based. Reason: one line per `agent create` grows unbounded but at realistic rates (< 100 agents), unbounded is fine for beta.1.
7. **`dclaw doctor workspace <path>`** subcommand — pre-flight check without creating. Reason: see §11 Q3.
8. **TOCTOU hardening beyond `OpenSafe`** — e.g., hold the fd open during `docker.CreateAgent` and pass `/proc/self/fd/N` instead of the canonical path to Docker. Reason: `OpenSafe` is a strong mitigation; further hardening is a separate PR.
9. **Audit log signing** — HMAC-SHA256 chain per record. Reason: tamper evidence is useful but not a beta.1 must-have; the `O_SYNC` guarantee already covers crash integrity.

---

## 13. Acceptance Checklist

Hatef ticks these off before tagging.

- [ ] PR-A merges clean on top of `76405ac`; CI green.
- [ ] PR-A: `os.UserHomeDir` grep finds exactly one call in `internal/config/resolve.go`; nowhere else in production code (tests excepted).
- [ ] PR-A: socket split-brain demonstrably fixed: on Linux with `XDG_RUNTIME_DIR=/tmp/xdg-test`, `./bin/dclaw daemon start && ls $XDG_RUNTIME_DIR/dclaw.sock` shows a socket; plain `./bin/dclaw` TUI reaches the same socket.
- [ ] PR-B: `./bin/dclaw --state-dir /tmp/x daemon start && ./bin/dclaw --state-dir /tmp/x daemon status` round-trips.
- [ ] PR-B: `DCLAW_STATE_DIR=/tmp/x ./bin/dclaw daemon status` connects to the daemon started via `--state-dir /tmp/x`.
- [ ] PR-C: `go test ./internal/paths/...` passes with ≥ 30 rows in the validator table, including NFC, NUL, APFS, symlink, rel-prefix-bypass (`$HOME/dclaw-evil` when root is `$HOME/dclaw`).
- [ ] PR-C: `$DCLAW_STATE_DIR/audit.log` gains exactly one line per `agent create` attempt (pass, forbidden, or trust), verified by automated test.
- [ ] PR-C: `./bin/dclaw agent create bad --workspace=/etc --image=dclaw-agent:v0.1` exits 65 and stderr contains the §8 text verbatim (including both remediation commands).
- [ ] PR-C: `./bin/dclaw agent create risky --workspace=/etc --workspace-trust "known risk" --image=dclaw-agent:v0.1` succeeds; `dclaw agent describe risky` shows `Workspace Trust: known risk`; audit.log has one `outcome=trust` entry.
- [ ] PR-C: Schema migration `0002_workspace_trust.sql` applies and rolls back cleanly.
- [ ] PR-C: Legacy-scan on daemon startup logs one warning per pre-beta.1 agent outside the current allow-root; does not block startup.
- [ ] PR-C: `internal/daemon/router.go:mapError` contains zero `strings.Contains` calls.
- [ ] PR-D: `./scripts/smoke-daemon.sh` passes green on the alpha.4.1 baseline (proves the rewrite is not broken independent of validator changes).
- [ ] PR-D: `./scripts/smoke-daemon.sh` passes green on post-PR-C HEAD (proves end-to-end).
- [ ] PR-D: `grep -rE 'HOME=\$\(mktemp|export HOME|rm -rf "\$HOME"|rm -rf \$HOME' scripts/ docs/ README.md agent/README.md` returns zero matches.
- [ ] PR-D: `make lint` runs `shellcheck scripts/*.sh agent/*.sh` and exits green.
- [ ] PR-D: `docs/phase-1-plan.md:540` reflects accurate bind-mount semantics.
- [ ] PR-D: `docs/workspace-root.md` exists and documents set / change / trust / audit log / three canonical errors.
- [ ] All four PRs squash-merged to `main` in the order A → B → C → D.
- [ ] `git tag -a v0.3.0-beta.1-paths-hardening -m "Phase 3 beta.1 paths-hardening: --workspace validation + state-dir consolidation"`.
- [ ] `git push origin main v0.3.0-beta.1-paths-hardening`.
- [ ] `WORKLOG.md` entry added documenting the ship.

---

## 14. References

- `WORKLOG.md` 2026-04-19 session — incident recap, decisions, 28 findings summary.
- `docs/phase-3-alpha4-plan.md` — style and structure reference.
- `docs/phase-3-alpha3-plan.md` — style cross-check.
- `docs/phase-1-plan.md:540` — the inaccurate claim this phase corrects.
- `docs/phase-2-cli-plan.md:805-829` — `feature_not_ready` precedent mirrored by the `workspace_forbidden` renderer.
- `internal/daemon/router.go:347` — the fragile `strings.Contains` ladder this phase replaces.
- `internal/sandbox/docker.go:77-114` — the bind-mount site.
- `internal/cli/agent.go:334` — the unvalidated `--workspace` flag.
- `internal/daemon/config.go:69-78` — `DefaultSocketPath`, moved to `internal/config` in PR-A.
- `cmd/dclaw/main.go:61-70` — the split-brain site.
