# dclaw WORKLOG

Reverse-chronological log of what happened and what we did, session by session. Redundant to plans by design — plans look forward, this log looks backward and survives machine loss if pushed.

---

## 2026-04-19 — Post-wipe RCA + paths-hardening plan

**Incident recap.** On 2026-04-18, mid-beta.1 execution (after `v0.3.0-alpha.4.1` shipped as `76405ac`), Hatef's machine was wiped. Unpushed local work lost: `docs/phase-3-beta1-plan.md` (~1444 lines), Phase 0 commit `d84554d` (migration `0003_` + protocol types + repo methods), Agent A commit `29633a8` (logs view + streaming RPC + Test 14), plus whatever Agents B (toasts) and C (chat history persistence) produced.

**Investigation findings.**
- Discord transcript had zero destructive commands. Last message before the gap was the "~20-30 min each" deploy of Agents B+C.
- Origin remote is still at `76405ac`; nothing past alpha.4.1 was ever pushed.
- User's `$HOME` changed from `/Users/hatef/` to `/Users/macmini/` → OS reimage consistent with the wipe account.
- Working hypothesis on root cause: sub-agent ran destructive bash off-transcript (cleanup step with wrong path, etc.). Definitive cause unrecoverable — key logs were on the wiped machine.

**Secondary finding — architecture hole.** During the RCA, found that `--workspace` has **zero path validation** anywhere in the Go code. `internal/cli/agent.go:334` → `internal/daemon/lifecycle.go:62` → `internal/sandbox/docker.go:90-96` passes the string straight to `mount.Mount{Type: TypeBind, Source: spec.Workspace}`. Binding `$HOME` or `/` as a workspace gives the in-container agent write access to the host path. `docs/phase-1-plan.md:540` claims "host is fine under `rm -rf /`" — wrong.

**Additional path concerns surfaced.**
- `scripts/smoke-daemon.sh:44` has `rm -rf "$HOME" || true` — safe as written today (HOME is reassigned to `mktemp -d` on line 11 before the `trap cleanup EXIT` on line 46), but fragile pattern: any refactor that reorders, any `source`-style invocation, any `TMPDIR=$HOME` env injection, and cleanup wipes the real home.
- 5-way duplication of `os.UserHomeDir()` across `cmd/dclaw/main.go:65`, `internal/client/rpc.go:323`, `internal/cli/root.go:83`, `internal/cli/daemon.go:30,91`, `internal/daemon/config.go:26`.
- Socket-path split-brain: `cmd/dclaw/main.go:64-70` hard-codes `home + "/.dclaw/dclaw.sock"` bypassing `DefaultSocketPath`. On Linux with `$XDG_RUNTIME_DIR` set, CLI and daemon already point at different sockets.

**Three-architect review (Security, UX/compat, Implementation).** 28 findings total. Key must-fixes beyond the initial plan:
1. `$HOME/dclaw/` default is wrong — both blast-radius and user-hostile. Decision: **no default**, require `dclaw config set workspace-root <path>` or `--workspace-root`.
2. Validate on `agent create` only, not `agent start` — existing agents in `state.db` have legacy workspace paths; grandfather them with a startup warning.
3. `filepath.Clean` alone insufficient — need `filepath.EvalSymlinks` + `O_NOFOLLOW` + re-canonicalize to defeat TOCTOU and symlink attacks.
4. Allowlist check must use `filepath.Rel` + "not starts with `..`", not `strings.HasPrefix` (else `$HOME/dclaw-evil` passes when root is `$HOME/dclaw`).
5. macOS denylist needs `/private/{etc,var,tmp}`, `/Volumes`, `/Library`, `/Applications`, `/opt`, `strings.EqualFold` comparison (APFS case-insensitivity), NFC Unicode normalization.
6. Rename escape hatch to `--workspace-trust=<reason>` — required reason string, persisted per-agent in state.db, shown in `agent describe`.
7. `cmd/dclaw/main.go:64-70` socket hard-code must be fixed as part of the `config.Resolve` refactor.

**Decisions locked in by Hatef.**
- (a) No default workspace root; file "easier setup" as post-beta.1 follow-up.
- Container-escape hardening (caps/seccomp/user-ns/ReadonlyRootfs/non-root-UID/PidsLimit/docker.sock-refuse) → separate **beta.2 sandbox-hardening** phase.
- Audit log `$STATE_DIR/audit.log` append-only `O_SYNC` — **included** in PR-C.
- Batch all 4 PRs into one review cycle.

**Operational rules adopted this session (persisted to Atlas memory, apply to all future dclaw work).**
- **Orchestrator-only.** Atlas deploys sub-agents for any implementation. No direct Edit/Write on code files.
- **WORKLOG.md.** This file, at repo root. Updated synchronously. Pushed with every commit batch so it survives machine loss.

**Work shape going into beta.1-paths-hardening.** Four PRs:
- **PR-A** (~150 lines, pure refactor, zero behavior change): new `internal/config/resolve.go`, consolidates the 5 home-dir resolutions, fixes socket split-brain as a side effect.
- **PR-B** (~100 lines): `--state-dir` persistent flag on `dclaw` rootCmd, `DCLAW_STATE_DIR` env, `DCLAW_WORKSPACE_ROOT` env. Depends on A.
- **PR-C** (~400 lines): new `internal/paths/` package with pure validator (Clean + EvalSymlinks + Rel + denylist + NFC + case-fold), `paths.OpenSafe` TOCTOU helper, `protocol.ErrWorkspaceForbidden = -32007`, router `mapError` switched from `strings.Contains` to `errors.Is`, CLI structured error render, `--workspace-trust=<reason>` persisted per-agent, append-only audit log. Independent of B.
- **PR-D** (~200 lines): rewrite `scripts/smoke-daemon.sh` (unset `TMPDIR`, `SMOKE_STATE` with prefix guard, trap-before-export, no `$HOME` reference anywhere), fix `docs/phase-1-plan.md:540`, doc sweep for stale `HOME=` references, new `docs/workspace-root.md` runbook, shellcheck in Makefile `lint`. Depends on B + C.

**Follow-ups filed for post-beta.1.**
- beta.2 sandbox hardening (Docker container escape surface)
- "Easier setup" for workspace-root (auto-create or interactive prompt)
- TOML config file at `$DCLAW_STATE_DIR/config.toml` with `flag > env > config > default` precedence
- XDG-aware state split on Linux
- Windows denylist via `runtime.GOOS` switch

**Status at end of session.** Architects done. Formal plan doc written and saved to `docs/phase-3-beta1-paths-hardening-plan.md`. Build not yet started — gated on Hatef sign-off of the plan doc.

**Open questions in the plan awaiting Hatef's confirmation:**
- Migration number `0002_` chosen (authoritative baseline has only `0001_initial.sql`; lost Phase-0 `0003_` work is unrecoverable). Documented residual risk for pre-wipe backups.
- TOML parsing via homegrown ~40-line reader (one key: `workspace-root`) rather than adding `github.com/pelletier/go-toml/v2`.
- `dclaw doctor workspace` pre-flight subcommand deferred to post-beta.1.
- Added `/root` to the denylist alongside `/home`.
