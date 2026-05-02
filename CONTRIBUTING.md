# Contributing to dclaw

This document describes how dclaw is built and shipped. It's the orientation guide for landing on the repo cold.

## What is dclaw

dclaw is a container-native multi-agent platform — a Go control-plane daemon (`dclawd`) plus mandatory Docker sandboxes for every agent (data plane). See [README.md](README.md) for the elevator pitch and the security posture.

## Versioning convention

dclaw uses two release shapes on the `v0.3.0` line:

- **Phase release: `v0.3.0-beta.X`** (e.g. `beta.1`, `beta.2`). A phase has a paired plan doc at `docs/phase-3-betaX-NAME-plan.md` (14 sections matching the templates of `phase-3-beta1-paths-hardening-plan.md` and `phase-3-beta2-sandbox-hardening-plan.md`). Phases are multi-PR series, batched into a single review cycle.
- **Patch release: `v0.3.0-beta.X.Y`** (e.g. `beta.2.6`). Smaller scope. Patches can bundle natural-affinity items in one commit when the items belong together (e.g. beta.2.6 bundled XDG state-dir on Linux + Windows denylist as "platform-port"). Patches don't get their own plan doc — they reference the parent phase plan's §0 follow-ups list.
- **Hotfix tag: trailing `.N` on a phase tag** (e.g. `beta.2-sandbox-hardening.4`). Used when CI surfaces a bug post-tag and you re-tag with the fix. The unfixed parent tag stays as a red historical marker. WORKLOG records each hotfix and what it addressed.

## Per-phase loop

A phase release runs through this sequence:

1. **Architect drafts the plan doc.** `docs/phase-3-betaX-NAME-plan.md`, 14 sections covering Status (§0), Overview, Dependencies, Sequencing, Per-PR Spec, Modified Files Diff Summary, Threat Model, Smoke-Test Additions, Operational Impact, Migration / Backwards Compatibility, Test Strategy, Open Questions, Acceptance, Plan Self-Audit. The plan is the source of truth for scope and is gated on Hatef sign-off.
2. **Orchestrator dispatches build agents per PR.** Each PR is a sub-agent invocation; the orchestrator never edits Go code directly. PRs are sequenced per the §3 dependency graph; squash-merged into `main`.
3. **CI gates every push.** `.github/workflows/build.yml` runs the `build` job on every push (main + tags + PRs); `docker-smoke` runs on `main` pushes and `v*` tag pushes (the latter trigger added in beta.2.1). All must be green before tag.
4. **Doc-review agent before tag.** Every release gets a documentation-review pass — a sub-agent checks all `*.md` files for consistency with the new shipped surface (versions, command names, file paths, plan-doc cross-refs). Findings are folded into a doc-only commit before tag push.
5. **Tag and push the tag.** `git tag v0.3.0-beta.X-NAME && git push origin v0.3.0-beta.X-NAME`. Never force-push.
6. **WORKLOG entry.** The orchestrator (NOT build agents) writes the WORKLOG entry for the release. Include shipped commit hashes, diff size, CI run times, deviations, follow-ups filed.
7. **Optional GitHub pre-release.** For phase tags, opening a GitHub Release with the WORKLOG narrative as body text gives external readers a navigable index.

## Per-patch loop

Patches follow a tighter loop:

1. **Pick the next item from the parent phase's §0 follow-ups list.** Items can be bundled when they share natural affinity (e.g. XDG + Windows denylist both belong under "platform-port").
2. **Build brief.** Sometimes a single Discord message; sometimes a 1-2 page brief. No 14-section plan doc required.
3. **Orchestrator dispatches one build sub-agent.** Single commit on `main`; no PR-A/B/C/D split. CI must be green.
4. **Doc-review sub-agent before tag.** Same coverage as phase doc-review, scoped to what the patch touched.
5. **Tag + push tag + WORKLOG entry.** Same as phase loop steps 5-6.

## WORKLOG.md protocol

- **Every release gets an entry.** Phase releases get a long entry (build cycle, hotfixes, lessons); patches get a shorter entry (~one screen).
- **Orchestrator owns it.** Build agents must NOT touch dated entries — they edit code, the orchestrator narrates afterward. The exception is the orientation preamble at the top of the file (file hygiene, not dated content).
- **Pushed with every commit batch.** WORKLOG survives machine loss only if it's on `origin`. The 2026-04-18 wipe destroyed unpushed work; the file is the most important fail-safe against another wipe.
- **Format:** dated H2 (`## YYYY-MM-DD — Title`), entries in chronological order (oldest top, newest bottom), each entry self-contained with a "Final state" subsection at the bottom (main tip, latest tag, CI status).

