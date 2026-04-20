// Package paths implements dclaw's workspace-path validation policy.
//
// Policy.Validate is the pure function at the core of beta.1-paths-hardening:
// given a raw --workspace input string, it returns either a canonical,
// absolute, NFC-normalized, symlink-resolved path inside the configured
// allow-root, or ErrWorkspaceForbidden with a reason. OpenSafe adds a
// TOCTOU-hardened open that re-canonicalizes the opened fd so a symlink
// swap between Validate and open cannot promote a denylisted target.
//
// The separation of concerns is strict:
//   - internal/paths: the validator and OpenSafe. Pure of business logic.
//   - internal/daemon: calls Policy.Validate on AgentCreate, logs the
//     decision via internal/audit, and surfaces ErrWorkspaceForbidden to
//     the router.
//   - internal/daemon/router.go: maps ErrWorkspaceForbidden to the
//     protocol wire code ErrWorkspaceForbidden = -32007.
//   - internal/sandbox: belt-and-suspenders assertion that any workspace
//     reaching the Docker bind-mount step is already absolute and clean.
package paths

import "errors"

// ErrWorkspaceForbidden is the sentinel for any workspace-path rejection.
// All failure modes in Policy.Validate wrap this value via fmt.Errorf("%w: ...").
// Callers use errors.Is(err, ErrWorkspaceForbidden) to detect policy denials
// without string matching.
var ErrWorkspaceForbidden = errors.New("workspace path forbidden by policy")
