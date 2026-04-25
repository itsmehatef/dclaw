//go:build !windows

package paths_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/itsmehatef/dclaw/internal/paths"
)

// testDenylist returns paths.DefaultDenylist with /var, /private/var and
// /private/tmp removed so t.TempDir paths on macOS are not rejected by a
// denylist entry that overlaps the OS's temp-dir storage. All tests that
// use t.TempDir should go through this helper.
func testDenylist() []string {
	strip := map[string]bool{"/var": true, "/private/var": true, "/private/tmp": true}
	out := make([]string, 0, len(paths.DefaultDenylist))
	for _, e := range paths.DefaultDenylist {
		if strip[e] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// TestOpenSafeReturnsFd verifies the happy-path contract: given a validated
// path inside the allow-root, OpenSafe returns an open directory fd whose
// kernel-reported canonical agrees with the Validate-returned canonical.
func TestOpenSafeReturnsFd(t *testing.T) {
	tmpRoot := t.TempDir()
	allowRoot := filepath.Join(tmpRoot, "root")
	if err := os.Mkdir(allowRoot, 0o755); err != nil {
		t.Fatalf("mkdir allow-root: %v", err)
	}
	target := filepath.Join(allowRoot, "ws")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	pol := paths.Policy{AllowRoot: allowRoot, Denylist: testDenylist()}
	canonical, err := pol.Validate(target)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	f, opened, err := paths.OpenSafe(canonical, pol)
	if err != nil {
		t.Fatalf("OpenSafe: %v", err)
	}
	defer f.Close()
	// The kernel's F_GETPATH / /proc/self/fd canonical may differ in case
	// or trailing-slash semantics from Validate's output on macOS; both
	// must resolve to the same EvalSymlinks result.
	a, _ := filepath.EvalSymlinks(canonical)
	b, _ := filepath.EvalSymlinks(opened)
	if a != b {
		t.Fatalf("OpenSafe canonical mismatch: validate=%q open=%q", a, b)
	}
}

// TestOpenSafeTOCTOUSymlinkAttack exercises the core reason OpenSafe
// exists: between Validate and the directory open, an attacker swaps the
// target for a symlink pointing at a denylisted path. O_NOFOLLOW prevents
// the kernel from traversing the swapped symlink, so either the open
// fails outright (EMLINK / ELOOP) or the canonical re-check catches the
// target. Either outcome yields a non-nil error wrapping ErrWorkspaceForbidden.
func TestOpenSafeTOCTOUSymlinkAttack(t *testing.T) {
	tmpRoot := t.TempDir()
	allowRoot := filepath.Join(tmpRoot, "root")
	if err := os.Mkdir(allowRoot, 0o755); err != nil {
		t.Fatalf("mkdir allow-root: %v", err)
	}
	// Initially, target is a real directory inside the allow-root. Validate
	// succeeds.
	target := filepath.Join(allowRoot, "ws")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	pol := paths.Policy{AllowRoot: allowRoot, Denylist: testDenylist()}
	canonical, err := pol.Validate(target)
	if err != nil {
		t.Fatalf("Validate (pre-swap): %v", err)
	}

	// Attacker wins the race: remove the real directory, symlink the
	// target to a path outside the allow-root. O_NOFOLLOW on the final
	// component should refuse to open a symlink, so OpenSafe fails.
	evil := filepath.Join(tmpRoot, "evil")
	if err := os.Mkdir(evil, 0o755); err != nil {
		t.Fatalf("mkdir evil: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("rm target: %v", err)
	}
	if err := os.Symlink(evil, target); err != nil {
		t.Fatalf("symlink swap: %v", err)
	}

	_, _, err = paths.OpenSafe(canonical, pol)
	if err == nil {
		t.Fatalf("OpenSafe did not detect TOCTOU swap — test skipped incorrectly?")
	}
	if !errors.Is(err, paths.ErrWorkspaceForbidden) {
		t.Fatalf("OpenSafe error did not wrap ErrWorkspaceForbidden: %v", err)
	}
}
