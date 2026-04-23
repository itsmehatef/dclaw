# Phase 3 Beta.2 Sandbox-Hardening Plan — v0.3.0-beta.2 Container-Escape Surface

**Goal:** One batched, four-PR series that closes the container-escape surface flagged by the post-beta.1 independent code review: the `dclaw-agent:v0.1` container currently runs as **uid 0** with **full default capabilities**, a **writable rootfs**, **no seccomp override**, **no `no-new-privileges`**, **no `PidsLimit`**, and no explicit denial of `docker.sock` as a bind-mount source. beta.2 drops to `CapDrop: ALL`, sets `no-new-privileges`, pins Docker's default seccomp profile, makes the rootfs read-only with explicit `/tmp` + `/run` tmpfs overlays, flips the agent image to a non-root UID, caps PIDs at 256, and adds `docker.sock` to the path denylist. Zero new user features; one new user-visible surface (`--workspace-trust` still the only escape hatch for path policy; container-posture has NO escape hatch by design). Paths hardening (beta.1) bounded WHERE the bind-mount points; beta.2 bounds WHAT the containerized agent can do once inside.

**Prereq:** `v0.3.0-beta.1-paths-hardening.2` tagged at commit `34367c5`. Main tip `02d4119` (documentation-only post-merge). `docs/phase-3-beta1-paths-hardening-plan.md` is the most recently shipped phase doc and the structural template for this plan. No migrations in flight. Only migrations on disk: `0001_initial.sql`, `0002_workspace_trust.sql`. `go 1.25.0` installed; Docker reachable; `dclaw-agent:v0.1` image built.

---

## 0. Status

**SHIPPED (2026-04-24) as `v0.3.0-beta.2-sandbox-hardening`.** Closed on origin at tag; see `WORKLOG.md` for the ship notes.

**Commits (on `main`, in order):**

| Hash | Scope |
|------|-------|
| `6ce2bb5` | beta.2(A): cap drop + no-new-privileges + seccomp + PidsLimit + posture harness |
| `a137e05` | beta.2(B): ReadonlyRootfs + tmpfs overlays |
| `2c35a7a` | beta.2(C): non-root UID enforcement (1000:1000) + run.mjs uid-0 guard |
| `827896c` | beta.2(D): docker.sock denylist + full posture probe + legacyScan warning |

| Field | Value |
|---|---|
| **Target tag** | `v0.3.0-beta.2-sandbox-hardening` (shipped; hotfix revs land as `.1`, `.2` matching the beta.1 pattern) |
| **Branch** | `main` (single batched review cycle; sub-branches per PR, squash-merged) |
| **Base commit** | `02d4119` (main tip, WORKLOG-only commit on top of `v0.3.0-beta.1-paths-hardening.2` @ `34367c5`) |
| **Est. duration** | 2–3 days (4 PRs, sequenced A → B → C → D) |
| **Prereqs** | beta.1-paths-hardening.2 green; smoke-daemon.sh Tests 1-16 green on tip |
| **Trigger** | Independent code review post-beta.1 merge: "Container-escape surface completely untouched. `internal/sandbox/docker.go:98-107` sets no `SecurityOpt`, no `CapDrop`, no `ReadonlyRootfs`, no `User`, no `UsernsMode`, no `PidsLimit`, no `Tmpfs`. Inside the container is uid 0 with full default caps — trivial to mknod a block device pointing at the host disk and read raw sectors." |

---

## 1. Overview

beta.1-paths-hardening bounded WHERE an agent's bind-mounted workspace can point on the host. beta.2 is orthogonal: bound WHAT the containerized agent can do once inside. An agent who convinces pi-mono to call `mknod`, `ptrace`, `keyctl`, or `setuid` today would get full raw-device access because the container runs uid 0 with default caps and a writable rootfs. beta.2 closes that.

The hole has seven distinct dimensions, each with its own implementation site:

1. **Capability drop.** `HostConfig.CapDrop: []string{"ALL"}` + empty `CapAdd`. pi-mono's Anthropic API client is pure HTTP; it needs no `CAP_SYS_ADMIN`, no `CAP_NET_RAW`, no `CAP_MKNOD`. Default Docker caps (14 caps incl. `CAP_NET_RAW`, `CAP_MKNOD`, `CAP_SYS_CHROOT`) are pure attack surface.
2. **`no-new-privileges`.** `SecurityOpt: []string{"no-new-privileges:true"}`. Blocks setuid/setgid escalation even if a setuid binary somehow exists on the rootfs. Cheap, high-value.
3. **Seccomp profile.** Pin Docker's default profile explicitly via `SecurityOpt: "seccomp=<builtin-default>"`. Today we rely on the daemon-side default being enabled; explicit is safer and future-proofs against a misconfigured daemon. A tighter custom policy is deferred.
4. **ReadonlyRootfs + tmpfs overlays.** `HostConfig.ReadonlyRootfs: true` + `Tmpfs: map[string]string{"/tmp": "rw,noexec,nosuid,nodev,size=64m", "/run": "rw,noexec,nosuid,nodev,size=8m"}`. Anything the agent writes vanishes with the container; workspace writes still go through the bind-mount.
5. **Non-root UID in the agent image.** `agent/Dockerfile` currently runs as `node` (uid 1000) — good — but `/workspace` is `WORKDIR` under `USER node` so it is created owned by `node:node`. Audit path: confirm `node` is 1000 non-root, and document that the pid-1 process is not uid 0. Add explicit `HostConfig.User: "1000:1000"` from the daemon side as belt-and-suspenders so a future image rebase that regresses to root is rejected (the daemon enforces the UID regardless of what `USER` the image declared).
6. **PidsLimit.** `HostConfig.Resources.PidsLimit: &256`. Caps fork-bomb surface. pi-mono spawns ~5 processes steady-state; 256 is generous.
7. **Refuse `docker.sock` as a workspace.** Add `/var/run/docker.sock`, `/run/docker.sock` (Linux), `/Users/*/Library/Containers/com.docker.docker/Data/docker-raw.sock` (Docker Desktop macOS) to `internal/paths/policy.go` `DefaultDenylist` so a `--workspace=/var/run/docker.sock` is rejected pre-mount. Already implicitly blocked by `/var` / `/run` denylist descendants (per `DefaultDenylist` at `internal/paths/policy.go:48-64`, `/var` is listed); explicit entries are clearer.

**What beta.2-sandbox-hardening delivers (IN SCOPE):**

- **PR-A — Capability drop + `no-new-privileges` + seccomp + PidsLimit.** `internal/sandbox/docker.go:CreateAgent` gains four `HostConfig` fields: `CapDrop`, `SecurityOpt` (two entries), `Resources.PidsLimit`. Zero code outside `internal/sandbox` touched; no wire protocol changes; no migration. Tests extend the mock client in `internal/daemon/lifecycle_test.go` by introducing a recording `DockerClient` interface that captures the `HostConfig` shape, then asserts every expected field. Smoke-test harness gains Test 17 (cap-probe) + Test 18 (seccomp-probe) + Test 19 (fork-bomb-probe).
- **PR-B — ReadonlyRootfs + tmpfs overlays.** `CreateAgent` sets `ReadonlyRootfs: true` and `Tmpfs: {/tmp: 64m noexec, /run: 8m noexec}`. Mount-audit sweep over `agent/Dockerfile` to confirm pi-mono writes only under `/tmp`, `/run`, `/workspace`, or `/app/node_modules/.cache/*` (which we also make tmpfs-overlay on demand). Potential impact: if pi-mono or the upstream node image writes to `/root` or `/home/node`, we need to tmpfs-overlay those too. Audit executed as part of PR-B scoping. Smoke Test 20 (rootfs-write-probe) asserts `touch /etc/x` and `touch /opt/x` both fail with `EROFS`.
- **PR-C — Non-root UID enforcement + agent image audit.** `HostConfig.User: "1000:1000"` set in `CreateAgent` regardless of image `USER`. The existing `dclaw-agent:v0.1` image already ships with `USER node` (uid 1000) per `agent/Dockerfile:45`; this PR documents the invariant and the daemon-side enforcement. Any future image rebase that regresses to root gets UID 1000 applied anyway. A second optional belt: add a runtime `whoami` probe to `agent/run.mjs` that exits `EX_SOFTWARE` (70) if `process.getuid() === 0`. Smoke Test 21 (uid-probe) asserts `id -u` returns 1000.
- **PR-D — docker.sock denylist + smoke-suite extension.** `internal/paths/policy.go:DefaultDenylist` gets three new entries for the Docker socket paths. `internal/paths/policy_test.go` gets four new table rows (Linux socket, Docker Desktop macOS socket, /run variant, and trailing-slash variant). `docs/workspace-root.md` gets a "Docker socket" subsection in the denylist list. `scripts/smoke-daemon.sh` Tests 22 is redundant with PR-A/B/C tests but covers the end-to-end: `--workspace=/var/run/docker.sock → workspace_forbidden`. Final commit of PR-D adds Test 23 (negative: full posture probe — one exec that tries mknod, ptrace, setuid, write-/etc, fork-bomb, and asserts every attempt fails).

