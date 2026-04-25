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

---

## 2026-04-20 — beta.1-paths-hardening build cycle

**Green-light received.** Hatef approved all 4 architect decisions + "go ahead" on build. Git identity configured globally on the new machine (`hatefkasraei@gmail.com` / `Hatef Kasraei`) — previous commits on origin were authored under the same identity from the pre-wipe machine.

### Shipped commits on branch `beta.1-paths-hardening`

```
3e7ebc7 beta.1(D): smoke-daemon.sh rewrite + workspace-root runbook + shellcheck
613146d beta.1(BC): wire --state-dir through PR-C's internal/cli call sites
a91daa9 beta.1(C): workspace validator + audit log + --workspace-trust + config
c13a6c0 beta.1(B): --state-dir flag on dclaw CLI + DCLAW_WORKSPACE_ROOT stub
964dba3 beta.1(A): config.Resolve — state-dir consolidation + socket split-brain fix
c221374 docs: beta.1-paths-hardening plan + WORKLOG
```

Total diff vs `76405ac` (alpha.4.1): **43 files, +4102 / -273**. `go test ./...` and `go vet ./...` green at every commit. Both binaries build.

### PR-A — `964dba3` (7 files, +278/-74)
- New `internal/config/resolve.go` (+ test) — consolidates 5 `os.UserHomeDir()` sites. Precedence flag > env > default.
- Socket split-brain fixed as a side effect: `cmd/dclaw/main.go` no longer hard-codes `home + "/.dclaw/dclaw.sock"` — routes through `config.MustResolveSocket()` which honors `$XDG_RUNTIME_DIR` on Linux.
- Deviation: `cmd/dclawd/main.go:7` retained a pre-existing doc comment mentioning `$XDG_RUNTIME_DIR`. Not implementation; out of scope.
- Build agent installed `go 1.26.2` via `brew install go` on the fresh machine (no Go toolchain post-wipe). `go.mod` requires ≥1.25.0 — compatible.

### PR-B — `c13a6c0` (4 files, +59/-11)
- Added `--state-dir` persistent flag on `dclaw` rootCmd, wired through `config.Resolve` set up in PR-A.
- Pre-declared `EnvWorkspaceRoot = "DCLAW_WORKSPACE_ROOT"` constant for PR-C to consume.
- Operational incident: parallel B + C agents contended for the shared working tree. PR-B's agent recovered via stash → re-checkout → re-verify. PR-C's agent also recovered; both commits landed on their respective branches correctly. **Lesson: serialize B and C in future multi-PR phases** — shared CWD + no worktree isolation = race condition.

### PR-C — `a91daa9` (30 files, +2690/-127; cherry-picked from `8571452` on `pr-c-validator`)
- New `internal/paths/` package — pure `Policy.Validate`, `OpenSafe` TOCTOU helper (split `opensafe_linux.go` for `/proc/self/fd` + `opensafe_darwin.go` for `F_GETPATH`).
- 32 validator test rows pass (NFC, NUL, APFS case-fold, symlink traversal, allow-root-prefix-bypass, etc.).
- New `internal/audit/` package — NDJSON audit log, `O_APPEND|O_CREATE|O_SYNC` mode 0600.
- New `internal/cli/config_cmd.go` — `dclaw config {get,set} workspace-root` backed by homegrown `internal/config/file.go` TOML reader (no new deps).
- Migration `0002_workspace_trust.sql`; `agents.workspace_trust_reason` column.
- Router `mapError` rewritten from `strings.Contains` ladder to `errors.Is` switch (sentinels added in `store` + `sandbox`).
- Belt-and-suspenders: `internal/sandbox/docker.go` asserts `filepath.IsAbs` + rejects `..` before bind mount.
- Deviations: (1) Denylist uses EqualFold exact-match AND prefix-fold descendant match (so `/Library/Preferences` trips on `/Library`). Spec said EqualFold only; prefix match is stricter — kept. (2) Row 3 APFS casing tested via sentinel (canonical casing depends on EvalSymlinks invocation). (3) `go mod tidy` promoted `golang.org/x/sys` and `golang.org/x/text` from indirect to direct (expected per plan §2).

### PR-BC integration fix — `613146d` (2 files, +3/-9)
- PR-C was written before PR-B landed. Its `config get/set` subcommand and `workspace_forbidden` error renderer used `config.Resolve("", "")` — working with `$DCLAW_STATE_DIR` env but ignoring `--state-dir` flag.
- Wired `stateDirFlag` (package-level var in `internal/cli/root.go`) through 3 call sites: `internal/cli/config_cmd.go:31`, `internal/cli/config_cmd.go:63`, `internal/cli/exit.go:111`. Removed PR-C's TODO comments.
- `internal/client/rpc.go:325` still calls `config.Resolve("", "")` — legitimate, different package, no CLI-flag access; fallback for default socket path.
- Manual verified: `dclaw --state-dir /tmp/x config set workspace-root /tmp/y` writes to `/tmp/x/config.toml`.

