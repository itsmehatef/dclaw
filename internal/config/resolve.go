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
//  3. Platform default (see defaultStateDir):
//     - linux: $XDG_STATE_HOME/dclaw if XDG_STATE_HOME is set, else
//       ~/.local/state/dclaw if ~/.local/state exists, else ~/.dclaw
//       (legacy backward-compat — if ~/.dclaw already populated from a
//       prior install, that path is preferred over XDG defaults).
//     - darwin: ~/.dclaw (XDG isn't a Darwin convention).
//     - other:  ~/.dclaw.
//
// Precedence for SocketPath:
//  1. socketFlag if non-empty
//  2. DefaultSocketPath(StateDir) — Linux XDG_RUNTIME_DIR override, else <StateDir>/dclaw.sock
//
// Resolve returns an error only when falling back to the default requires
// os.UserHomeDir and the OS cannot supply it. Callers providing an explicit
// flag or env never see this error path.
//
// XDG decision matrix (Linux, beta.2.6 / Plan §12 #4):
//
//	XDG_STATE_HOME set  | ~/.dclaw exists | result
//	--------------------|-----------------|------------------------------
//	yes                 | no              | $XDG_STATE_HOME/dclaw
//	yes                 | yes             | ~/.dclaw       (legacy wins)
//	no, ~/.local/state  | no              | ~/.local/state/dclaw
//	no, ~/.local/state  | yes             | ~/.dclaw       (legacy wins)
//	no, no .local/state | n/a             | ~/.dclaw
//
// macOS + other OSes: always ~/.dclaw — XDG is not a convention there.
func Resolve(stateDirFlag, socketFlag string) (Paths, error) {
	stateDir := stateDirFlag
	if stateDir == "" {
		stateDir = os.Getenv("DCLAW_STATE_DIR")
	}
	if stateDir == "" {
		d, err := defaultStateDir()
		if err != nil {
			return Paths{}, err
		}
		stateDir = d
	}

	socket := socketFlag
	if socket == "" {
		socket = DefaultSocketPath(stateDir)
	}

	return Paths{StateDir: stateDir, SocketPath: socket}, nil
}

// defaultStateDir returns the platform-appropriate default state-dir when no
// flag and no DCLAW_STATE_DIR env var have been set. See Resolve for the
// full precedence rules.
//
// Linux (XDG): The freedesktop.org Base Directory Spec says state-dirs
// belong under $XDG_STATE_HOME (default $HOME/.local/state). Beta.2.6
// honors that on Linux while keeping ~/.dclaw as a backward-compatible
// fallback for existing installs.
//
// Backward-compat priority: if ~/.dclaw already exists (the pre-beta.2.6
// default), it wins over the XDG path so existing operators don't lose
// their state across an upgrade. New installs land in the XDG path.
//
// macOS / other: ~/.dclaw unchanged. XDG_STATE_HOME on macOS is rare
// enough that opting in there would surprise more users than it helps.
func defaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	legacy := filepath.Join(home, ".dclaw")

	if runtime.GOOS != "linux" {
		// darwin + other OSes: always use the legacy path.
		return legacy, nil
	}

	// Linux: backward-compat — if ~/.dclaw already exists from a prior
	// install, prefer it over any XDG path. This avoids breaking an
	// upgrade where the operator already has populated state.
	if fi, statErr := os.Stat(legacy); statErr == nil && fi.IsDir() {
		return legacy, nil
	}

	// XDG_STATE_HOME wins when set and points at a writable directory.
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		if isWritableDir(xdg) {
			return filepath.Join(xdg, "dclaw"), nil
		}
	}

	// Spec default: $HOME/.local/state/dclaw. Use it only if the parent
	// $HOME/.local/state already exists, signalling the user has an XDG
	// state tree. Otherwise fall back to the legacy path so the daemon
	// doesn't silently materialize an XDG tree on systems that don't use
	// one.
	xdgDefaultParent := filepath.Join(home, ".local", "state")
	if fi, statErr := os.Stat(xdgDefaultParent); statErr == nil && fi.IsDir() {
		return filepath.Join(xdgDefaultParent, "dclaw"), nil
	}

	return legacy, nil
}

// isWritableDir reports whether path exists, is a directory, and is
// writable by the current process. Used by defaultStateDir to validate
// $XDG_STATE_HOME before honoring it: a misconfigured pointer (e.g. a
// regular file or unwritable mount) silently falls back to the next
// rung in the XDG precedence ladder rather than failing the daemon
// at startup.
func isWritableDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return false
	}
	// os.Access(2) is not portable through the stdlib; the canonical
	// Go idiom is to attempt a write probe. We avoid creating real
	// content by using a tempfile pattern whose name is unique per
	// process and removing it immediately. Any error (EACCES, EROFS,
	// ENOSPC) → not writable.
	probe, err := os.CreateTemp(path, ".dclaw-xdg-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
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
