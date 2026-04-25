package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/paths"
)

// resetInitFlags clears package-level flag state so subsequent tests do not
// see leftover values from earlier runs. Cobra reuses the same *cobra.Command
// pointer across t.Run subtests. Also strips /var, /private/var, /private/tmp
// from the validator denylist for the test's lifetime so t.TempDir paths on
// macOS — which resolve under /private/var/folders/... — are not rejected by
// an entry that overlaps the OS's temp-dir storage. Mirrors the override
// applied in internal/paths/{policy,opensafe}_test.go.
func resetInitFlags(t *testing.T) {
	t.Helper()
	origDenylist := initDenylist
	initDenylist = stripTempDirEntriesForTest(paths.DefaultDenylist)
	t.Cleanup(func() {
		initYes = false
		initWorkspaceRoot = ""
		initDenylist = origDenylist
		// Reset the "Changed" bookkeeping so the next test sees a fresh flagset.
		if f := initCmd.Flags().Lookup("workspace-root"); f != nil {
			f.Changed = false
		}
		if f := initCmd.Flags().Lookup("yes"); f != nil {
			f.Changed = false
		}
	})
}

// stripTempDirEntriesForTest removes entries that overlap with t.TempDir's
// storage locations on macOS. Mirrors stripTempDirEntries in
// internal/paths/policy_test.go.
func stripTempDirEntriesForTest(in []string) []string {
	strip := map[string]bool{"/var": true, "/private/var": true, "/private/tmp": true}
	out := make([]string, 0, len(in))
	for _, e := range in {
		if strip[e] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// TestInitYesUsesDefault verifies that `dclaw init --yes` with a fresh
// state-dir creates $HOME/dclaw, writes config.toml pointing at that path,
// and prints the canonical path on stdout. Both DCLAW_STATE_DIR and HOME
// are redirected to t.TempDir() so the test never touches the real $HOME.
func TestInitYesUsesDefault(t *testing.T) {
	resetInitFlags(t)
	stateDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", stateDir)
	t.Setenv("HOME", homeDir)

	initYes = true
	defer func() { initYes = false }()

	var out bytes.Buffer
	initCmd.SetOut(&out)
	initCmd.SetErr(&out)
	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	expected := filepath.Join(homeDir, "dclaw")
	// On macOS /tmp resolves through /private — Validate canonicalizes via
	// EvalSymlinks. Match by EvalSymlinks of the expected path.
	canonExpected, err := filepath.EvalSymlinks(expected)
	if err != nil {
		t.Fatalf("EvalSymlinks expected: %v", err)
	}

	if !strings.Contains(out.String(), canonExpected) {
		t.Fatalf("output missing canonical path %q: %q", canonExpected, out.String())
	}
	if !strings.Contains(out.String(), "workspace-root configured:") {
		t.Fatalf("output missing 'workspace-root configured:' line: %q", out.String())
	}
	if !strings.Contains(out.String(), "created (mode 0700)") {
		t.Fatalf("output missing 'created (mode 0700)' line: %q", out.String())
	}

	// Verify the directory was created with 0700.
	st, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("stat created dir: %v", err)
	}
	if !st.IsDir() {
		t.Fatalf("expected directory, got %v", st.Mode())
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		t.Fatalf("expected mode 0700, got %o", perm)
	}

	// Verify config.toml was written.
	cfg, err := config.ReadConfigFile(stateDir)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if cfg.WorkspaceRoot != canonExpected {
		t.Fatalf("config workspace-root = %q, want %q", cfg.WorkspaceRoot, canonExpected)
	}
}

// TestInitWorkspaceRootFlag verifies that --workspace-root <path> bypasses
// the default and writes the explicit path. Uses a t.TempDir() target so the
// test does not assume /tmp is writable for a literal /tmp/explicit.
func TestInitWorkspaceRootFlag(t *testing.T) {
	resetInitFlags(t)
	stateDir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", stateDir)
	t.Setenv("HOME", t.TempDir())

	target := filepath.Join(t.TempDir(), "explicit")
	initWorkspaceRoot = target
	if f := initCmd.Flags().Lookup("workspace-root"); f != nil {
		f.Changed = true
	}
	defer func() { initWorkspaceRoot = "" }()

	var out bytes.Buffer
	initCmd.SetOut(&out)
	initCmd.SetErr(&out)
	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	canonTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks target: %v", err)
	}

	cfg, err := config.ReadConfigFile(stateDir)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if cfg.WorkspaceRoot != canonTarget {
		t.Fatalf("config workspace-root = %q, want %q", cfg.WorkspaceRoot, canonTarget)
	}
	if !strings.Contains(out.String(), canonTarget) {
		t.Fatalf("output missing target path %q: %q", canonTarget, out.String())
	}
}

// TestInitIdempotent verifies that running `dclaw init` a second time with
// workspace-root already set prints "already configured" and exits 0 without
// modifying config.toml. The check uses the file's modtime / contents
// before-and-after to confirm no write happened.
func TestInitIdempotent(t *testing.T) {
	resetInitFlags(t)
	stateDir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", stateDir)
	t.Setenv("HOME", t.TempDir())

	// Pre-seed config.toml with an arbitrary value.
	preseed := config.FileConfig{WorkspaceRoot: "/already/set/by/operator"}
	if err := config.WriteConfigFile(stateDir, preseed); err != nil {
		t.Fatalf("preseed: %v", err)
	}

	cfgPath := filepath.Join(stateDir, "config.toml")
	preBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read preseed: %v", err)
	}

	initYes = true
	defer func() { initYes = false }()

	var out bytes.Buffer
	initCmd.SetOut(&out)
	initCmd.SetErr(&out)
	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	if !strings.Contains(out.String(), "already configured") {
		t.Fatalf("output missing 'already configured': %q", out.String())
	}
	if !strings.Contains(out.String(), "/already/set/by/operator") {
		t.Fatalf("output missing existing value: %q", out.String())
	}

	// Verify file content is byte-identical after the no-op.
	postBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read post: %v", err)
	}
	if !bytes.Equal(preBytes, postBytes) {
		t.Fatalf("config.toml modified by idempotent init:\nbefore: %s\nafter:  %s", preBytes, postBytes)
	}
}

// TestInitRejectsDenylistedRoot verifies that --workspace-root /etc fails
// with workspace_forbidden semantics (errors.Is unwraps to the sentinel).
// Uses --workspace-root flag form so no stdin handling is needed.
func TestInitRejectsDenylistedRoot(t *testing.T) {
	resetInitFlags(t)
	stateDir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", stateDir)
	t.Setenv("HOME", t.TempDir())

	initWorkspaceRoot = "/etc"
	if f := initCmd.Flags().Lookup("workspace-root"); f != nil {
		f.Changed = true
	}
	defer func() { initWorkspaceRoot = "" }()

	var out bytes.Buffer
	initCmd.SetOut(&out)
	initCmd.SetErr(&out)
	err := runInit(initCmd, nil)
	if err == nil {
		t.Fatalf("expected error for /etc, got nil; out=%q", out.String())
	}
	if !errors.Is(err, paths.ErrWorkspaceForbidden) {
		t.Fatalf("expected ErrWorkspaceForbidden wrap, got %v", err)
	}

	// Verify config.toml was NOT written.
	cfg, err := config.ReadConfigFile(stateDir)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if cfg.WorkspaceRoot != "" {
		t.Fatalf("config.toml unexpectedly contains workspace-root=%q", cfg.WorkspaceRoot)
	}
}