**What this phase does NOT deliver (NOT IN SCOPE):**

- **Agent image rebase beyond UID confirmation.** A full security rebase of pi-mono — pinned base image refreshes, apt CVE sweeps, node_modules vulnerability audit, swap to distroless — is a separate follow-up. beta.2 only adds the daemon-side `HostConfig.User: "1000:1000"` enforcement and an optional runtime assertion in `run.mjs`.
- **Rootless Docker daemon mode.** An operator-side decision that requires configuring Docker Engine or Docker Desktop to run rootless. dclaw cannot enable this from its own code. We can recommend it in `docs/workspace-root.md` but do not implement.
- **Network egress allowlisting inside the container.** `protocol.AgentCreateParams` already has a defined-but-unused `EgressAllowlist` field (declared in `WorkerSpawn` at `internal/protocol/messages.go:281`; `AgentCreateParams` does not yet). Wiring an allowlist requires either a userspace proxy inside the container or iptables rules in the sandbox, and the iptables approach needs `CAP_NET_ADMIN` which beta.2 drops. Do later as its own phase.
- **Switching to gVisor / Kata / Firecracker runtime.** Deployer choice, not code. We can recommend `--runtime=runsc` in `docs/workspace-root.md` but do not wire a dclaw-side flag.
- **AppArmor / SELinux profile beyond Docker's default.** Docker's default AppArmor profile (`docker-default`) applies automatically on AppArmor-enabled hosts. Custom profiles are a follow-up.
- **User-namespace remapping.** `HostConfig.UsernsMode` controls whether the container runs in a private user namespace (`"private"`) or shares the host's (`"host"`, default). Docker Desktop on macOS always runs in a Linux VM and handles this at the VM level; on Linux Engine, rootless mode is the modern path. Enabling `UsernsMode: "private"` requires `/etc/subuid` and `/etc/subgid` to be configured on the host — outside dclaw's control. beta.2 leaves `UsernsMode` at the default and documents the tradeoff. See §11 Q1.
- **Custom (tighter-than-default) seccomp profile.** beta.2 pins Docker's default profile explicitly. A tighter policy denying `keyctl`, `add_key`, `personality`, `ptrace` (already denied by default for non-root, but belt-and-suspenders) is a follow-up PR. See §11 Q2.
- **Per-agent resource limits (`Memory`, `CPUQuota`).** Real DoS protection but orthogonal to escape-surface hardening. Follow-up.

**Sequence relative to the product roadmap:**

```
alpha.4.1               → hotfixes (shipped 2026-04-18)
                                            ← machine wipe 2026-04-18 ←
beta.1-paths-hardening  → --workspace validation + state-dir consolidation (shipped 2026-04-22)
beta.2-sandbox-hardening → container escape surface ← THIS PLAN
beta.1                  → logs view + toasts + chat history persistence (pre-wipe content, to be re-derived)
v0.3.0                  → GA
```

---

## 2. Dependencies

**No new Go dependencies.** Every field beta.2 sets (`CapDrop`, `SecurityOpt`, `ReadonlyRootfs`, `Tmpfs`, `User`, `PidsLimit`) already exists on `github.com/docker/docker/api/types/container.HostConfig` at `github.com/docker/docker v26.1.3+incompatible` (per `go.mod:10`). Verified against the SDK — `HostConfig.CapDrop`, `HostConfig.SecurityOpt`, `HostConfig.ReadonlyRootfs`, `HostConfig.Tmpfs`, `HostConfig.Resources.PidsLimit` are all `v26.1.3` fields.

**No migration.** All changes are Docker-side container posture; state.db schema is unaffected.

**No wire protocol changes.** `protocol.AgentCreateParams` is unchanged; `Agent` wire shape is unchanged. The hardening is entirely a property of how `dclawd` constructs the container.

**Promoted indirect → direct (optional).** None. `golang.org/x/sys` was already promoted to direct in beta.1 PR-C. No further promotions.

After any inadvertent `go.mod` touch, run `go mod tidy` from the repo root.

---

## 3. Sequencing

**PR dependency graph:**

```
PR-A  (caps drop + no-new-privileges + seccomp + PidsLimit, ~120 lines)
   ↓
PR-B  (ReadonlyRootfs + tmpfs overlays, ~100 lines; depends on A's test harness)
   ↓
PR-C  (non-root UID enforcement + optional run.mjs guard, ~60 lines)
   ↓
PR-D  (docker.sock denylist + smoke Tests 22/23 + doc sweep, ~120 lines)
```

- **PR-A is the gate.** It introduces the recording `DockerClient` test harness that B, C, and D reuse to assert additional `HostConfig` fields. Shipping A first lets B/C be pure additions to the posture rather than inventing test scaffolding.
- **PR-B depends on PR-A** because ReadonlyRootfs changes the audit baseline for pi-mono's write paths — running PR-B's smoke tests requires the posture from PR-A already applied to rule out "the container worked before we added ReadonlyRootfs because caps were still wide open" confounds.
- **PR-C depends on PR-B** because the non-root UID change interacts with ReadonlyRootfs: uid 1000 inside the container cannot `chown` the tmpfs overlay (no `CAP_CHOWN`, dropped by PR-A; no write to `/`, blocked by PR-B). The integration tests for PR-C want the full lower-priv posture in place.
- **PR-D is the tail.** Adds one line to the denylist slice + one comprehensive end-to-end smoke test that exercises every hardening dimension at once.

All four PRs reviewed together as one batched cycle matching the beta.1 cadence; merged in the sequence above.

---

## 4. Per-PR Spec

### 4.1 PR-A — Capability Drop + `no-new-privileges` + Seccomp + PidsLimit

**Goal:** Drop all Linux capabilities, block setuid/setgid privilege escalation, pin the default seccomp profile, cap PIDs. Zero user-visible change; the agent still runs. Attack surface shrinks by ~80% of the classic container-escape CVE corpus.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` | MODIFIED | `CreateAgent` (lines 90-136): add four fields to `HostConfig`. (1) `CapDrop: []string{"ALL"}`. (2) `SecurityOpt: []string{"no-new-privileges:true", "seccomp=default"}` — the `seccomp=default` token is Docker Engine's built-in profile selector; explicit is safer than relying on daemon-side config. (3) `Resources: container.Resources{PidsLimit: pidsLimitPtr(256)}`. (4) New package-level helper `func pidsLimitPtr(n int64) *int64`. Net lines: ~+25. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` | MODIFIED | New package-level constants at the top of the file (after the import block, before `ErrDockerFailure`): `// Container posture constants (beta.2-sandbox-hardening). Held as package-level named constants so tests can assert the exact shape without string-matching the implementation.` Define `DefaultCapDrop = []string{"ALL"}`, `DefaultSecurityOpt = []string{"no-new-privileges:true", "seccomp=default"}`, `DefaultPidsLimit int64 = 256`. Use these inside `CreateAgent` so the test harness can reference them. Net lines: ~+10. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker_test.go` | NEW | New file. Defines an in-package test harness `type captureClient struct { lastCfg *container.Config; lastHostCfg *container.HostConfig }` plus a stubbed Docker API client interface the tests can inject. Test function `TestCreateAgentAppliesBeta2HardeningPosture` asserts after a `CreateAgent` call: `assert.Equal(t, []string{"ALL"}, gotHostCfg.CapDrop)`, `assert.Contains(t, gotHostCfg.SecurityOpt, "no-new-privileges:true")`, `assert.Contains(t, gotHostCfg.SecurityOpt, "seccomp=default")`, `require.NotNil(t, gotHostCfg.Resources.PidsLimit)`, `assert.Equal(t, int64(256), *gotHostCfg.Resources.PidsLimit)`. Uses `testing/quick`-style table for the happy path + negative path (empty spec.Workspace should still apply all four). Net lines: ~+180. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` | MODIFIED | To support the test harness, refactor `NewDockerClient` to return a `*DockerClient` whose `cli` field is a minimal interface `type dockerAPI interface { ContainerCreate(...) ContainerStart(...) ContainerStop(...) ... }` that `*client.Client` satisfies. This is a mechanical wrap; all existing call sites in this file continue to work unchanged. Net lines: ~+15 (new interface + one-line signature change on `cli`). **Alternative discussed in §11 Q3:** skip the interface refactor and use `testcontainers-go` or a real Docker socket in CI. Decision: keep the refactor; it is cheap and the mock test is what guarantees the posture regression-tests. |
| `/Users/macmini/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` | MODIFIED | Add Test 17 (capability probe), Test 18 (seccomp probe), Test 19 (fork-bomb probe). Each uses `dclaw agent exec` to run a command inside the container and asserts the expected failure. Exact bodies in §4.1a. |

