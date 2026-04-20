// Package config provides the canonical state-dir and socket-path resolution
// shared by every entrypoint (cmd/dclaw, cmd/dclawd, internal/cli,
// internal/client, internal/daemon). It exists so the five historical
// os.UserHomeDir + filepath.Join(home, ".dclaw") call sites converge on a
// single precedence rule: flag > env (DCLAW_STATE_DIR) > default.
//
// Import boundary: this package imports only the standard library. It must
// not import any other internal/ package to keep the dependency DAG acyclic
// (internal/daemon, internal/cli, internal/client and cmd/* all import
// internal/config, never the reverse).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// EnvWorkspaceRoot names the environment variable that selects the workspace
// root used for agent workspace policy validation. It is consumed by PR-C's
// config reader (internal/config file reader + internal/paths.Policy); PR-B
// only pre-declares the constant so the name has exactly one source of truth.
const EnvWorkspaceRoot = "DCLAW_WORKSPACE_ROOT"

// Paths is the resolved on-disk surface used by the daemon and all clients.
// Callers treat Paths as immutable after Resolve returns.
type Paths struct {
	// StateDir is the root directory for dclaw state (default: ~/.dclaw).
	// Created with mode 0700 elsewhere; Resolve does not mkdir.
	StateDir string
	// SocketPath is the Unix domain socket dclawd listens on.
	SocketPath string
}

// Resolve computes Paths from a flag value, the DCLAW_STATE_DIR env var, and
// the user's home directory, in that precedence. An empty stateDirFlag or
// socketFlag means "unset"; only a non-empty value counts as "flag wins".
//
// Precedence for StateDir:
//  1. stateDirFlag if non-empty
//  2. os.Getenv("DCLAW_STATE_DIR") if non-empty
//  3. filepath.Join(<home>, ".dclaw")
//
// Precedence for SocketPath:
//  1. socketFlag if non-empty
//  2. DefaultSocketPath(StateDir) — Linux XDG_RUNTIME_DIR override, else <StateDir>/dclaw.sock
//
// Resolve returns an error only when falling back to the default requires
// os.UserHomeDir and the OS cannot supply it. Callers providing an explicit
// flag or env never see this error path.
func Resolve(stateDirFlag, socketFlag string) (Paths, error) {
	stateDir := stateDirFlag
	if stateDir == "" {
		stateDir = os.Getenv("DCLAW_STATE_DIR")
	}
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home dir: %w", err)
		}
		stateDir = filepath.Join(home, ".dclaw")
	}

	socket := socketFlag
	if socket == "" {
		socket = DefaultSocketPath(stateDir)
	}

	return Paths{StateDir: stateDir, SocketPath: socket}, nil
}

// DefaultSocketPath returns the resolved socket path for this host.
//
// On Linux, prefer $XDG_RUNTIME_DIR/dclaw.sock (typically /run/user/<uid>).
// If XDG_RUNTIME_DIR is unset or not a writable directory, fall back to
// <stateDir>/dclaw.sock. On macOS, XDG_RUNTIME_DIR is rarely set; always
// use <stateDir>/dclaw.sock.
//
// This logic is copied verbatim from the pre-PR-A internal/daemon.DefaultSocketPath
// so that every call site that previously relied on it retains byte-identical behavior.
func DefaultSocketPath(stateDir string) string {
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			if fi, err := os.Stat(xdg); err == nil && fi.IsDir() {
				return filepath.Join(xdg, "dclaw.sock")
			}
		}
	}
	return filepath.Join(stateDir, "dclaw.sock")
}

// MustResolveSocket returns the default socket path, falling back to
// /tmp/dclaw.sock when the home directory cannot be resolved. It matches
// the legacy error behavior of internal/client.DefaultSocketPath and
// cmd/dclaw/main.go:resolveSocket so that the bare TUI launch path keeps
// working on broken environments.
//
// Used by cmd/dclaw/main.go, which cannot return errors from the bare
// invocation path and must choose a socket before handing off to the TUI.
func MustResolveSocket() string {
	p, err := Resolve("", "")
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return p.SocketPath
}