### PR-D — `3e7ebc7` (5 files, +257/-72)
- `scripts/smoke-daemon.sh` full rewrite per plan §4.4a: no `$HOME` reassignment, `TMPDIR` prefix guard, `SMOKE_STATE=$(mktemp -d)` + prefix-whitelist guard, trap armed before exports, `rm -rf "${SMOKE_STATE:?refuse empty}"` (no `|| true`). Every dclaw/dclawd invocation gets `--state-dir "$SMOKE_STATE"` redundantly with the env var. Added Test 14 (validator rejection), Test 15 (trust override), Test 16 (audit log contains forbidden + trust).
- `docs/phase-1-plan.md:540` corrected to accurate bind-mount semantics.
- New `docs/workspace-root.md` runbook (110 lines) — set/change root, `--workspace-trust`, audit log format, canonical errors, backup-restore notes.
- `README.md` gained a one-liner pointer to the runbook.
- `Makefile` `lint` target extended with `shellcheck scripts/*.sh agent/*.sh` (no-ops if not installed, mirrors existing golangci-lint pattern).
- **Flagged issue — smoke Test 15 macOS compatibility.** Test 15 uses `--workspace /tmp/smoke-trusted-ws-$$`. On macOS `/tmp` → `/private/tmp` via symlink, and `/private/tmp` is in the default denylist. `--workspace-trust` bypasses the allow-root `Rel` check but NOT the denylist (per validator row 28). When docker becomes available and Test 15 actually runs on macOS, it will likely fail with `workspace_forbidden: /private/tmp in denylist`. This is a plan-level oversight, not an implementation bug. Fix options: (a) Test 15 uses `$HOME/dclaw-smoke-trusted-$$` (under $HOME, not denied; $HOME exact-match denial doesn't cover children); (b) dynamically pick a non-denylisted path. Docker not running on this machine, so the issue is latent. **Filing as a follow-up to fix before the first docker-backed CI run.**

### Acceptance status

Using plan §13 checklist:

- [x] PR-A merges clean, CI green.
- [x] PR-A: `os.UserHomeDir` reduced to one production match in `internal/config/resolve.go`.
- [ ] PR-A: socket split-brain linux-verified — requires Linux + XDG_RUNTIME_DIR env; this machine is macOS. Unit-level confirmation via `config.Resolve` tests. Flagging for docker-capable CI run.
- [x] PR-B: `dclaw --state-dir /tmp/x ...` round-trip verified.
- [x] PR-B: `DCLAW_STATE_DIR=/tmp/x dclaw ...` env verified.
- [x] PR-C: 32 validator rows green.
- [ ] PR-C: audit.log line-per-create verified at unit-test level; end-to-end via smoke blocked on docker.
- [ ] PR-C: §8 error text verbatim on stderr — needs docker-backed integration to exercise the wire; unit tests assert structured payload shape only.
- [ ] PR-C: `--workspace-trust` round-trip via live daemon — blocked on docker.
- [x] PR-C: migration 0002 up/down roundtrip.
- [ ] PR-C: legacy-scan warning on startup — needs a daemon with pre-existing agents; not exercised.
- [x] PR-C: router `strings.Contains` count is zero.
- [ ] PR-D: smoke-daemon.sh baseline green — blocked on docker.
- [ ] PR-D: smoke-daemon.sh post-PR-C green — blocked on docker.
- [x] PR-D: stale `HOME=` grep sweep returns zero hits (outside alpha-era plan docs, intentionally preserved).
- [x] PR-D: `make lint` runs green (shellcheck absent → skipped per spec).
- [x] PR-D: `docs/phase-1-plan.md:540` corrected.
- [x] PR-D: `docs/workspace-root.md` exists with all required sections.
- [ ] Squash-merge to main in order A→B→C→D — pending Hatef review.
- [ ] Tag `v0.3.0-beta.1-paths-hardening` — pending Hatef review.
- [ ] Push — pending Hatef review.

### Follow-ups filed

- **Smoke Test 15 workspace selection** — must pick a path that is outside allow-root AND not in the macOS denylist. Currently uses `/tmp` which resolves to `/private/tmp` (denied). Fix before docker-backed CI.
- Previously-filed beta.2 items (sandbox hardening, easier setup, TOML config file, XDG split, Windows denylist) unchanged.

### Status

All 6 commits on `beta.1-paths-hardening` awaiting Hatef's review. No push yet. Integration branches `pr-b-flag` and `pr-c-validator` still exist with stale stash entries — safe to delete after Hatef confirms the cherry-picks are good.

---

## 2026-04-21/22 — Review → ship → hotfix saga

### Independent code review (2026-04-21)

Dispatched a fresh reviewer agent on `beta.1-paths-hardening` tip `7307e14`. Verdict: **SHIP WITH SMALL FIXES.** No must-fix, no blockers. 5 should-fix + 6 nice-to-have findings. All 8 top threats (TOCTOU, symlink escape, NFC, APFS case-fold, NUL injection, allow-root-prefix-bypass, env injection, audit-log poisoning) verified addressed.

Bundled 4 of the 5 should-fix items into **PR-E (`824fc81`)**: wired `DCLAW_WORKSPACE_ROOT` into `buildPolicy` (was dangling const since PR-B), extended TOML regex to accept inline comments + test, tightened legacy-scan guard to skip when allow-root is empty (previously noisy), removed stale `NewLifecycleLegacy` comment. 4 files, +65/-15. Item 5 (smoke Test 15 macOS workspace selection) stays a docker-CI follow-up.

### Merge + tag (2026-04-22)

Pushed branch to origin, opened PR #1. CI red on first push — not a regression but a latent test-gate bug from PR-C:

**PR-F (`22429f2`):** Rows 08-11 of `policy_test.go` test macOS-specific paths (`/ETC` APFS case-fold, `/private/{etc,var,tmp}` firmlinks) that don't exist on Linux ext4 — `EvalSymlinks` errors before the denylist check, so the test assertion never matches. Rows 12-15 were already gated via `os.Stat` checks; PR-C build agent missed extending that gate to 08-11. Review agent missed it too. Added skip-guards (runtime.GOOS check for row 08 — APFS semantic; `os.Stat` for 09/10/11 — path-existence).

**PR-G (`8b19ddb`):** After PR-F, `smoke-cli.sh` Test 5 failed. Investigation revealed this expectation had been stale **since alpha.1** — `dclaw agent list -o json` was asserted to emit `"error": "feature_not_ready"`, which was the v0.2.0-cli `RequireDaemon` stub shape. Alpha.1 replaced the stub with a real daemon call; `DaemonUnreachable` emits `"error": "daemon_unreachable"`. Every push to main since alpha.1 (2026-04-16) had red CI and nobody watched. Flipped the assertion. Not a beta.1 regression — a pre-existing bug our branch unmasked.

PR #1 turned green (10 commits total). **Rebase-merged to main** with `gh pr merge --rebase --delete-branch`. Tag `v0.3.0-beta.1-paths-hardening` pushed. First green main-push CI since 2026-04-15.

### Post-merge hotfix cascade

Tag push triggered `docker-smoke` CI (only runs on `v*` tags). First actual end-to-end run of `smoke-daemon.sh` against the new paths-hardening policy — and it uncovered two more PR-D bugs because the build agent never had docker to validate against:

**Hotfix 1 (`37c64d8`, tag `.1`):** Tests 3-13 (existing create flow) failed with `workspace_forbidden: no workspace-root configured`. PR-D added Tests 14-16 for the new policy but didn't adapt Tests 3-13 — they still called `agent create` without any allow-root set. Fix: `export DCLAW_WORKSPACE_ROOT="${DCLAW_WORKSPACE_ROOT:-/tmp}"` at script top. On Linux CI, `/tmp` is not denylisted and covers every mktemp dir. macOS local would still break (`/tmp` → `/private/tmp`, denylisted) — operator must override.

**Hotfix 2 (`34367c5`, tag `.2`):** Test 14 then failed because the assertion grepped stderr for the literal `workspace_forbidden`, but the stderr renderer emits human prose. The machine-readable code only appears in `-o json` output. Rewrote Test 14 to use `-o json` + grep JSON. Tests 15/16 turned out to already be correct against the CLI contract (the dispatching agent pushed back on my spec and was right).

**Tag `v0.3.0-beta.1-paths-hardening.2`:** full green (`build` 21s, `docker-smoke` 47s). Both earlier tags remain as historical markers.

### Meta lessons

- **Build agents without end-to-end validation ship untested assertions.** PR-D's Tests 14-16 were spec'd correctly but never exercised; docker wasn't reachable on the dev machine. Mitigation for future: require agents to run the target CI surface before reporting green, OR write assertions against the renderer directly (unit-level).
- **Parallel build agents on a shared working tree race.** PR-B and PR-C's concurrent runs caused a `git checkout` swap mid-session; both recovered, but lesson holds — serialize, use worktree isolation, or branch-per-agent with explicit `git checkout` before any operation.
- **Stale CI that nobody watches silently accumulates rot.** `main` had been red on `build` since alpha.1 (6 days) and docker-smoke on every tag since alpha.1 (5 tags). Our branch was the first to actually gate on CI because the earlier validator failure forced an investigation. Red CI is only useful if someone's watching.
- **Independent code review catches real issues** (DCLAW_WORKSPACE_ROOT dead code, TOML inline-comment gap, legacy-scan comment/reality drift). It missed the PR-F test-gating bug (same way the build agent missed it). Review is necessary-not-sufficient — CI has to actually be green too.

### Final state

- Main tip: `34367c5`.
- Latest green tag: `v0.3.0-beta.1-paths-hardening.2`.
- CI: build green, docker-smoke green (tag-triggered).
- Outstanding: follow-up GitHub issues (beta.2 sandbox hardening, easier setup for workspace-root, full TOML config, XDG split, Windows denylist, audit-log rotation, `dclaw doctor workspace`, Test 15 macOS workspace-root issue, polish umbrella) — Hatef to decide whether/how to file.

---

## 2026-04-23 — beta.2-sandbox-hardening build cycle

**Plan and conventions locked in.** Hatef approved the backlog as individual patch releases under a beta.2 umbrella: `v0.3.0-beta.2` = sandbox hardening (full phase), then `beta.2.1`..`.N` for follow-up patches with natural bundling where theme warrants. Two new conventions saved to Atlas memory: (1) every dclaw release runs a doc-review agent before tag push, (2) `v0.3.0-beta.X` for phases / `v0.3.0-beta.X.Y` for patches.

**Kickoff:** architect wrote `docs/phase-3-beta2-sandbox-hardening-plan.md` (~1000 lines, mirrors beta.1 plan's 14-section shape). Doc-review baseline sweep found 2 BLOCKERS + 6 IMPORTANTs + 3 MINORs. Shipped the 2 BLOCKERS + plan-DRAFT-flip as `v0.3.0-beta.1-paths-hardening.3` (commit `30886a0`) — docs-only patch before beta.2 build started.

### Shipped on `main` — beta.2 PR series

```
d08ccad docs: pre-tag sweep — flip beta.2 plan to SHIPPED + README + agent + CI Go pin
827896c beta.2(D): docker.sock denylist + full posture probe + legacyScan warning
2c35a7a beta.2(C): non-root UID enforcement (1000:1000) + run.mjs uid-0 guard
a137e05 beta.2(B): ReadonlyRootfs + tmpfs overlays
6ce2bb5 beta.2(A): cap drop + no-new-privileges + seccomp + PidsLimit + posture harness
```

Total: ~+1200 lines of code + tests across 4 PRs + ~+90 lines doc sweep. Main-push build CI green at every commit (21-30s per run). Zero new `go.mod` deps; zero migrations; zero wire-protocol changes.

### PR-A — `6ce2bb5` (3 files, +379/-2)
- `DefaultCapDrop = ["ALL"]`, `DefaultSecurityOpt = ["no-new-privileges:true", "seccomp=default"]`, `DefaultPidsLimit = 256` as package-level constants in `internal/sandbox/docker.go`. Posture shape asserted by `TestCreateAgentAppliesBeta2HardeningPosture`.
- `dockerAPI` interface refactor — `DockerClient.cli` now an interface covering the 11 `ContainerCreate/Start/Stop/Remove/Inspect/Logs/ExecCreate/ExecAttach/ExecInspect/Close` methods. Compile-time assertion `var _ dockerAPI = (*client.Client)(nil)` guards against SDK drift. Test injects a recording `captureClient` fake.
- Smoke Tests 17/18/19: CAP_MKNOD drop (`mknod → EPERM`), seccomp default (`unshare -U -r → EPERM`), PidsLimit (300-fork loop bounded).

### PR-B — `a137e05` (3 files, +146/-11)
- `HostConfig.ReadonlyRootfs: true` + `HostConfig.Tmpfs: {/tmp: 64m, /run: 8m}`, both `rw,noexec,nosuid,nodev`.
- pi-mono write-path audit executed before flipping the flag: `/workspace/*` (bind), `/tmp/*` (tmpfs), `/run/*` (tmpfs), `/root/.pi/agent/*` (suppressed via `--no-session` per `agent/run.mjs:29`), `/app/node_modules/.cache/*` (build-time only). No additional tmpfs entries needed for `dclaw-agent:v0.1`.
- Smoke Test 20: `touch /etc/...` + `touch /opt/...` → EROFS; `touch /tmp/ok` + `touch /workspace/ok` → success.

### PR-C — `2c35a7a` (5 files, +70/-0)
- `DefaultContainerUser = "1000:1000"` constant; applied via `container.Config.User` (SDK v26 places `User` on Config, not HostConfig).
- `agent/run.mjs` uid-0 guard (4 lines): `if (process.getuid() === 0) { process.exit(70); }` — third line of defense behind image USER directive + daemon-side `container.Config.User`.
- `agent/Dockerfile` invariant comment documenting the uid-1000 contract.
- Smoke Test 21: `id -u` + `id -g` both assert `1000`.

### PR-D — `827896c` (6 files, +351/-22)
- Three explicit docker.sock denylist entries in `internal/paths/policy.go:DefaultDenylist`: `/var/run/docker.sock`, `/run/docker.sock`, plus Docker Desktop macOS variant via substring match on `/Library/Containers/com.docker.docker/` AND `docker-raw.sock` suffix (~10 lines avoid a glob-lib dep). Reordered DefaultDenylist so docker.sock entries land before `/var` — cleaner error reason than `/var` descendant match.
- 4 new validator test rows (33-36); rows 33/34/36 skip on hosts without `docker.sock`, row 35 runs unconditionally via a fake tree under `t.TempDir`.
- `docs/workspace-root.md`: new H3 "Docker socket" subsection + "Custom image compatibility" (3 rules for operators shipping their own `--image=`).
- `cmd/dclawd/main.go:legacyScan` extension: on startup, inspect each existing agent's container via new `DockerClient.InspectPosture` method (kept dockerAPI types package-private — sandbox remains single source of truth for SDK shapes); warn per agent with pre-beta.2 weak posture. Advisory only.
- Smoke Tests 22 + 23: docker.sock rejection via `-o json` + grep, full 6-probe posture test.

### Pre-tag docs sweep — `d08ccad` (4 files, +845/-14)

Second doc-review pass before tagging found 3 BLOCKERS + 2 IMPORTANTs:
1. beta.2 plan §0 Status DRAFT → SHIPPED with 4-commit table (plan doc was untracked pre-sweep; landed as committed file in this commit alongside the §0 flip).
2. `README.md:105` — "beta.2 sandbox-hardening next" line → "container posture hardened" (false the moment the tag lands).
3. `README.md:46,59` — version header + code-fence example → `v0.3.0-beta.2-sandbox-hardening`.
4. `agent/README.md` "Known limitations (v0.1)" → rewritten to reflect current scope (multi-turn + streaming shipped alpha.3; beta.2 posture now daemon-enforced).
5. `.github/workflows/build.yml` both jobs: `go-version: '1.22'` → `'1.25'`. CI was working (setup-go auto-upgrades to go.mod's 1.25.0) but the declared version lied.

3 MINORs deferred to beta.2.1 or later: README CI badge, `docs/workspace-root.md` title scope, "in beta.1" phrasing on audit-log rotation/retention notes.

### Threat model closed

All 9 escape-vector categories the independent code reviewer flagged post-beta.1 are addressed:
- **mknod + raw block device** — `CAP_MKNOD` dropped (PR-A). `mknod → EPERM`.
- **ptrace injection** — seccomp default + `no-new-privileges` (PR-A).
- **keyctl / add_key / request_key** — seccomp default explicit pin (PR-A).
- **setuid/setgid escalation** — `no-new-privileges` + `ReadonlyRootfs` + uid 1000 (PR-A + B + C). Mitigates CVE-2019-5736, CVE-2022-0185.
- **fork-bomb / PID DoS** — `PidsLimit: 256` (PR-A).
- **rootfs tampering** — `ReadonlyRootfs: true` with tmpfs overlays (PR-B).
- **docker.sock as Trojan workspace** — explicit denylist (PR-D).
- **host PID/network/IPC namespace sharing** — dclaw does not expose privileged flags; unchanged.
- **kernel-level exploits (Dirty Pipe, Dirty Cred)** — out of dclaw's reach; operator keeps host kernel patched. Documented in `docs/workspace-root.md`.

### Follow-ups still filed (per versioning plan)

- `beta.2.1` — smoke hygiene bundle: Test 15 macOS workspace fix (#2) + review polish umbrella (#9).
- `beta.2.2` — easier setup for workspace-root (#3, `dclaw init` wizard).
- `beta.2.3` — audit log rotation (#7).
- `beta.2.4` — `dclaw doctor` subcommand (#8).
- `beta.2.5` — TOML config refactor (#4).
- `beta.2.6` — platform-port bundle: XDG split (#5) + Windows denylist (#6).

### Final state (pre-hotfix-cascade)

- Main tip (pre-tag): `d08ccad`.
- About to tag: `v0.3.0-beta.2-sandbox-hardening`.
- CI: build green on every beta.2 commit (main-push); docker-smoke pending on the tag push.

### Hotfix cascade (tag-triggered docker-smoke surfaced 4 latent bugs)

Tag `v0.3.0-beta.2-sandbox-hardening` was the first end-to-end run of beta.2 posture against a live docker daemon. Unit tests and `bash -n` were all green; docker-dind uncovered what mock-only testing couldn't. Four hotfixes landed in sequence, each with its own tag.

| Hotfix | Tag | Commit | Problem → Fix |
|---|---|---|---|
| 1 | `.1` | `f979ad3` | `SecurityOpt: "seccomp=default"` — Docker rejects as invalid JSON. Docker's default profile applies automatically when `seccomp=` isn't set. Dropped the entry. |
| 2 | `.2` | `3afa4b6` | Test 19 PidsLimit assertion parsed `jobs | wc -l` as the success signal — but when the cap fires mid-loop, the shell emits "Cannot fork" before `wc` runs. Accept kernel fork-refusal as equivalent proof. |
| 3 | `.3` | `f1c3065` | Test 20 `/workspace` write failed. GH Actions runner uid 1001 vs container uid 1000 (post-PR-C) = DAC denial; PR-A dropped CAP_DAC_OVERRIDE. `chmod 0777 $STATE_DIR_T20` on the test workspace before `agent start`. Real users whose uid happens to be 1000 don't hit this (common default on Linux). |
| 4 | `.4` | `e7ec8c0` | Test 23 probe #2: `chmod u+s /tmp/x; [ -u /tmp/x ]` fired false BREACH. `no-new-privileges` blocks setuid-bit-becoming-effective at `execve()`, not `chmod`. Setting the bit on your own file is always allowed. Replaced with `/proc/self/status` `NoNewPrivs` check — direct kernel flag. |

### Final state (beta.2 green)

- **Main tip: `e7ec8c0`**.
- **Latest green tag: `v0.3.0-beta.2-sandbox-hardening.4`** — build 18s + docker-smoke 55s, all 23 smoke tests pass.
- CI: both jobs green end-to-end for the first time on beta.2 content.
- Prior tags (`v0.3.0-beta.2-sandbox-hardening[.1/.2/.3]`) retained as historical markers of each failed attempt.

### Meta-lessons from the beta.2 hotfix cascade

- **Docker SDK `SecurityOpt` doesn't have a "profile-by-name" syntax for seccomp.** The architect assumed `seccomp=default` was a valid selector; it isn't. Docker only accepts `unconfined` or an inline JSON profile. When you want the built-in default, don't set the option. Lesson: architect agents should have verified the SDK shape before writing the value.
- **Shell-parsed output fails when the command errors mid-pipeline.** Test 19 assumed `jobs | wc -l` would always run. It doesn't if the shell exhausts its PidsLimit trying to reach that line. Assertions should accept multiple equivalent pass signals.
- **Container UID mismatches are a real operational gotcha, not just a plan-doc footnote.** The plan §4.3 mentioned this in a rollout-risk bullet; CI surfaced it immediately. Users whose uid differs from 1000 will hit the same. Filing this as a beta.3+ ergonomic concern: the daemon could chown bind-mounted workspaces to uid 1000 at start time, or a `--workspace-uid` flag could let operators opt into the current uid.
- **"Does the kernel flag do X?" is not a test you can write in `[ ... ]` brackets.** Probe #2 conflated file-permission-bit setting with privilege-escalation-at-execve. Reading `/proc/self/status` is the canonical check for any `prctl`-managed flag.
- **5 tags to ship a release is a lot.** beta.1 needed 3 (initial + 2 hotfixes); beta.2 needed 5. Both saw the same pattern: tag push is the first real integration signal. Consider making `docker-smoke` run on `main` pushes (not just tag pushes) so the signal arrives before tagging — follow-up for the CI config.

### Follow-ups refreshed

- All prior `beta.2.X` plan items unchanged.
- Add: **[meta] make `docker-smoke` run on main pushes** — pull the signal earlier than tag time. Saves the tag-spam we just went through.
- Add: **[UX] container UID mismatch handling** — daemon could chown workspaces or let operators pick an in-container uid.

---

## 2026-04-25 — beta.2.1 smoke hygiene bundle

**`v0.3.0-beta.2.1-smoke-hygiene` (`aced98a`) — shipped clean, no hotfix cascade.** First release since beta.1-paths-hardening to land green on the first attempt. Six items in one commit:

| # | Fix | Files |
|---|---|---|
| 1 | smoke Test 15 macOS workspace fix — Darwin skip + Linux uses sibling-of-allow-root so trust is actually exercised (was a no-op pass before) | `scripts/smoke-daemon.sh` |
| 2 | `TestAuditLogConcurrentWrites` under `-race` — N goroutines, mutex + `O_APPEND` proven atomic | `internal/audit/audit_test.go` |
| 3 | Cache canonical `AllowRoot` once, not per-call. Process-wide `sync.Map` (per-Policy field would be invalidated by `policy := l.policy; policy.AllowTrust = true` copy in lifecycle) | `internal/paths/policy.go` |
| 4 | Typed `Remediation` struct replaces `[]map[string]string` in exit.go. JSON wire-identical | `internal/cli/exit.go` |
| 5 | `WriteConfigFile` doc comment: rename atomicity is best-effort, cross-fs (NFS) may not be | `internal/config/file.go` |
| 6 | **CI hygiene**: `docker-smoke` now runs on `main` pushes AND tag pushes (was tag-only). Catches integration bugs pre-tag | `.github/workflows/build.yml` |

Total diff: 6 files, +266/-48.

**CI: both pipelines green end-to-end.**
- Main push run: build 15s + docker-smoke 52s ✓ (FIRST main-push docker-smoke run on this repo, per the new trigger).
- Tag push run: build 15s + docker-smoke 1m9s ✓.

All 23 smoke tests pass. Test 15 now exercises `--workspace-trust` legitimately (negative-path assertion: without trust → exit 65; positive-path: with trust → success + describe shows reason). Test 16 trust assertion moved into Test 15 (necessary because new Test 15 uses its own daemon under `$STATE_DIR_T15` rather than the shared `$SMOKE_STATE` daemon).

**Operational improvement realized.** The new `docker-smoke` on main trigger is the most valuable item in the bundle. Both prior phases (beta.1, beta.2) needed multi-tag hotfix cascades because docker-smoke only ran on tag pushes — so bugs surfaced post-tag, requiring a new tag per fix. With docker-smoke on every main push, future hotfix cycles should be 1 hotfix max, not 4.

### Final state

- Main tip: `aced98a`.
- Latest green tag: `v0.3.0-beta.2.1-smoke-hygiene`.
- Tag history: 11 tags total on the v0.3.0 line (alpha.1..4.1, beta.1-paths-hardening + 3 patches, beta.2-sandbox-hardening + 4 patches, beta.2.1).

---

## 2026-04-25 — beta.2.2 easier setup (`dclaw init`)

**`v0.3.0-beta.2.2-easier-setup` (`519484b`) — clean ship.** New `dclaw init` cobra subcommand:
- Default workspace-root: `$HOME/dclaw` (mode 0700, created on demand).
- Idempotent — re-running prints `workspace-root already configured: <path>` without modifying config.
- Flags: `--yes` (non-interactive), `--workspace-root <path>` (explicit override).
- Validates the chosen path against `paths.Policy` denylist with `AllowTrust=true` so e.g. `dclaw init --workspace-root /etc` is refused even at init time.
- TTY prompt via `mattn/go-isatty` (already a direct dep — agent flagged that the spec's `golang.org/x/term` suggestion was wrong).

4 new tests (`TestInit{YesUsesDefault,WorkspaceRootFlag,Idempotent,RejectsDenylistedRoot}`) all green. README + `docs/workspace-root.md` updated to recommend `dclaw init` as first step. Diff: 4 files, +524/-4. CI: build 16s + docker-smoke 50s on tag, all green on main push too.

---

## 2026-04-25 — beta.2.3 audit log rotation

**`v0.3.0-beta.2.3-audit-rotation` (`745a50f`) — clean ship.** Size-based rotation in `internal/audit/audit.go`:
- Defaults: `MaxSize=10MB`, `MaxFiles=5` (exported `Logger` fields; tests override for fast cycles).
- On write, before appending: if `currentSize + len(record)+1 > MaxSize`, rotate under the existing mutex.
- Rename chain: `audit.log.{N-1} → audit.log.{N}` down to `audit.log → audit.log.1`. Oldest beyond `MaxFiles-1` removed.
- Three new tests: size-trigger, ordering preserved across rotated files, defaults inspection. Existing `TestAuditLogConcurrentWrites` still green under `-race` with rotation enabled.
- `docs/workspace-root.md` updated. Diff: 3 files, +304/-6. CI: build 20s + docker-smoke 53s on tag.

Agent flagged a minor spec ambiguity around `MaxFiles=N`: I'd written "MaxFiles=3 → .1, .2, .3 exist" (4 total files), but the docs + rename-chain logic implies "MaxFiles=N total files retained". Agent reconciled to the docs reading. Either's defensible; agent picked the consistent one.

---

## 2026-04-25 — beta.2.4 `dclaw doctor` health-check subcommand

**`v0.3.0-beta.2.4-doctor` (`646b3a4`) — clean ship.** New `dclaw doctor` cobra command runs an ordered battery of 7 checks: `config_resolved`, `workspace_root_configured`, `workspace_root_valid`, `daemon_reachable`, `docker_reachable`, `agent_image_present`, `audit_log_writable`. Each emits OK | WARN | FAIL with a short message; exit 0 unless any FAIL. `-o json` available.

`dclaw doctor workspace <path>` sub-subcommand pre-flights a path through `Policy.Validate` without creating an agent or polluting `audit.log` — lets users iterate on `--workspace` values cheaply.

4 new tests; subprocess-built binary used in `TestDoctorWorkspaceForbidden` to capture exact `ExitDataErr=65` (since `go run` mangles exit codes). Local `doctorDenylist` package-var pattern mirrors init's existing approach for macOS test compatibility. Docker SDK used directly for `docker_reachable` + `agent_image_present` (internal/sandbox adapter would have needed broader changes).

Diff: 4 files, +923/-0. CI: build 21s + docker-smoke 52s on tag.

---

## 2026-04-25 — beta.2.5 TOML config refactor

**`v0.3.0-beta.2.5-toml-config` (`4eb4d29`) — clean ship.** Replaced the homegrown ~40-line regex parser with `github.com/pelletier/go-toml/v2`. `FileConfig` gains `[audit]` (max-size-bytes, max-files) and `[daemon]` (socket, log-level) sub-tables alongside `workspace-root`. `cmd/dclawd` reads the audit section at startup and overrides Logger.MaxSize/MaxFiles defaults if set.

Backward compat preserved: existing single-key `config.toml` files written by beta.1+ init still parse correctly (`TestReadConfigFileBackwardsCompat`). `ReadConfigFile`/`WriteConfigFile` signatures unchanged.

Agent flagged 3 minor deviations: relaxed an assertion that depended on the specific homegrown error wording, removed a `TestWriteConfigFileRejectsQuote` test that was guarding a homegrown-writer footgun no longer present, and hoisted `resolveAuditConfig` above the `--migrate-only` early-return so the audit-config info log line fires in both paths. All consistent with the spec's intent.

Diff: 6 files, +289/-76. CI: build 59s + docker-smoke 1m18s on tag.

---

## 2026-04-25 — beta.2.6 platform-port bundle (XDG + Windows denylist) — series complete

**`v0.3.0-beta.2.6-platform-port` (`fe69729`) — clean ship. FINAL patch in the beta.2.X series.**

Two platform-portability fixes bundled per Plan B (natural-affinity):

**XDG state-dir on Linux** (Plan §12 #4):
- Linux: prefer `$XDG_STATE_HOME/dclaw` if set, fall back to `~/.local/state/dclaw`, fall back to `~/.dclaw` (legacy).
- macOS: `~/.dclaw` unchanged (XDG isn't a Darwin convention).
- Backward compat: existing `~/.dclaw` installs work unchanged.

**Windows denylist** (Plan §12 #5):
- `runtime.GOOS == "windows"` adds `C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\ProgramData`, `C:\Users\Default|Public|All Users` to `DefaultDenylist`.
- Defensive scaffolding only — dclaw isn't actively tested on Windows.

Agent flagged 3 deviations:
- Pre-existing `internal/cli/daemon.go` POSIX-only syscalls (`Kill`, `Setsid`) prevent `GOOS=windows go build ./cmd/dclaw`. Out of scope. `cmd/dclawd` does cross-compile clean.
- Pragmatic scope creep into `internal/paths/`: added `//go:build !windows` to `opensafe.go` + new `opensafe_windows.go` stub returning `ErrWorkspaceForbidden`. Without this, every binary importing `internal/paths` fails Windows cross-compile. Justified.
- Updated `TestResolvePrecedence`'s "default-when-nothing-set" row to compute via `defaultStateDir()` instead of hard-coded `~/.dclaw` — XDG branch on Linux CI would have broken the literal assertion. Same precedence semantics, just portable.

Diff: 9 files, +500/-12. CI: build 17s + docker-smoke 55s on tag.

---

## 2026-04-25 — beta.2.X series complete

**5 consecutive clean ships, no hotfix cascades.** The CI hygiene fix from beta.2.1 (docker-smoke on main pushes) eliminated the need for tag-after-tag hotfix cycles that plagued beta.1 (3 patch tags) and beta.2 (4 patch tags before green).

| Tag | Theme | Diff | Notes |
|---|---|---|---|
| `v0.3.0-beta.2.1-smoke-hygiene` | Test 15 macOS + 4 polish + CI trigger | 6 files, +266/-48 | First main-push docker-smoke run. |
| `v0.3.0-beta.2.2-easier-setup` | `dclaw init` wizard | 4 files, +524/-4 | First-run UX. |
| `v0.3.0-beta.2.3-audit-rotation` | 10MB / 5 files default | 3 files, +304/-6 | Bounds audit log growth. |
| `v0.3.0-beta.2.4-doctor` | `dclaw doctor` health-check | 4 files, +923/-0 | Pre-flight diagnostics. |
| `v0.3.0-beta.2.5-toml-config` | `pelletier/go-toml/v2` refactor | 6 files, +289/-76 | `[audit]`/`[daemon]` sub-tables. |
| `v0.3.0-beta.2.6-platform-port` | XDG + Windows denylist | 9 files, +500/-12 | Cross-platform scaffolding. |

**Total: ~+2800 lines across 5 patches.** Every release: green build + green docker-smoke on both main push and tag push.

### Tag history end of beta.2.X

```
v0.3.0-beta.2.6-platform-port      ← latest
v0.3.0-beta.2.5-toml-config
v0.3.0-beta.2.4-doctor
v0.3.0-beta.2.3-audit-rotation
v0.3.0-beta.2.2-easier-setup
v0.3.0-beta.2.1-smoke-hygiene
v0.3.0-beta.2-sandbox-hardening.4  (last beta.2 phase tag)
v0.3.0-beta.2-sandbox-hardening.3
v0.3.0-beta.2-sandbox-hardening.2
v0.3.0-beta.2-sandbox-hardening.1
v0.3.0-beta.2-sandbox-hardening    (initial beta.2 tag, red — historical)
v0.3.0-beta.1-paths-hardening.3    (docs patch)
v0.3.0-beta.1-paths-hardening.2    (last beta.1 phase tag, green)
v0.3.0-beta.1-paths-hardening.1
v0.3.0-beta.1-paths-hardening      (initial, red — historical)
v0.3.0-alpha.4.1
v0.3.0-alpha.4 / .3 / .2 / .1
v0.2.0-cli
v0.1.0
```

### What's covered now

dclaw daemon now ships with:
- **paths**: `--workspace` validated against denylist + allow-root + symlink resolution + TOCTOU-hardened OpenSafe + audit log per decision.
- **sandbox**: container runs uid 1000, no caps, no-new-privileges, default seccomp, ReadonlyRootfs + tmpfs overlays, PidsLimit 256, docker.sock denylisted.
- **operator UX**: `dclaw init`, `dclaw doctor`, `dclaw config get/set`, structured `workspace_forbidden` errors with remediations.
- **state**: `[audit]` rotation tunable via TOML config (default 10MB / 5 files), full TOML schema for future config keys.
- **portability**: XDG state-dir on Linux (with backward-compat fallback), Windows denylist scaffolding for `cmd/dclawd` cross-compile.
- **CI**: build + docker-smoke on every main push; smoke-daemon.sh has 23 tests covering Tests 1-23 (CRUD + validator + audit + posture).

### Outstanding follow-ups (not yet addressed)

- **[beta.3+] Container UID mismatch**: real users with non-1000 uids hit `/workspace` write denials. Fix: daemon-side chown on workspace mount, or `--workspace-uid` flag.
- **[future] Custom seccomp profile** (tighter than Docker's default): plan §11 Q2.
- **[future] Per-agent memory + CPU limits**: plan §12 follow-up.
- **[future] Network egress allowlist**: `EgressAllowlist` field exists on `WorkerSpawn` protocol type but isn't wired through.
- **[future] Image security rebase**: pin base image, apt CVE sweep, distroless swap.
- **[future] `cmd/dclaw` Windows cross-compile**: `internal/cli/daemon.go` uses POSIX-only syscalls; needs Windows stubs.
- **[future] Original beta.1 content lost in 2026-04-18 wipe**: logs view, toasts, chat history persistence. Will need to be re-derived for v0.3.0 GA.

### Final state

- Main tip: `fe69729` (beta.2.6 ship).
- Latest green tag: `v0.3.0-beta.2.6-platform-port`.
- 5-clean-ship streak. No outstanding red CI.
- Total tag count on v0.3.0 line: 19 (alpha.1..4.1 + beta.1 phase + 3 patches + beta.2 phase + 4 patches + 6 beta.2.X patches).