**§4.1a — smoke Test bodies for PR-A:**

```bash
echo "--- Test 17: capability drop — CAP_MKNOD unavailable ---"
# Inside the container, try to create a block device. With CAP_MKNOD dropped,
# mknod should fail with EPERM. Without caps, even uid 0 inside the container
# cannot do this. Under pre-beta.2 posture, `mknod /tmp/dev b 8 0` would
# succeed (exposing the host's /dev/sda raw-device surface).
STATE_DIR_T17=$(mktemp -d -t dclaw-smoke-t17-XXXXXXXX)
case "$STATE_DIR_T17" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T17=$STATE_DIR_T17 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T17="$STATE_DIR_T17/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" daemon start || fail "t17-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" agent create smoke-cap-probe \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T17" || fail "t17-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" agent start smoke-cap-probe || fail "t17-start-agent"
set +e
OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" agent exec smoke-cap-probe \
  -- mknod /tmp/dev-sda b 8 0 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 17: mknod succeeded inside container; CAP_MKNOD not dropped (output: $OUT)"
fi
echo "$OUT" | grep -qi "operation not permitted\|eperm" \
  || fail "Test 17: expected EPERM from mknod, got: $OUT"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" daemon stop >/dev/null 2>&1 || true
rm -rf "${STATE_DIR_T17:?refuse empty}"
pass "CAP_MKNOD dropped (mknod failed with EPERM)"

echo "--- Test 18: seccomp profile — unshare(CLONE_NEWUSER) denied ---"
# The default Docker seccomp profile denies unshare with CLONE_NEWUSER for
# unprivileged tasks. This test exercises one concrete denied syscall; a
# full profile regression suite lives in `syscall_blocklist_test.sh` under
# follow-ups. Under pre-beta.2, whether this succeeded depended on the
# daemon config — post-beta.2, we pin `seccomp=default` explicitly.
STATE_DIR_T18=$(mktemp -d -t dclaw-smoke-t18-XXXXXXXX)
...
set +e
OUT=$("$DCLAW_BIN" ... agent exec smoke-seccomp-probe -- unshare -U -r whoami 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 18: unshare -U succeeded; seccomp default not applied (output: $OUT)"
fi
echo "$OUT" | grep -qi "operation not permitted\|eperm" \
  || fail "Test 18: expected EPERM from unshare, got: $OUT"
...
pass "seccomp default profile applied (unshare(CLONE_NEWUSER) denied)"

echo "--- Test 19: PidsLimit — fork bomb capped at 256 ---"
# Spawn 300 sleeping processes. With PidsLimit=256, the 257th fork fails.
# We assert the count is bounded, not that we hit exactly 256 (kernel may
# count pid-1 and the shell itself).
STATE_DIR_T19=$(mktemp -d -t dclaw-smoke-t19-XXXXXXXX)
...
set +e
OUT=$("$DCLAW_BIN" ... agent exec smoke-pids-probe -- sh -c \
  'i=0; while [ "$i" -lt 300 ]; do (sleep 30) & i=$((i+1)); done; jobs | wc -l' 2>&1)
EX=$?
set -e
JOB_COUNT=$(echo "$OUT" | tail -n1)
# Accept any result < 300, as the kernel counts pid-1 + shell + the sleeps.
if [ "$JOB_COUNT" -ge 300 ] 2>/dev/null; then
  fail "Test 19: PidsLimit not enforced; spawned $JOB_COUNT processes (output: $OUT)"
fi
...
pass "PidsLimit enforced (spawned $JOB_COUNT < 300 processes before EAGAIN)"
```

**Test plan:**

- `go test ./internal/sandbox/...` — new `TestCreateAgentAppliesBeta2HardeningPosture` table with three rows: (1) with workspace bind-mount, (2) without, (3) with env + labels populated. Each row must pass after inspecting the captured `HostConfig`.
- `go test ./...` — regression; nothing should break.
- `go vet ./...` clean.
- `go build ./cmd/dclaw ./cmd/dclawd` both compile.
- Integration smoke: `./scripts/smoke-daemon.sh` Tests 1-16 (beta.1 suite) still green, plus new Tests 17-19.

**Acceptance criteria:**

1. `go test ./internal/sandbox/...` passes with the three-row posture regression test.
2. `internal/sandbox/docker.go` `CreateAgent` has exactly four new `HostConfig` fields: `CapDrop`, `SecurityOpt`, `Resources.PidsLimit`, and (preserved from beta.1) `Mounts`. Grep `grep -E 'CapDrop|SecurityOpt|PidsLimit' internal/sandbox/docker.go` returns ≥ 4 lines.
3. `./scripts/smoke-daemon.sh` Tests 17/18/19 green on a host with Docker reachable + `dclaw-agent:v0.1` built.
4. No wire-protocol changes: `git diff 02d4119 -- internal/protocol/messages.go` returns empty.
5. No new migrations: `ls internal/store/migrations/ | wc -l` returns 2 (unchanged from beta.1).

**Rollout risk:** Low. Every field added is a hardening flag; none add new code paths in the happy case. If `CapDrop: ALL` breaks something pi-mono depends on (most plausibly `CAP_DAC_READ_SEARCH` for reading config files outside the user's uid), the workspace write path would fail — but pi-mono runs as `node` (uid 1000) writing to `/workspace` owned by `node`, so DAC applies normally with no caps needed. Seccomp default is Docker's long-standing default, so turning it on explicitly cannot regress vs. the common case. PidsLimit 256 is 50× pi-mono's steady-state process count.

**Rollback:** Revert the merge commit. No state-persistence implications; container posture is per-`CreateAgent`-call and applies fresh on next agent create.

---

### 4.2 PR-B — ReadonlyRootfs + Tmpfs Overlays

**Goal:** Container rootfs becomes non-writable except where we explicitly grant writable tmpfs. Workspace writes continue to flow through the bind-mount. If the agent attempts to modify any system file — `/etc/passwd`, `/usr/lib/*`, `/app/run.mjs` itself — the write fails with `EROFS`.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` | MODIFIED | `CreateAgent`: add `ReadonlyRootfs: true` and `Tmpfs: map[string]string{"/tmp": "rw,noexec,nosuid,nodev,size=64m", "/run": "rw,noexec,nosuid,nodev,size=8m"}` to the `HostConfig`. Hoist the tmpfs map into a package-level `DefaultTmpfs` alongside `DefaultCapDrop`, etc. Net lines: ~+15. Tmpfs sizes: `/tmp 64m` covers pi-mono's typical scratch (shell history, a few JSON working files); `/run 8m` covers systemd/tini runtime sockets and lock files. Both `noexec+nosuid+nodev` because the rootfs is already read-only so anything dropped in these tmpfses should never be executed. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker_test.go` | MODIFIED | Extend `TestCreateAgentAppliesBeta2HardeningPosture` to also assert `ReadonlyRootfs == true` and `Tmpfs["/tmp"]` + `Tmpfs["/run"]` contain the expected mount options. New row: verify both Tmpfs entries include `noexec,nosuid,nodev`. Net lines: ~+40. |
| `/Users/macmini/workspace/agents/atlas/dclaw/agent/Dockerfile` | MODIFIED (optional, see §11 Q4) | If the audit finds pi-mono writes to a path outside `/tmp`, `/run`, or `/workspace`, add a tmpfs-overlay declaration at `CreateAgent` and document the reason. Current expectation: `node` user home `/home/node` does not exist (Debian slim creates `node` without a home), pi-mono runs with `cwd=/workspace`, `npm` cache would go to `/root/.npm` but we never run npm at runtime. Audit checklist below. |
| `/Users/macmini/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` | MODIFIED | Add Test 20 (rootfs write probe): two `agent exec` calls trying `touch /etc/sandbox-breach` and `touch /opt/breach`, both must fail with `EROFS` or "read-only file system". Also positive check: `touch /tmp/ok` and `touch /workspace/ok` both succeed. Exact body in §4.2a. |

**§4.2 pi-mono write-path audit.** Before enabling `ReadonlyRootfs: true`, enumerate every path pi-mono writes at runtime. Read `/Users/macmini/workspace/agents/atlas/dclaw/agent/run.mjs` (done; it just spawns `pi -p --no-session`) and trace pi's runtime writes. Known writes:

- `/workspace/*` — bind-mount, writable regardless of ReadonlyRootfs. OK.
- `/tmp/*` — scratch space. Covered by tmpfs mount. OK.
- `/root/.pi/agent/*` — pi's session cache. **Suppressed by `--no-session`** per `agent/run.mjs:29`. Confirmed not written under current entry point.
- `/app/node_modules/.cache/*` — npm's internal cache. Only written during `npm ci` (build-time), not runtime. OK.
- `$HOME/.npm/*` — not relevant at runtime.
- `/etc/resolv.conf`, `/etc/hosts` — written by Docker daemon itself, mounted by Docker into the container pre-ReadonlyRootfs. Post-ReadonlyRootfs, Docker continues to mount these as individual bind-mounts that remain writable to the daemon but appear to the container as part of the rootfs (Docker handles the mount.propagation automatically). Verified in Docker SDK docs.

If the audit surfaces an unexpected write path, add it to `DefaultTmpfs` and document in this section. See §11 Q4.

**§4.2a — Test 20 body:**

```bash
echo "--- Test 20: ReadonlyRootfs — /etc + /opt not writable; /tmp + /workspace writable ---"
STATE_DIR_T20=$(mktemp -d -t dclaw-smoke-t20-XXXXXXXX)
case "$STATE_DIR_T20" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T20=$STATE_DIR_T20 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T20="$STATE_DIR_T20/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" daemon start || fail "t20-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent create smoke-rootfs-probe \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T20" || fail "t20-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent start smoke-rootfs-probe || fail "t20-start-agent"

# Negative: /etc write must fail with EROFS
set +e
OUT=$("$DCLAW_BIN" ... agent exec smoke-rootfs-probe -- touch /etc/sandbox-breach 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 20 /etc: ReadonlyRootfs not applied; /etc is writable (output: $OUT)"
fi
echo "$OUT" | grep -qi "read-only\|erofs" \
  || fail "Test 20 /etc: expected EROFS, got: $OUT"

# Negative: /opt write must fail with EROFS
set +e
OUT=$("$DCLAW_BIN" ... agent exec smoke-rootfs-probe -- touch /opt/sandbox-breach 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 20 /opt: ReadonlyRootfs not applied; /opt is writable (output: $OUT)"
fi

# Positive: /tmp write must succeed (tmpfs overlay)
"$DCLAW_BIN" ... agent exec smoke-rootfs-probe -- touch /tmp/ok \
  || fail "Test 20 /tmp: tmpfs overlay missing; /tmp not writable"

# Positive: /workspace write must succeed (bind-mount)
"$DCLAW_BIN" ... agent exec smoke-rootfs-probe -- touch /workspace/ok \
  || fail "Test 20 /workspace: bind-mount missing; /workspace not writable"

...
pass "ReadonlyRootfs enforced (/etc, /opt non-writable); /tmp + /workspace writable"
```

**Test plan:**

- `go test ./internal/sandbox/...` — extended posture test row for ReadonlyRootfs + Tmpfs entries.
- `go test ./...` regression.
- `./scripts/smoke-daemon.sh` 1-20 green.
- Manual: `./bin/dclaw agent exec <name> -- sh -c 'echo x > /etc/motd'` returns exit != 0 with stderr containing "Read-only file system".

**Acceptance criteria:**

1. `HostConfig.ReadonlyRootfs == true` and `HostConfig.Tmpfs` contains exactly the two entries `{/tmp, /run}` per the `TestCreateAgentAppliesBeta2HardeningPosture` assertions.
2. Test 20 green on the smoke host.
3. pi-mono's `--no-session` happy path still succeeds end-to-end (Test 13 in the existing smoke suite still passes when `ANTHROPIC_API_KEY` is set).
4. Tmpfs options include `noexec,nosuid,nodev` — verified by string-match on `gotHostCfg.Tmpfs["/tmp"]` in the Go test.

**Rollout risk:** Medium. If pi-mono's runtime write-path audit missed a dependency (most likely candidate: pi-mono's config dir at some unexpected location), the agent will fail with `EROFS` at first real chat invocation. Mitigation: PR-B ships after PR-A and the independent smoke-test run against `dclaw-agent:v0.1` confirms the write-path list before flipping `ReadonlyRootfs: true` in code. Rollback is one-line (remove the field).

