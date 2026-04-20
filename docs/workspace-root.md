# workspace-root runbook (beta.1-paths-hardening)

Operational guide for the workspace-path validator shipped in beta.1-paths-hardening. Covers what `workspace-root` is, how to set/change it, the `--workspace-trust` escape hatch, the audit log, canonical errors with fixes, and backup-restore notes for pre-wipe SQLite snapshots.

## What is workspace-root?

`workspace-root` is the allow-root under which any `--workspace` path passed to `dclaw agent create` must live. It is configured per-operator via `dclaw config set workspace-root <path>` and persisted to `$DCLAW_STATE_DIR/config.toml`. At create-time the daemon runs `Policy.Validate` over the raw `--workspace` string: the path is cleaned, NFC-normalized, symlink-resolved (via `filepath.EvalSymlinks`), then checked two ways — (1) a hard-coded denylist of system paths (`/`, `/etc`, `/private/etc`, `/private/var`, `/Library`, `/Applications`, `/opt/homebrew`, the daemon user's `$HOME`, `/Volumes/External`, and the usual macOS suspects); (2) a `filepath.Rel`-based containment check against the configured `workspace-root`, which closes the sibling-prefix bypass — so `/Users/hatef/dclaw-agents-evil` does NOT pass just because the allow-root is `/Users/hatef/dclaw-agents`. If both checks fail, the daemon rejects the create with `workspace_forbidden` (exit 65, wire error `-32007`). The `--workspace-trust "<reason>"` flag is the documented escape hatch: it bypasses the allow-root check (but NOT the denylist), records the operator-supplied reason in `state.db`, surfaces it in `dclaw agent describe`, and writes an `outcome=trust` line to the audit log.

## Setting it the first time

```bash
dclaw config set workspace-root ~/dclaw-agents
```

Writes `workspace-root = "/Users/<you>/dclaw-agents"` to `$DCLAW_STATE_DIR/config.toml` (mode 0600). The daemon picks it up at next `agent create`. Before the first `config set`, `workspace-root` is zero-value and every `agent create` without `--workspace-trust` returns `workspace_forbidden` with `Configured allow-root: (not configured — run 'dclaw config set workspace-root <path>')`.

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
- **Rotation:** none in beta.1. The file grows unbounded. Rotation is a documented follow-up.
- **Retention:** keep forever in beta.1.

Example line:

```
{"ts":"2026-04-19T14:05:11.918Z","agent_name":"risky","raw_input":"/etc","canonical":"/etc","outcome":"forbidden","reason":"","policy_version":1}
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
