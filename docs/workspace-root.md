# workspace-root runbook (beta.1-paths-hardening)

Operational guide for the workspace-path validator shipped in beta.1-paths-hardening. Covers what `workspace-root` is, how to set/change it, the `--workspace-trust` escape hatch, the audit log, canonical errors with fixes, and backup-restore notes for pre-wipe SQLite snapshots.

## What is workspace-root?

`workspace-root` is the allow-root under which any `--workspace` path passed to `dclaw agent create` must live. It is configured per-operator via `dclaw config set workspace-root <path>` and persisted to `$DCLAW_STATE_DIR/config.toml`. At create-time the daemon runs `Policy.Validate` over the raw `--workspace` string: the path is cleaned, NFC-normalized, symlink-resolved (via `filepath.EvalSymlinks`), then checked two ways — (1) a hard-coded denylist of system paths (`/`, `/etc`, `/private/etc`, `/private/var`, `/Library`, `/Applications`, `/opt/homebrew`, the daemon user's `$HOME`, `/Volumes/External`, and the usual macOS suspects); (2) a `filepath.Rel`-based containment check against the configured `workspace-root`, which closes the sibling-prefix bypass — so `/Users/hatef/dclaw-agents-evil` does NOT pass just because the allow-root is `/Users/hatef/dclaw-agents`. If both checks fail, the daemon rejects the create with `workspace_forbidden` (exit 65, wire error `-32007`). The `--workspace-trust "<reason>"` flag is the documented escape hatch: it bypasses the allow-root check (but NOT the denylist), records the operator-supplied reason in `state.db`, surfaces it in `dclaw agent describe`, and writes an `outcome=trust` line to the audit log.

### Docker socket

The Docker control socket is explicitly denylisted across all three common locations. Mounting the Docker socket into a container is equivalent to granting root on the host — the agent could start privileged containers, mount any host path, or control the Docker daemon directly — so the validator rejects any `--workspace` that resolves to one of these paths regardless of the configured `workspace-root`, and `--workspace-trust` does NOT bypass the rejection (denylist is absolute):

- `/var/run/docker.sock` — canonical Linux location (managed by the `docker` package).
- `/run/docker.sock` — systemd-managed Linux distributions (often a symlink into `/var/run`).
- `/Users/<user>/Library/Containers/com.docker.docker/Data/docker-raw.sock` — Docker Desktop on macOS. Because the `<user>` component varies per operator, this path is matched by a substring check in `Policy.Validate` (the two invariant fragments `"/Library/Containers/com.docker.docker/"` and suffix `"docker-raw.sock"`) rather than a literal denylist entry. Any canonical path that matches both fragments is rejected with the reason `"is the Docker Desktop control socket"`.

If a legitimate automation workflow truly needs Docker-in-Docker, the correct path is to run the daemon under a separate UID with its own socket, NOT to bind the host `docker.sock` into an agent container.

### Custom image compatibility

Operators who ship their own `--image=...` images alongside or instead of `dclaw-agent:v0.1` must ensure the image is compatible with the beta.2 container posture. Three rules:

1. **`USER` directive sets uid 1000.** The daemon enforces `Config.User = "1000:1000"` at `ContainerCreate` time regardless of the image's own directive, but aligning the Dockerfile-time `USER` makes build-time file ownership match runtime so files created under `/workspace` are readable/writable without a runtime `chown`.
2. **All runtime writes go to `/tmp`, `/run`, or `/workspace`.** The rootfs is `ReadonlyRootfs: true` in beta.2. `/tmp` and `/run` are tmpfs overlays (rw, `noexec,nosuid,nodev`); `/workspace` is the bind-mounted host directory. Any image that writes to `/etc`, `/opt`, `/usr`, or elsewhere on the rootfs at runtime will hit `EROFS`.
3. **No runtime dependency on dropped capabilities.** The daemon drops `ALL` capabilities and applies `no-new-privileges` + the default seccomp profile. Images that call `mknod`, `unshare(CLONE_NEWUSER)`, raw `ptrace`, `setuid`-bit binaries, or similar privileged syscalls will fail with `EPERM` at runtime. pi-mono's happy path does not hit any of these.

## config.toml schema

`$DCLAW_STATE_DIR/config.toml` is the canonical operator-config file. Beta.2.5 graduated the parser to [pelletier/go-toml/v2](https://pkg.go.dev/github.com/pelletier/go-toml/v2) (replacing beta.1's homegrown one-key regex reader) and split the schema into a top-level key plus two sub-tables. Every field is optional; any unset field falls back to its default. The full shape:

```toml
# Top-level: the workspace allow-root, written by `dclaw init` or
# `dclaw config set workspace-root <path>`. Empty / missing means
# "not configured" — every `agent create` without --workspace-trust
# is rejected until you set it.
workspace-root = "/Users/alice/dclaw-agents"

# [audit] tunes the audit-log rotation introduced in beta.2.3. Both
# fields default to the audit package constants (10 MB / 5 files);
# omit the table entirely to inherit those.
[audit]
max-size-bytes = 10485760   # rotation threshold in bytes (default 10 MB)
max-files      = 5          # total files retained: audit.log + .1 .. .{N-1}

# [daemon] is declared for future use. Beta.2.5 reads the table but
# does not wire its fields to consumers — flags (--socket, --log-level)
# still win. Pre-staging a value here today is a no-op; it activates
# when the wiring lands.
[daemon]
socket    = "/run/dclaw/dclaw.sock"
log-level = "debug"
```

Behavior summary:

- **Missing config.toml** → every field zero-valued (the pre-init state).
- **Single-key file** (just `workspace-root = "..."`, the beta.1+ shape) → workspace-root surfaces, sub-tables are zero. Existing operator configs upgrade transparently — no rewrite required.
- **Unset `[audit].max-size-bytes` or `max-files`** → defaults from `internal/audit` (10 MB / 5 files in beta.2.3) apply. The daemon logs `audit log configured max_size=<N> max_files=<N>` at startup so operators see what's effective.
- **`[daemon]` fields** → declared, not wired. Setting them in beta.2.5 is forward-compatible scaffolding.

Re-write the file via `dclaw config set workspace-root <path>` (top-level only) or by hand-editing — the file is plain TOML and any text editor works. `dclaw config set` performs read-modify-write through `go-toml/v2`, so it preserves any `[audit]` / `[daemon]` keys you added by hand; comments, whitespace, and key ordering are NOT preserved across a `set` (the file is re-emitted by the marshaler).

## Setting it the first time

The recommended path is the `dclaw init` first-run wizard:

```bash
dclaw init                       # interactive: defaults to $HOME/dclaw
dclaw init --yes                 # non-interactive: accept the default
dclaw init --workspace-root ~/dclaw-agents   # explicit path
```

`dclaw init` resolves the chosen path, runs it through the same denylist that protects `agent create` (refuses `/etc`, `/var`, the Docker socket, etc.), creates the directory at mode 0700 if missing, and writes `workspace-root = "..."` to `$DCLAW_STATE_DIR/config.toml`. Re-running `dclaw init` with `workspace-root` already set is a no-op — it prints the current value and exits 0 (idempotent).

The explicit alternative remains supported and is appropriate when scripting or pointing at a path that already exists:

```bash
dclaw config set workspace-root ~/dclaw-agents
```

Writes `workspace-root = "/Users/<you>/dclaw-agents"` to `$DCLAW_STATE_DIR/config.toml` (mode 0600). The daemon picks it up at next `agent create`. Before the first `config set` (or `dclaw init`), `workspace-root` is zero-value and every `agent create` without `--workspace-trust` returns `workspace_forbidden` with `Configured allow-root: (not configured — run 'dclaw config set workspace-root <path>')`.

## Changing it

```bash
dclaw config set workspace-root /new/path
```

Rewrites `config.toml` in place. **Grandfathering:** agents already in `state.db` with workspaces outside the new root are NOT deleted or re-validated. They keep running under their original paths. On daemon startup the reconciler scans all agents, and for each whose `workspace` fails the current policy (and whose `workspace_trust_reason` is empty), it emits one `WARN` log line of the form:

```
legacy agent with unverified workspace path name=<agent> workspace=<path> reason=<policy error>
```

This is a surface-to-operator warning only; no action is taken. To bring a legacy agent into compliance you must `dclaw agent delete <name>` and `dclaw agent create <name> ...` with a compliant path (or the `--workspace-trust` escape).

## What `--workspace-trust` means

Pass `--workspace-trust "<non-empty reason string>"` to `dclaw agent create` to deliberately override the allow-root check for one specific agent. The reason string is mandatory (empty strings are rejected at the CLI layer), persisted in `state.db` as `agents.workspace_trust_reason`, echoed by `dclaw agent describe <name>` on a `Workspace Trust: <reason>` line, and written to `$DCLAW_STATE_DIR/audit.log` as an `outcome=trust` NDJSON record. Trust does NOT bypass the denylist — `--workspace-trust "I know what I'm doing" --workspace=/etc` is still rejected. The intent is that operators can explain in-band why a given agent is allowed to run outside the configured root (e.g., a one-off path, a legacy layout being migrated, an external volume), and auditors can reconstruct those decisions from `audit.log` after the fact.

## Audit log

- **Location:** `$DCLAW_STATE_DIR/audit.log`
- **Format:** NDJSON, one JSON object per line
- **Open mode:** `O_APPEND | O_CREATE | O_SYNC`, permission `0600`
- **Fields** (matches plan §7):
  - `ts` — RFC3339 UTC with millisecond precision
  - `agent_name` — the `--name` argument
  - `raw_input` — the `--workspace` string as passed (unvalidated)
  - `canonical` — the path returned by `Policy.Validate` when `outcome` is `pass` or `trust`; same as `raw_input` when validation failed before canonicalization
  - `outcome` — `"pass" | "forbidden" | "trust"`
  - `reason` — empty unless `outcome=="trust"`, in which case it's the `--workspace-trust` reason
  - `policy_version` — integer, incremented when denylist semantics change; beta.1 ships `1`
- **Rotation:** size-based, in-process (added in beta.2.3). The active `audit.log` rotates when the next record would push its size past **10 MB**, after which the daemon retains the **5 most recent files**: `audit.log`, `audit.log.1`, `audit.log.2`, `audit.log.3`, `audit.log.4`. On rotation `audit.log.{N-1}` shifts to `audit.log.{N}` for N from 4 down to 1, the active `audit.log` becomes `audit.log.1`, and a fresh `audit.log` is opened with the same `O_APPEND|O_CREATE|O_SYNC` / 0600. The slot at `audit.log.4` is removed before the rename chain so the oldest cohort is dropped silently — no operator-visible error and no external tooling required. Rotation runs under the same `sync.Mutex` that serializes writes, so callers see one slightly slower `LogDecision` whenever a rotation triggers and otherwise no change; the existing concurrent-write guarantees from beta.2.1 (`TestAuditLogConcurrentWrites` under `-race`) survive intact.
- **Retention:** the 5 most recent files only — older audit history is dropped at rotation time. If long-term retention is required, copy `audit.log*` out of `$DCLAW_STATE_DIR` on a schedule (e.g. daily) before the chain rolls over.

Example line:

```
{"ts":"2026-04-19T14:05:11.918Z","agent_name":"risky","raw_input":"/etc","canonical":"/etc","outcome":"forbidden","reason":"","policy_version":1}
```

## Pre-flight diagnostics

Before debugging a `workspace_forbidden` rejection by hand, run `dclaw doctor` — it prints a pass/fail breakdown of seven checks (config resolution, workspace-root configuration + validity, daemon reachability, docker reachability, agent image presence, audit-log writability) so you can tell at a glance which preflight is the actual problem. Use `dclaw doctor workspace <path>` to dry-run a candidate `--workspace` value through `Policy.Validate` without creating an agent and without writing to `audit.log` — useful for iterating on paths cheaply before committing to `dclaw agent create`.

```bash
dclaw doctor                          # full battery, exits 1 on any FAIL
dclaw doctor -o json                  # structured output for scripts
dclaw doctor workspace ~/dclaw/x      # pre-flight a single path
```

## Common errors + fixes

### `workspace_forbidden: path /etc is in the system denylist`

The path (or its canonical resolution) matches a hard-coded denylist entry. The denylist is absolute and cannot be bypassed — not even with `--workspace-trust`. Fix:

```bash
# Option A: pick a path inside the allow-root
dclaw agent create <name> --workspace ~/dclaw-agents/<subdir> --image=<image>

# Option B: if the path truly must be outside the allow-root, pick a path that is
#           NOT on the denylist (the denylist covers system paths only)
```

### `workspace_forbidden: /Users/hatef/elsewhere is not under allow-root /Users/hatef/dclaw`

The path passes the denylist but is not under the configured `workspace-root`. Fix:

```bash
# Option A: broaden the allow-root to cover the path
dclaw config set workspace-root /Users/hatef

# Option B: use a path that already lives under the current allow-root
dclaw agent create <name> --workspace /Users/hatef/dclaw/<subdir> --image=<image>

# Option C: explicit trust override (persisted in state.db + audit.log)
dclaw agent create <name> --workspace /Users/hatef/elsewhere \
  --workspace-trust "reason string required" --image=<image>
```

### `workspace_forbidden: symlink component escapes allow-root`

`filepath.EvalSymlinks` resolved a component of the path to a target that is no longer inside the allow-root (or is on the denylist). This is a defense against `~/dclaw-agents/trojan -> /etc` trickery. Fix:

```bash
# Inspect the symlinks along the path
ls -la <path>
readlink <symlink-component>

# Then either remove or replace the symlink
rm <symlink-component>
# or point it somewhere inside the allow-root
ln -sf <intended-target-inside-root> <symlink-component>
```

## Backup-restore note

If you are restoring a SQLite snapshot of `$DCLAW_STATE_DIR/state.db` taken between 2026-04-17 and 2026-04-18 (the window covering the pre-wipe Phase-0 migration work that was lost) onto a beta.1-paths-hardening daemon, the `_migrations` table may reference migration versions that never shipped to this branch. Before starting `dclawd` against the restored DB, run:

```sql
DELETE FROM _migrations WHERE version > '0001';
```

Then start `dclawd`; goose will re-apply `0002_workspace_trust.sql` cleanly on top of the restored `0001` state. Backups taken after 2026-04-18 against the authoritative post-wipe baseline (`76405ac` or later) do NOT need this step — the migration history is already consistent with beta.1-paths-hardening. See plan §11 Q1 for background on the renumbering decision.

## Cross-platform notes

dclaw is actively tested on macOS and Linux. Beta.2.6 (Plan §12 #4 + §12 #5) made two platform-portability changes that operators on either OS should be aware of, and added defensive scaffolding for a future Windows port.

### State directory defaults by OS

The state directory holds `state.db`, `audit.log*`, `config.toml`, the daemon socket, and per-agent log dirs. The default location depends on the OS and on whether `$XDG_STATE_HOME` is set. The full ladder, in precedence order, is:

1. `--state-dir <path>` flag on `dclaw` or `dclawd` — wins everywhere.
2. `$DCLAW_STATE_DIR` env var — wins everywhere when set.
3. Platform default (the rest of this section).

| OS | `XDG_STATE_HOME` | `~/.dclaw` exists | Default state-dir |
|---|---|---|---|
| Linux | set, writable | no | `$XDG_STATE_HOME/dclaw` |
| Linux | set, writable | yes | `~/.dclaw` (legacy wins for upgrades) |
| Linux | unset / unwritable | no, but `~/.local/state` exists | `~/.local/state/dclaw` |
| Linux | unset / unwritable | yes | `~/.dclaw` |
| Linux | unset / unwritable | no, no `~/.local/state` | `~/.dclaw` |
| macOS | (any) | (any) | `~/.dclaw` (XDG is not a Darwin convention) |
| Windows (experimental) | (any) | (any) | `~/.dclaw` (no XDG; placeholder) |

Migration: existing Linux installs that have a populated `~/.dclaw` from beta.2.5 or earlier keep using it after upgrading to beta.2.6 — the legacy-wins branch fires on first start. New Linux installs (no prior `~/.dclaw`) land in the XDG path. Operators who want to migrate an existing legacy install onto XDG can `mv ~/.dclaw $XDG_STATE_HOME/dclaw` while `dclawd` is stopped, then start the daemon — both flag-and-env precedence and the lookup ladder will route to the new path.

### Windows experimental support

dclaw does not run on Windows out of the box in beta.2.6, but the codebase now refuses to compile on Windows without the platform-specific Windows-system denylist active. `paths.DefaultDenylist` includes the following entries on Windows builds (`runtime.GOOS == "windows"`):

- `C:\Windows`
- `C:\Program Files`
- `C:\Program Files (x86)`
- `C:\ProgramData`
- `C:\Users\Default`
- `C:\Users\Public`
- `C:\Users\All Users`

The validator's existing case-insensitive `EqualFold` matching applies unchanged. Any future Windows port will inherit the denylist for free; the alternative — a Unix-only denylist with no Windows entries — would let an operator who manages to build dclaw on Windows accidentally bind `C:\Windows` into an agent container, which the validator would not catch. `--workspace-trust` does NOT bypass these entries (denylist is absolute on every OS, see Docker socket discussion above).

dclaw's CI does not currently exercise a Windows build target. Treat the Windows entries as defensive scaffolding rather than a supported runtime.