**Rollback:** Revert the merge commit. Containers created before the revert retain their existing posture; new containers go back to writable rootfs.

---

### 4.3 PR-C — Non-Root UID Enforcement

**Goal:** Even if a future `dclaw-agent` image regression sets `USER root` or omits `USER`, the daemon-side `HostConfig.User: "1000:1000"` forces uid/gid 1000 at container start time. Belt-and-suspenders since the current image already has `USER node`.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` | MODIFIED | `CreateAgent`: add `User: DefaultContainerUser` to `container.Config` (not `HostConfig` — the `User` field lives on the Config in the Docker SDK for v26). `const DefaultContainerUser = "1000:1000"` at the top of the file alongside the other beta.2 constants. The format `"<uid>:<gid>"` is the canonical Docker form; `"1000"` alone would inherit the group from the image, but explicit is safer. Net lines: ~+5. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker_test.go` | MODIFIED | Extend the posture assertion table to check `gotCfg.User == "1000:1000"`. Net lines: ~+5. |
| `/Users/macmini/workspace/agents/atlas/dclaw/agent/run.mjs` | MODIFIED (optional) | Add at the top of the file, after the shebang but before any other code: `if (process.getuid() === 0) { console.error("error: dclaw-agent must not run as uid 0; daemon should have applied --user=1000:1000"); process.exit(70); }`. This is the second line of defense: if both the image `USER` directive AND the daemon-side `HostConfig.User` are somehow bypassed, the agent entry point refuses to run. Net lines: ~+4. See §11 Q5 for the discussion of whether this is worth the complexity. |
| `/Users/macmini/workspace/agents/atlas/dclaw/agent/Dockerfile` | MODIFIED (optional) | Clarifying comment (not functional): add a `# INVARIANT: container must run as uid 1000 (node). Daemon enforces via HostConfig.User; image also ships USER node.` Above line 45. No code change. |
| `/Users/macmini/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` | MODIFIED | Test 21 (uid probe): `agent exec <name> -- id -u` returns `1000`. Exact body in §4.3a. |

**§4.3a — Test 21 body:**

```bash
echo "--- Test 21: non-root UID — id -u returns 1000 ---"
STATE_DIR_T21=$(mktemp -d -t dclaw-smoke-t21-XXXXXXXX)
case "$STATE_DIR_T21" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T21=$STATE_DIR_T21 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T21="$STATE_DIR_T21/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T21" --daemon-socket "$SOCKET_T21" daemon start || fail "t21-start"
"$DCLAW_BIN" ... agent create smoke-uid-probe --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T21" || fail "t21-create"
"$DCLAW_BIN" ... agent start smoke-uid-probe || fail "t21-agent-start"

UID_OBSERVED=$("$DCLAW_BIN" ... agent exec smoke-uid-probe -- id -u 2>/dev/null | tr -d '[:space:]')
if [ "$UID_OBSERVED" != "1000" ]; then
  fail "Test 21: expected uid=1000 inside container, got '$UID_OBSERVED'"
fi

GID_OBSERVED=$("$DCLAW_BIN" ... agent exec smoke-uid-probe -- id -g 2>/dev/null | tr -d '[:space:]')
if [ "$GID_OBSERVED" != "1000" ]; then
  fail "Test 21: expected gid=1000 inside container, got '$GID_OBSERVED'"
fi

...
pass "non-root UID enforced (uid=1000 gid=1000 inside container)"
```

**Test plan:**