## Plan-doc shape

Use `docs/phase-3-beta1-paths-hardening-plan.md` and `docs/phase-3-beta2-sandbox-hardening-plan.md` as the structural templates. The 14 sections are:

§0 Status, §1 Overview, §2 Dependencies, §3 Sequencing, §4 Per-PR Spec, §5 Modified Files Diff Summary, §6 Threat Model, §7 Smoke-Test Additions, §8 Operational Impact, §9 Migration / Backwards Compatibility, §10 Test Strategy, §11 Open Questions, §12 Acceptance / Follow-ups, §13 Plan Self-Audit.

§0 must include shipped-commits table (post-ship), target tag, branch, base commit, est. duration, prereqs, trigger.

## CI shape

`.github/workflows/build.yml` defines two jobs:

- **`build`**: runs on every push (`main`, `v*` tags, PRs). Fast (~15-25s). Runs `go build ./cmd/dclaw ./cmd/dclawd`, `go test ./...`, `go vet ./...`.
- **`docker-smoke`**: runs on `main` pushes AND `v*` tag pushes (the main-push trigger landed in beta.2.1). Slower (~50-90s). Spins up docker-dind, runs `scripts/smoke-daemon.sh` end-to-end.

Both must be green for a release to ship. Pre-beta.2.1 the `docker-smoke` job was tag-only; that gap caused multi-tag hotfix cascades on beta.1 and beta.2 because integration bugs only surfaced post-tag. The main-push trigger is the most important CI improvement on the v0.3.0 line.

## Smoke tests

`scripts/smoke-daemon.sh` is the canonical end-to-end smoke suite. 23 tests as of beta.2.6:

- **Tests 1-13**: paths/CRUD baseline (daemon up/down, agent create/list/describe/start/stop/delete, chat round-trip, error paths).
- **Tests 14-16**: beta.1-paths-hardening — workspace validator, `--workspace-trust`, audit log NDJSON shape.
- **Tests 17-23**: beta.2-sandbox-hardening — cap-probe, seccomp-probe, fork-bomb-probe, rootfs-write-probe, uid-probe, docker.sock denylist, full posture probe.

When adding a phase, extend this file with new tests and assert on observable behavior (exit codes, JSON output, container state). The harness uses `$SMOKE_STATE` (a `mktemp -d` path) for state isolation; never reference `$HOME` directly.

## How to add a new release

1. **Bump versioning per the rules above.** New surface or multi-PR series → phase tag (`beta.X+1`). Single follow-up item or natural-affinity bundle → patch tag (`beta.X.Y+1`). Bug-fix on top of an already-tagged release → hotfix tag (`.N+1`).
2. **Write plan doc OR build brief.** Phase: full 14-section doc, gated on sign-off. Patch: one-screen brief.
3. **Ship the code.** Orchestrator dispatches sub-agent(s). Smoke-suite stays green. CI green on every push.
4. **Verify CI.** Both `build` and `docker-smoke` green on the final main commit before tag.
5. **Doc-review pass.** Sub-agent reads all changed-or-related `*.md` files. Folds findings into a docs-only commit before tag push.
6. **Tag.** `git tag <tag> && git push origin <tag>`. Wait for tag-triggered CI to confirm green.
7. **WORKLOG entry.** Orchestrator writes the entry, push to `main`.
8. **Optional GitHub pre-release.** Open a Release on GitHub with WORKLOG narrative as body.

## Important conventions

- **Orchestrator-only mode.** The orchestrator deploys sub-agents for any code change. No direct Edit/Write on Go source from the orchestrator.
- **Orchestrator owns WORKLOG.** Build agents ship code; the orchestrator narrates afterward.
- **Doc-review every release.** Phase or patch — every tag gets a docs sweep before push.
- **Plan-first.** No code lands until the plan (or brief) is signed off.
- **Push WORKLOG with every commit batch.** Survives machine loss only if it's on `origin`.
- **No force-push, no rebase, no `git reset --hard`** on `main`. New commits only. Hotfix tags handle "the previous tag was wrong" cleanly.