- `go test ./internal/sandbox/...` — posture assertion extended.
- `./scripts/smoke-daemon.sh` Test 21 green.
- Manual: `./bin/dclaw agent exec <name> -- whoami` returns `node` (or `1000` if `/etc/passwd` doesn't have the name mapping — it does in the upstream image).

**Acceptance criteria:**

1. `HostConfig.User == "1000:1000"` asserted by `TestCreateAgentAppliesBeta2HardeningPosture`.
2. Test 21 green.
3. Workspace bind-mount still writable from inside the container (uid 1000 on the container side, operator's uid on the host side — Docker maps this by default).
4. `agent/run.mjs` optional uid assertion, if adopted, does not break the existing entry-point behavior for any valid input.

**Rollout risk:** Low. The current image already runs as `node` (uid 1000). Setting `HostConfig.User` to `"1000:1000"` re-applies what the image already declared. The rare case where the image `USER` directive and the `HostConfig.User` field conflict — e.g., image says `USER root` but `HostConfig.User` says `"1000:1000"` — Docker resolves in favor of the `HostConfig.User` value. The failure mode is "agent can no longer write to `/workspace` because the host-side uid doesn't match the container-side uid." Mitigated because the existing smoke tests already exercise workspace writes with uid 1000.

**Rollback:** Revert. One field.

---

### 4.4 PR-D — docker.sock Denylist + End-to-End Posture Probe

**Goal:** Explicit denylist entries for the Docker control socket (all three common locations) so `--workspace=/var/run/docker.sock` is rejected pre-mount with a clearer error than "`/var` descendant denylist match". Final smoke test that exercises every beta.2 hardening dimension in one call.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/paths/policy.go` | MODIFIED | `DefaultDenylist` (lines 48-64): append three entries: `"/var/run/docker.sock"`, `"/run/docker.sock"`, `"/Users/macmini/Library/Containers/com.docker.docker/Data/docker-raw.sock"` (Docker Desktop macOS per-user path; note this requires a glob match, not a literal, because `macmini` varies per user — see §4.4a for the resolution). Net lines: ~+8. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/paths/policy_test.go` | MODIFIED | Add 4 rows to the validator table: (33) `/var/run/docker.sock` → forbidden, (34) `/run/docker.sock` → forbidden, (35) `/Users/<user>/Library/Containers/com.docker.docker/Data/docker-raw.sock` → forbidden via the per-user glob (see §4.4a), (36) `/var/run/docker.sock/` (trailing slash) → forbidden. Net lines: ~+40. |
| `/Users/macmini/workspace/agents/atlas/dclaw/docs/workspace-root.md` | MODIFIED | Add a "Docker socket" subsection to the "What is workspace-root?" section listing the three paths and the rationale ("mounting the Docker socket into a container is equivalent to granting root on the host"). Net lines: ~+15. |
| `/Users/macmini/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` | MODIFIED | Test 22 (docker.sock rejection): `dclaw agent create evil --workspace=/var/run/docker.sock ...` → exit 65, `workspace_forbidden` in JSON. Test 23 (full posture probe): single `agent exec` that runs a comprehensive script asserting mknod fails, setuid fails (tries `chmod u+s /tmp/x`), unshare fails, fork >256 processes caps, rootfs write fails, uid == 1000. Exact bodies in §4.4b. |

**§4.4a — Docker Desktop macOS socket path resolution.**

The Docker Desktop on macOS socket is `/Users/<user>/Library/Containers/com.docker.docker/Data/docker-raw.sock`. The `<user>` component varies per operator, so a literal string in `DefaultDenylist` would only catch one user. Three options:

1. **Hard-code common paths** — too brittle; any future change in Docker Desktop's layout breaks this.
2. **Glob against `/Users/*/Library/Containers/com.docker.docker/Data/docker-raw.sock`** — requires the policy engine to grow glob support. Adds ~50 lines.
3. **Match by suffix `docker-raw.sock` AND path containing `/Library/Containers/com.docker.docker/`** — simpler, no glob lib needed.

**Decided: option 3.** Extend `Policy.Validate` to check for the two literal substrings `"/Library/Containers/com.docker.docker/"` and `"docker-raw.sock"` both appearing in the canonical path. If both match, reject with reason "Docker Desktop control socket". This is a ~10-line addition in `policy.go` and requires no new dependency. Rows 35 in `policy_test.go` exercises this.

`/var/run/docker.sock` and `/run/docker.sock` are Linux-native literal paths and go into `DefaultDenylist` directly.

**§4.4b — Test 22 + 23 bodies:**

```bash
echo "--- Test 22: docker.sock rejected as --workspace ---"
STATE_DIR_T22=$(mktemp -d -t dclaw-smoke-t22-XXXXXXXX)
case "$STATE_DIR_T22" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T22=$STATE_DIR_T22 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T22="$STATE_DIR_T22/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T22" --daemon-socket "$SOCKET_T22" daemon start || fail "t22-start"
# Use -o json for the machine-readable assertion per Test 14 precedent.
set +e
T22_OUT=$("$DCLAW_BIN" -o json --state-dir "$STATE_DIR_T22" --daemon-socket "$SOCKET_T22" agent create evil-sock \
  --image=dclaw-agent:v0.1 --workspace=/var/run/docker.sock 2>&1)
T22_EXIT=$?
set -e
if [ "$T22_EXIT" -ne 65 ]; then
  fail "Test 22 expected exit 65, got exit $T22_EXIT (output: $T22_OUT)"
fi
echo "$T22_OUT" | grep -q '"error": *"workspace_forbidden"' \
  || fail "Test 22 expected 'workspace_forbidden' JSON, got: $T22_OUT"
# Confirm the reason mentions docker.sock specifically.
echo "$T22_OUT" | grep -qi "docker\|socket" \
  || fail "Test 22 expected reason to mention docker/socket, got: $T22_OUT"
...
pass "docker.sock rejected as --workspace (exit 65 + workspace_forbidden)"

echo "--- Test 23: full beta.2 posture probe ---"
# One agent, six probes in a single exec script. Each probe must fail with
# the expected errno. If ANY probe succeeds, fail the whole test loudly.
STATE_DIR_T23=$(mktemp -d -t dclaw-smoke-t23-XXXXXXXX)
case "$STATE_DIR_T23" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T23=$STATE_DIR_T23 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T23="$STATE_DIR_T23/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T23" --daemon-socket "$SOCKET_T23" daemon start || fail "t23-start"
"$DCLAW_BIN" ... agent create smoke-posture --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T23" || fail "t23-create"
"$DCLAW_BIN" ... agent start smoke-posture || fail "t23-agent-start"

POSTURE_SCRIPT='
set -e
FAILS=0
# 1. mknod must fail (CAP_MKNOD dropped)
if mknod /tmp/dev b 8 0 2>/dev/null; then
  echo "BREACH: mknod succeeded"; FAILS=$((FAILS+1))
fi
# 2. setuid chmod must fail or be no-op (no-new-privileges)
touch /tmp/x 2>/dev/null && chmod u+s /tmp/x 2>/dev/null
if [ -u /tmp/x ]; then
  echo "BREACH: setuid bit set despite no-new-privileges"; FAILS=$((FAILS+1))
fi
# 3. unshare(CLONE_NEWUSER) must fail (seccomp default)
if unshare -U -r id 2>/dev/null; then
  echo "BREACH: unshare(CLONE_NEWUSER) succeeded"; FAILS=$((FAILS+1))
fi
# 4. rootfs write to /etc must fail (ReadonlyRootfs)
if touch /etc/breach 2>/dev/null; then
  echo "BREACH: /etc writable"; FAILS=$((FAILS+1))
fi
# 5. uid must be 1000
UID_SELF=$(id -u)
if [ "$UID_SELF" != "1000" ]; then
  echo "BREACH: uid=$UID_SELF (expected 1000)"; FAILS=$((FAILS+1))
fi
# 6. PidsLimit — spawn 300 and confirm we hit the cap
i=0
while [ "$i" -lt 300 ]; do (sleep 10 &) 2>/dev/null || break; i=$((i+1)); done
if [ "$i" -ge 300 ]; then
  echo "BREACH: spawned $i processes (no PidsLimit)"; FAILS=$((FAILS+1))
fi
exit $FAILS
'
set +e
OUT=$("$DCLAW_BIN" ... agent exec smoke-posture -- sh -c "$POSTURE_SCRIPT" 2>&1)
EX=$?
set -e
if [ "$EX" -ne 0 ]; then
  fail "Test 23: $EX posture probe(s) failed:
$OUT"
fi
...
pass "full beta.2 posture probe (all 6 dimensions enforced)"
```

**Test plan:**

- `go test ./internal/paths/...` — 4 new rows passing.
- `./scripts/smoke-daemon.sh` Test 22 + 23 green.
- Grep: `grep -E 'docker.sock|docker-raw.sock' internal/paths/policy.go` returns 3 entries.

**Acceptance criteria:**

1. Validator rejects all three docker-socket path variants.
2. Test 22 green.
3. Test 23 (end-to-end posture) green.
4. `docs/workspace-root.md` mentions Docker socket in the denylist section.

**Rollout risk:** Low. Pure addition to the denylist. Any operator currently trying to use `/var/run/docker.sock` as a workspace is already catastrophically misusing the tool; failing closed is desirable.

**Rollback:** Revert.

---

## 5. Modified Files Diff Summary

| File | Change type | Net lines | Phase |
|---|---|---|---|
| `internal/sandbox/docker.go` | caps/seccomp/pids/constants | +50 | PR-A |
| `internal/sandbox/docker_test.go` | new posture harness + tests | +180 | PR-A |
| `scripts/smoke-daemon.sh` | Tests 17-19 | +120 | PR-A |
| `internal/sandbox/docker.go` | ReadonlyRootfs + Tmpfs | +15 | PR-B |
| `internal/sandbox/docker_test.go` | Tmpfs posture assertions | +40 | PR-B |
| `scripts/smoke-daemon.sh` | Test 20 | +40 | PR-B |
| `internal/sandbox/docker.go` | User: 1000:1000 | +8 | PR-C |
| `internal/sandbox/docker_test.go` | User posture assertion | +5 | PR-C |
| `agent/run.mjs` | optional uid-0 guard | +4 | PR-C |
| `agent/Dockerfile` | invariant comment | +1 | PR-C |
| `scripts/smoke-daemon.sh` | Test 21 | +30 | PR-C |
| `internal/paths/policy.go` | docker.sock denylist + helper | +20 | PR-D |
| `internal/paths/policy_test.go` | 4 new validator rows | +40 | PR-D |
| `docs/workspace-root.md` | docker.sock subsection | +15 | PR-D |
| `scripts/smoke-daemon.sh` | Tests 22-23 | +100 | PR-D |

**Total estimated diff: ~+670 lines across 4 PRs** (target ~120 + ~100 + ~60 + ~120 = 400; actual is larger due to the posture test harness in PR-A and the comprehensive posture probe in PR-D Test 23).

---

## 6. Threat Model

beta.2 closes the escape vectors the code-review flagged. Walk each one:

### 6.1 `mknod` + raw block device access

**Pre-beta.2:** Container runs with default Docker caps, which includes `CAP_MKNOD` (allows creating block/char devices) and `CAP_SYS_ADMIN` is *dropped* by default. But the default posture still leaves `CAP_MKNOD` + `CAP_DAC_OVERRIDE` intact. `CAP_MKNOD` alone is enough: `mknod /tmp/sda b 8 0` creates a block device pointing at the host's first SCSI disk. Reading `/tmp/sda` exfiltrates raw sectors from the host disk, including `/etc/shadow`, SSH host keys, anything outside `/workspace` but on the root filesystem of the host VM (or the real host on Linux).

**Reference CVE:** None needed — this is a documented primitive. See Docker docs "Runtime privilege and Linux capabilities" warning against leaving `CAP_MKNOD` in place for untrusted workloads.

**Post-beta.2 mitigation:** PR-A's `CapDrop: ALL` removes `CAP_MKNOD`. `mknod` returns `EPERM`. Tested by Test 17.

### 6.2 `ptrace`-based process injection

**Pre-beta.2:** `CAP_SYS_PTRACE` is NOT in Docker's default cap set — already dropped. However, non-root `ptrace` of the container's own processes is still allowed, and default seccomp lets `ptrace(2)` through for attacking other container processes. If a future dclaw feature spawns a worker process with elevated permissions inside the same container, an agent running as `node` could ptrace into the worker.

**Post-beta.2 mitigation:** PR-A's `SecurityOpt: "seccomp=default"` blocks `ptrace` for non-capability workloads via the default Docker seccomp profile. Test 23 probe #3 (`unshare -U`) confirms the default profile is active; `ptrace` follows the same profile. Additionally, Yama LSM on the host side would block `ptrace` of non-parent processes (ptrace_scope=1), but that is a host-side setting.

### 6.3 `keyctl` kernel keyring abuse

**Pre-beta.2:** `keyctl`, `add_key`, `request_key` are in the seccomp default-allow list because the profile was last revised in 2019 and kernel keyring exploits (CVE-2016-0728, CVE-2022-0185) postdate some profile editions. Default Docker profile *does* deny these for unprivileged — but only if the profile is explicitly applied.

**Post-beta.2 mitigation:** PR-A's explicit `SecurityOpt: "seccomp=default"` ensures the profile is loaded even if the Docker daemon's global config disables it. Custom-profile extension (deny all three syscalls explicitly) is §11 Q2 follow-up.

### 6.4 `setuid`/`setgid` privilege escalation

**Pre-beta.2:** If the rootfs contains a setuid binary (e.g., `/usr/bin/mount` in `node:22-bookworm-slim`) and the agent can reach it, executing it could escalate. `no-new-privileges` is NOT set by default.

**Reference CVE:** CVE-2019-5736 (runc escape via /proc/self/exe overwrite) — mitigated by `no-new-privileges` + read-only rootfs combo. CVE-2022-0185 (user-ns-based fs_context) — mitigated by seccomp default + `no-new-privileges`.

**Post-beta.2 mitigation:** PR-A's `SecurityOpt: "no-new-privileges:true"` prevents `execve` from granting new privileges via setuid/setgid bits. PR-B's `ReadonlyRootfs: true` prevents writing new setuid binaries to the rootfs. PR-C's `HostConfig.User: "1000:1000"` means even if a setuid binary existed, it would execute as uid 1000 (still). Tested by Test 23 probe #2.

### 6.5 fork-bomb / PID DoS

**Pre-beta.2:** No `PidsLimit`. An agent that spawns `(:(){ :|:& };:)` exhausts the host's PID table. On a macOS dev machine this can crash the Docker Desktop VM; on Linux it crashes the host kernel.

**Post-beta.2 mitigation:** PR-A's `PidsLimit: 256`. The 257th fork returns `EAGAIN`. pi-mono's steady-state process count is ~5 (pi, npm, node, plus the occasional spawned tool). 256 is 50× headroom. Tested by Test 19.

### 6.6 Rootfs tampering

**Pre-beta.2:** Writable rootfs. An agent could `echo "..." >> /etc/passwd` (blocked by DAC since it runs as `node`, uid 1000), BUT could overwrite `/app/run.mjs` (chown'd to `node` at build time) — persisting malicious entry-point code for the next container exec, except that containers are replaced per-agent-start. Higher-severity write: overwriting `$HOME/.pi/agent/session.jsonl` to poison the next chat invocation. `--no-session` mitigates; but a defense-in-depth stance says rootfs should be read-only.

**Post-beta.2 mitigation:** PR-B's `ReadonlyRootfs: true`. All writes outside `/tmp`, `/run`, and `/workspace` return `EROFS`. Tested by Test 20.

### 6.7 docker.sock as a Trojan workspace

**Pre-beta.2:** The `/var` denylist (beta.1) blocks `/var/run/docker.sock` as a descendant match. But the error message is "under denylisted root /var" which is confusing when the operator specifically typed `docker.sock`. Docker Desktop macOS socket at `/Users/*/Library/Containers/com.docker.docker/Data/docker-raw.sock` is NOT blocked — the allow-root check might miss it depending on `workspace-root` config, and `/Users` is NOT in `DefaultDenylist` (only `/Library`, `/Applications`, `/Volumes` are).

**Post-beta.2 mitigation:** PR-D adds three explicit denylist entries (`/var/run/docker.sock`, `/run/docker.sock`, plus the Docker Desktop Mac path via suffix match). Error message now reads "`/var/run/docker.sock` is on the system-path denylist (docker socket)". Tested by Test 22.

### 6.8 Host PID/network/IPC namespace sharing

**Pre-beta.2:** dclaw does not pass `--pid=host`, `--network=host`, or `--ipc=host` flags; default namespaces apply. OK as-is.

**Post-beta.2 mitigation:** Unchanged. Explicitly document in `docs/workspace-root.md` that an operator running `dclaw agent create --privileged` equivalents would bypass beta.2. dclaw does not expose any flag that would do this; a future PR adding host-PID-ns support would need to re-trigger this threat model.

### 6.9 kernel exploit primitives beta.2 does NOT cover

- **Dirty Pipe (CVE-2022-0847)** — kernel-level, any container is affected regardless of caps. Host-kernel patch is the fix.
- **Dirty Cred (CVE-2022-2588)** — ditto.
- **container_t AppArmor escape** — requires AppArmor misconfig on the host; out of scope.

These are **kernel-level** and cannot be closed by Docker `HostConfig` flags. Mitigation: keep the host kernel patched; document this in `docs/workspace-root.md`.

---

## 7. Smoke-Test Additions

Full list of smoke tests after beta.2 merges. Tests 1-16 existing (beta.1). Tests 17-23 new.

| # | Test | Probes | PR |
|---|---|---|---|
| 17 | CAP_MKNOD drop | `mknod /tmp/dev b 8 0` → EPERM | A |
| 18 | seccomp default | `unshare -U -r whoami` → EPERM | A |
| 19 | PidsLimit | spawn 300 processes, confirm cap | A |
| 20 | ReadonlyRootfs | `touch /etc/x` → EROFS; `touch /tmp/ok` → success | B |
| 21 | Non-root uid | `id -u` → `1000` | C |
| 22 | docker.sock denylist | `agent create --workspace=/var/run/docker.sock` → exit 65, `workspace_forbidden` | D |
| 23 | Full posture probe | all of the above in one `agent exec`, plus setuid-bit probe | D |

All tests follow the Test 14-16 precedent: isolated `STATE_DIR`, trap-armed cleanup, prefix-whitelist guard on the temp dir, explicit `--state-dir`/`--daemon-socket`/`--workspace` flags. No `$HOME` touching.

**Docker requirement.** Tests 17-23 all require Docker reachable + `dclaw-agent:v0.1` built. The `docker-smoke` CI workflow (tag-triggered, introduced during the beta.1 hotfix saga) covers this. Dev machines without Docker get "SKIP:" messages the same way Test 13 skips without `ANTHROPIC_API_KEY`. Pattern is already established in the script.

---

## 8. Operational Impact

**User-visible changes on existing agents.** Existing agents in `state.db` created under beta.1 posture will receive the new container posture on their next `agent start`. Specifically:

- Agents that rely on writing to `/etc`, `/usr`, `/opt` from inside the container (unusual for pi-mono workloads) will now get `EROFS`. Mitigation documented in `docs/workspace-root.md`: "write to `/workspace` or `/tmp` only."
- Agents that rely on privileged syscalls (also unusual) will get `EPERM`. No known dclaw use cases hit this.
- Agents that rely on spawning >256 processes simultaneously will get `EAGAIN`. No known dclaw use cases hit this.
- Agents that are running as root inside the container will now run as uid 1000. Any files written before beta.2 under uid 0 may be unreadable under uid 1000. Mitigation: `agent delete <name>` + `agent create <name>` re-uses the same workspace and the workspace's host-side uid; the container's uid 1000 maps to the host operator's uid via Docker's default uid-namespace semantics. This is the same as the current image's `USER node` directive. No data migration needed.

**Image compatibility.** The existing `dclaw-agent:v0.1` image is fully compatible with beta.2 posture — verified by the pi-mono write-path audit in §4.2. Future `dclaw-agent:v0.2+` images must also be audit-clean against `/tmp`, `/run`, `/workspace` write-only semantics.

**Custom images.** Operators who ship their own `--image=...` images need to ensure:

1. `USER` directive sets uid 1000 (daemon enforces this regardless, but image-side `USER` aligns file perms at build time).
2. All runtime writes go to `/tmp`, `/run`, or `/workspace`.
3. No runtime dependency on dropped capabilities.

Document these three rules in a new "Custom image compatibility" section of `docs/workspace-root.md` as part of PR-D.

**CI impact.** `docker-smoke` CI run time grows by ~30 seconds (Tests 17-23 add ~5s each; Test 23 is the big one at ~10s). Total `docker-smoke` job post-beta.2 estimated at ~80 seconds. Acceptable.

---

## 9. Migration / Backwards Compatibility

**No schema migration.** beta.2 touches no SQLite state.

**No wire protocol change.** `protocol.AgentCreateParams`, `protocol.Agent`, `RPCError` are identical to beta.1-paths-hardening.2. An alpha.4 CLI can still talk to a beta.2 daemon.

**State.db compatibility.** Existing agents in `state.db` from beta.1 carry no field that beta.2 interprets. The hardening applies at `ContainerCreate` time so existing stopped containers get the new posture on next `agent start` (which internally calls `ContainerCreate` before `ContainerStart` for a never-started container — but for a previously-created-and-stopped container, the posture is baked into the container ID at the original create time).

**Critical implication.** Agents created under beta.1 that have already had `ContainerCreate` called (which happens in `AgentCreate`, not `AgentStart` — see `internal/daemon/lifecycle.go:122`) will retain their original posture even after beta.2 ships. To get the beta.2 posture, operators must `agent delete <name>` + `agent create <name>`.

**Decision for beta.2:** document this in `docs/workspace-root.md` as "agents created before beta.2 retain their original (weaker) container posture until you delete and recreate them." Add one log line on daemon startup listing beta.1-era agents so the operator knows which ones need recreation:

```go
// cmd/dclawd/main.go legacyScan — extend to also check container posture
// via docker.ContainerInspect(rec.ContainerID).HostConfig.CapDrop.
// If !contains(CapDrop, "ALL") — log "agent %s has pre-beta.2 weak
// container posture; recreate with 'agent delete' + 'agent create' to
// apply the hardening." This is purely advisory; agent continues to
// run.
```

This extension to `legacyScan` is bundled into PR-D (~20 lines).

---

## 10. Test Strategy

### Automated unit + integration tests

| Test | Location | Exercises | PR |
|---|---|---|---|
| `TestCreateAgentAppliesBeta2HardeningPosture` | `internal/sandbox/docker_test.go` | CapDrop, SecurityOpt, PidsLimit applied | A |
| same, extended | `internal/sandbox/docker_test.go` | + ReadonlyRootfs + Tmpfs | B |
| same, extended | `internal/sandbox/docker_test.go` | + User: 1000:1000 | C |
| `TestPolicyValidateDockerSock` (4 rows) | `internal/paths/policy_test.go` | docker.sock paths rejected | D |
| `TestLegacyScanPostureWarning` | `cmd/dclawd/main_test.go` (new) | beta.1-era agents detected | D |
| All existing tests | various | regression | A/B/C/D |

### Integration tests via smoke-daemon.sh

- Tests 17-23 (new, see §7).
- Tests 1-16 (beta.1) must remain green.

### Docker-CI (docker-smoke workflow, tag-triggered)

- Full suite runs on every tag push starting with `v*`.
- Current run time ~47s (per WORKLOG 2026-04-22); post-beta.2 estimated ~80s.

### Manual verification matrix

| Check | Pre-PR | Post-PR |
|---|---|---|
| `agent exec <name> -- mknod /tmp/dev b 8 0` | succeeds | EPERM |
| `agent exec <name> -- touch /etc/x` | succeeds | EROFS |
| `agent exec <name> -- id -u` | `0` or `1000` (depends on image) | `1000` |
| `agent exec <name> -- sh -c 'i=0; while [ $i -lt 300 ]; do (sleep 10 &) 2>/dev/null || break; i=$((i+1)); done; echo $i'` | 300 | ≤ 256 |
| `agent create evil --workspace=/var/run/docker.sock` | rejected (under /var denylist) but vague error | rejected with "docker socket" reason |
| pi-mono chat round-trip (Test 13) | works | works (no regression from hardening) |

---

## 11. Open Questions

### Q1: User-namespace remapping — on by default, opt-in, or defer?

**Decided: defer.** `HostConfig.UsernsMode: "private"` requires `/etc/subuid` and `/etc/subgid` configured on the host, which is outside dclaw's control. Docker Desktop on macOS runs everything in a Linux VM with its own user-namespace story; enabling `UsernsMode: "private"` on macOS has no effect beyond the VM boundary. On Linux, rootless Docker mode is the modern idiomatic path and requires operator-side setup.

beta.2 leaves `UsernsMode` at the Docker daemon default. Document in `docs/workspace-root.md` the user-namespace options for operators who want the extra layer:

1. Run Docker in rootless mode (documented upstream).
2. Configure `/etc/subuid` + `/etc/subgid` and set `--userns-remap=default` on the dockerd side.
3. Accept the default: uid 1000 in the container maps to uid 1000 on the host. Combined with PR-C's `User: 1000:1000`, this is already a meaningful boundary.

**Residual risk:** An agent running as uid 1000 inside the container has the same on-host privileges as any user named `node` with uid 1000 on the host. If the operator's uid is 1000 (common on Linux), this means "same privileges as the operator." Mitigation is workspace-root limits (beta.1) + read-only rootfs (PR-B) + cap-drop (PR-A) — all of which still apply.

### Q2: Seccomp profile — default, or tighter custom?

**Decided: default in beta.2, tighter as follow-up.** Docker's default seccomp profile blocks ~70 syscalls that ordinary workloads never need (`keyctl`, `add_key`, `request_key`, `userfaultfd`, `bpf`, `personality`, etc.). Shipping the default is a full order-of-magnitude improvement over what we have today (implicit default that depends on daemon config).

A tighter custom profile denying additional syscalls that pi-mono specifically doesn't need (e.g., `ptrace` even for same-user, `mount`, `umount2`, `pivot_root`, `clone3` with non-standard flags) is a valuable follow-up but adds ~200 lines of JSON to author and maintain. beta.2 leaves this out.

**Residual risk:** Default seccomp's allowlist grows as the kernel adds syscalls. New primitives that land in Linux 6.x+ may not be explicitly denied. Mitigation: periodic audit against CIS Docker Benchmark (roughly annual cadence).

### Q3: Mock Docker client for testing — interface refactor, or testcontainers-go?

**Decided: lightweight interface refactor.** The current `DockerClient` struct embeds `*client.Client` directly. Adding a minimal `dockerAPI` interface that only covers the methods `CreateAgent` calls (`ContainerCreate`, `ContainerStart`, etc.) is ~15 lines and lets `docker_test.go` inject a recording mock that captures the `HostConfig` without touching a real daemon. Alternative approaches considered:

1. **testcontainers-go** — adds a dependency and requires Docker in CI (already required by `docker-smoke`, but forces unit tests to need Docker too).
2. **real Docker socket in CI** — `docker-smoke` already does this; unit tests shouldn't.
3. **interface refactor** — local, cheap, self-contained.

Decision: interface refactor. See `internal/sandbox/docker.go` PR-A spec.

### Q4: pi-mono write-path audit — what if the audit finds an unexpected write dir?

**Decided: extend `DefaultTmpfs` to include the discovered path, document the reason inline.** Worst case scenarios and mitigations:

| Surface | Where | Mitigation |
|---|---|---|
| pi-mono session file despite `--no-session` | `/root/.pi/agent/session-*.json` | Add `/root: 8m` tmpfs. Unlikely because `--no-session` is explicit. |
| npm cache at runtime (new dependency install) | `/app/node_modules/.cache/*` | Out of scope — runtime should not install deps. If we find this, file a bug against the image build. |
| apt lock file (if something touches apt) | `/var/lib/apt/lists` | Should never happen in a running container. Reject if found. |

The audit runs before PR-B code changes land; findings drive the exact `DefaultTmpfs` contents. Current expectation based on code inspection: `/tmp` and `/run` are sufficient.

### Q5: `agent/run.mjs` uid-0 guard — ship or skip?

**Decided: ship, in PR-C, optional-to-merge.** Four-line change, defense in depth. Even if the image `USER` and daemon-side `HostConfig.User` are both somehow bypassed (perhaps by a future CLI feature that lets operators override image USER), the entry-point itself refuses to run as root.

Trade-off: the guard duplicates what Docker already enforces. If a future image ships a non-Node entry point, run.mjs isn't the entry point any more and the guard is dead code. Acceptable — the guard is cheap.

### Q6: Tmpfs size limits — 64m + 8m, or generous?

**Decided: 64m for `/tmp`, 8m for `/run`.** Conservative; forces pi-mono or tools to surface OOM errors early rather than silently filling tmpfs. Operators running workflows that need more can fork dclaw and tune. Audit revealed pi-mono's typical `/tmp` usage <10 MB; 64 MB is 6× headroom.

### Q7: Should beta.2 also add `Memory` / `CPUQuota`?

**Decided: out of scope; follow-up.** Memory limits are real DoS mitigation but orthogonal to escape-surface hardening. A fork-bombing agent that gets `PidsLimit`-capped at 256 can still consume 10 GB of RAM across those 256 processes. Worth doing, separate PR.

### Q8: Posture regression detection — how do we catch the "someone added a new HostConfig field" accidentally weakening the posture?

**Decided: golden-file test.** `TestCreateAgentAppliesBeta2HardeningPosture` asserts the full shape of the captured `HostConfig` against a golden struct literal. Any new field added to `HostConfig` that we don't explicitly zero/assert trips the test. Verbose but reliable. Alternative — runtime introspection of `docker inspect` output in `docker-smoke` — deferred as it requires real Docker.

---

## 12. Follow-Ups (Deferred — Not Shipped in beta.2-sandbox-hardening)

1. **Custom seccomp profile** — author a tighter JSON profile denying `keyctl`, `add_key`, `request_key`, `ptrace`, `userfaultfd`, `bpf`, `personality`, `clone3` with namespace flags. See §11 Q2. Lives at `internal/sandbox/seccomp.json` and is embedded via `//go:embed`, applied via `SecurityOpt: "seccomp=/path/to/seccomp.json"`.
2. **Per-agent memory + CPU limits** — `HostConfig.Resources.Memory`, `CPUQuota`. See §11 Q7.
3. **User-namespace remapping** — dclaw-side setup for `UsernsMode: "private"` with subuid/subgid provisioning. See §11 Q1.
4. **Rootless Docker daemon recommendation** — `docs/workspace-root.md` section recommending operator-side rootless mode, with links to upstream docs.
5. **gVisor / Kata runtime support** — optional `--runtime` flag on `agent create`, plumbed to `HostConfig.Runtime`. Lets operators opt into stronger isolation on hosts that have gVisor installed.
6. **Network egress allowlist** — wire `protocol.AgentCreateParams.EgressAllowlist` through a userspace proxy or iptables. Separate phase; blocked by `CAP_NET_ADMIN` drop in PR-A if we choose iptables.
7. **Image security rebase** — pinned base image refresh, apt CVE sweep, `npm audit` run on the `dclaw-agent:v0.1` image. Distroless swap as a stretch goal.
8. **AppArmor custom profile** — ship a `docker-default`-derived AppArmor profile with dclaw-specific denies. Requires AppArmor on the host; deferred.
9. **Host-kernel version advisory in `docs/workspace-root.md`** — document that beta.2 does not mitigate kernel CVEs (Dirty Pipe etc.) and operators must keep the host patched.
10. **Per-agent posture override for operators who opt into weaker posture** — e.g., `--no-readonly-rootfs`, `--cap-add=CAP_NET_RAW`. Deliberately NOT in beta.2 to keep the default secure; add behind a loud `--trust-posture="reason"` flag if ever needed.
11. **Container posture introspection via `agent describe`** — show CapDrop, SecurityOpt, ReadonlyRootfs in `dclaw agent describe` output. Useful for verifying posture without shelling into `docker inspect`.
12. **`legacyScan` posture warning** — extend `cmd/dclawd/main.go:legacyScan` to flag beta.1-era agents whose containers were created with weaker posture. Bundled into PR-D scope; see §9.

---

## 13. Acceptance Checklist

Hatef ticks these off before tagging.

- [ ] PR-A merges clean on top of `02d4119`; CI green (build + vet).
- [ ] PR-A: `go test ./internal/sandbox/...` passes with the posture assertion table (≥3 rows).
- [ ] PR-A: `grep -E 'CapDrop|SecurityOpt|PidsLimit' internal/sandbox/docker.go` returns ≥4 lines.
- [ ] PR-A: smoke Test 17 (mknod) + Test 18 (seccomp) + Test 19 (PidsLimit) green on a docker-reachable host.
- [ ] PR-B merges clean; CI green.
- [ ] PR-B: `HostConfig.ReadonlyRootfs == true` and `Tmpfs` contains `/tmp` + `/run` (asserted by Go test).
- [ ] PR-B: smoke Test 20 (rootfs write) green.
- [ ] PR-B: pi-mono chat Test 13 still green (no regression from ReadonlyRootfs).
- [ ] PR-C merges clean; CI green.
- [ ] PR-C: `HostConfig.User == "1000:1000"` (asserted by Go test).
- [ ] PR-C: smoke Test 21 (uid probe) returns `1000`.
- [ ] PR-C: `agent/run.mjs` uid-0 guard ships (optional; see §11 Q5).
- [ ] PR-D merges clean; CI green.
- [ ] PR-D: `internal/paths/policy.go` `DefaultDenylist` grows by three entries (`/var/run/docker.sock`, `/run/docker.sock`, plus the Docker Desktop Mac suffix-match in the `Validate` logic).
- [ ] PR-D: `go test ./internal/paths/...` passes with 4 new validator rows.
- [ ] PR-D: smoke Test 22 (docker.sock reject) + Test 23 (full posture probe) green.
- [ ] PR-D: `docs/workspace-root.md` gets "Docker socket" subsection.
- [ ] PR-D: `cmd/dclawd/main.go:legacyScan` extension warns on beta.1-era container posture.
- [ ] All four PRs squash-merged to `main` in order A → B → C → D.
- [ ] `git tag -a v0.3.0-beta.2-sandbox-hardening -m "Phase 3 beta.2 sandbox-hardening: container escape surface"`.
- [ ] `git push origin main v0.3.0-beta.2-sandbox-hardening`.
- [ ] `docker-smoke` CI workflow green on the new tag (Tests 1-23 all pass).
- [ ] `WORKLOG.md` entry added documenting the ship.

---

## 14. References

- `WORKLOG.md` 2026-04-21/22 session — the "Independent code review" entry that flagged the escape surface. Specifically: "internal/sandbox/docker.go:98-107 sets no SecurityOpt, no CapDrop, no ReadonlyRootfs, no User, no UsernsMode, no PidsLimit, no Tmpfs."
- `docs/phase-3-beta1-paths-hardening-plan.md` — style and structure template. beta.2 mirrors the section 0-14 layout exactly.
- `docs/phase-1-plan.md:540` — corrected by beta.1 to describe bind-mount semantics; beta.2 now extends that discussion to in-container blast radius.
- `docs/phase-3-alpha4-plan.md` — style cross-check.
- `internal/sandbox/docker.go:90-136` — the `CreateAgent` site where every PR-A/B/C landing lives.
- `internal/paths/policy.go:48-64` — the `DefaultDenylist` that PR-D extends.
- `agent/Dockerfile:45` — `USER node` (uid 1000) that PR-C reinforces via `HostConfig.User: "1000:1000"`.
- `agent/run.mjs:8` — entry-point that PR-C optionally guards against uid 0.
- `internal/daemon/lifecycle.go:122` — `AgentCreate` → `docker.CreateAgent` call site; unchanged by beta.2 (posture lives entirely in sandbox layer).
- `scripts/smoke-daemon.sh:191-244` — beta.1 Tests 14-16; beta.2 adds Tests 17-23 in the same style.
- Docker SDK: `github.com/docker/docker/api/types/container@v26.1.3` — `HostConfig` fields used (CapDrop, SecurityOpt, ReadonlyRootfs, Tmpfs, Resources.PidsLimit, User via container.Config).
- CIS Docker Benchmark v1.6.0 — section 5 "Container Runtime" — all items PR-A/B/C implement.
- Docker docs: "Runtime privilege and Linux capabilities" — source for the CapDrop=ALL + default-cap-list discussion.
- CVE-2019-5736 (runc /proc/self/exe) — mitigated by the PR-A+B combination.
- CVE-2022-0185 (fs_context) — mitigated by seccomp default + `no-new-privileges` (PR-A).
