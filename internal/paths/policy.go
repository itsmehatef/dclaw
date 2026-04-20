package paths

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// PolicyVersion is the integer currently recorded in audit entries. Bump
// any time denylist semantics change so readers can tell old vs. new
// decisions apart.
const PolicyVersion = 1

// MaxPathLen is a hard upper bound on path length (bytes, post-Clean).
// Matches macOS _PC_PATH_MAX (4096). Darwin and Linux both honor the same
// limit; Windows is out of scope for beta.1.
const MaxPathLen = 4096

// Policy is the immutable description of which paths the validator will
// accept. Callers construct one Policy at daemon startup from the resolved
// config file plus the built-in denylist and reuse it for every
// Policy.Validate call.
//
//   - AllowRoot is the configured workspace-root. Every canonical path that
//     is not explicitly trusted must be at or under this path. An empty
//     AllowRoot means "no root configured"; in that mode every non-trust
//     call is rejected with a pointer to `dclaw config set workspace-root`.
//   - Denylist is the list of canonical absolute paths that are forbidden
//     regardless of allow-root. Matching is case-insensitive (EqualFold)
//     to defend against APFS case-insensitivity bypasses, and applies to
//     both the exact path and any path that has a denylist entry as a
//     descendant directory.
//   - AllowTrust, when true, bypasses the AllowRoot-prefix check but still
//     runs every other invariant (NFC, NUL/control rejection, Clean, Rel
//     for "no .. escape" semantics). AllowTrust does NOT bypass Denylist.
type Policy struct {
	AllowRoot  string
	Denylist   []string
	AllowTrust bool
}

// DefaultDenylist is the canonical macOS + common Unix list of paths that
// must never be used as an agent workspace. Callers assemble the daemon's
// runtime Denylist by appending the daemon user's $HOME (resolved at
// startup via config.Resolve).
var DefaultDenylist = []string{
	"/",
	"/etc",
	"/usr",
	"/var",
	"/bin",
	"/sbin",
	"/home",
	"/root",
	"/private/etc",
	"/private/var",
	"/private/tmp",
	"/Volumes",
	"/Library",
	"/Applications",
	"/opt",
}

// Validate runs the full validator pipeline against raw. On success it
// returns the canonical absolute path that must be passed to the Docker
// bind-mount (never the raw input; callers that pass raw re-open the
// TOCTOU window). On failure it wraps ErrWorkspaceForbidden with a human
// readable reason that tells the operator which rule tripped.
//
// Pipeline order matters and is the same as §6 of the plan:
//  1. Empty/whitespace check.
//  2. Reject NUL, newlines, control chars (<0x20 except \t).
//  3. NFC normalize (NFD→NFC on macOS so a ≠ NFD-a bypass cannot happen).
//  4. filepath.Abs + filepath.Clean. Rejects relative paths when no
//     AllowRoot anchor exists; allows cwd-relative when AllowRoot is set
//     is out of scope for beta.1 (fail-closed).
//  5. Length check.
//  6. filepath.EvalSymlinks — any component pointing outside the tree
//     that would pass the Rel check otherwise gets rewritten to its
//     actual target, and that target is what gets Rel/Denylist-checked.
//  7. Denylist EqualFold match (both exact and is-descendant-of).
//  8. Rel(AllowRoot, canonical) — rejects ".." prefix or absolute result.
//     Only skipped when AllowTrust is true.
//
// Only step 6 (EvalSymlinks) touches the filesystem.
func (p Policy) Validate(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%w: workspace path empty", ErrWorkspaceForbidden)
	}

	if err := checkForbiddenRunes(raw); err != nil {
		return "", err
	}

	// NFC-normalize before any path manipulation. On macOS, filenames can
	// round-trip as NFD from the filesystem but the allow-root is stored
	// as the user typed it (NFC from a shell); without this step, a path
	// that looks like "café" but is encoded as NFD a+◌́ can escape the Rel
	// check.
	nfc := norm.NFC.String(raw)

	if !filepath.IsAbs(nfc) {
		return "", fmt.Errorf("%w: path must be absolute, got %q", ErrWorkspaceForbidden, raw)
	}

	// Clean removes ".." and "." segments. Abs is a no-op for already-absolute
	// paths but kept defensively so callers passing "/foo/." get normalized.
	abs, err := filepath.Abs(nfc)
	if err != nil {
		return "", fmt.Errorf("%w: abs: %v", ErrWorkspaceForbidden, err)
	}
	cleaned := filepath.Clean(abs)

	if len(cleaned) > MaxPathLen {
		return "", fmt.Errorf("%w: path too long (%d > %d)", ErrWorkspaceForbidden, len(cleaned), MaxPathLen)
	}

	// EvalSymlinks returns an error if the path does not exist. That is the
	// correct behavior for beta.1: a workspace the operator names must exist
	// before the agent is created. Operators who want auto-creation use a
	// wrapper script; dclaw itself does not mkdir behind the user's back.
	canonical, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", fmt.Errorf("%w: resolve symlinks %q: %v", ErrWorkspaceForbidden, cleaned, err)
	}

	// Denylist match. Exact EqualFold (APFS-safe) or descendant-of-entry.
	// Descendant match covers workspaces like /Library/Preferences or
	// /etc/subdir — the Rel check below would also reject these when an
	// AllowRoot is configured, but the denylist catches them earlier
	// and produces a clearer error message ("denylist" vs "not under
	// allow-root"). With AllowTrust=true, the descendant check is the
	// only line of defense since Rel is skipped.
	for _, entry := range p.Denylist {
		if pathEqualFold(canonical, entry) {
			return "", fmt.Errorf("%w: %q is on the system-path denylist (%s)", ErrWorkspaceForbidden, canonical, entry)
		}
		if isUnderFold(canonical, entry) {
			return "", fmt.Errorf("%w: %q is under denylisted root %q", ErrWorkspaceForbidden, canonical, entry)
		}
	}

	// AllowTrust bypasses the Rel check (this is the --workspace-trust path)
	// but all prior invariants still applied.
	if p.AllowTrust {
		return canonical, nil
	}

	if p.AllowRoot == "" {
		return "", fmt.Errorf("%w: no workspace-root configured — run 'dclaw config set workspace-root <path>'", ErrWorkspaceForbidden)
	}

	// Canonicalize AllowRoot too; if the operator configured the root via a
	// symlink we want the Rel check to operate on real paths.
	rootNFC := norm.NFC.String(p.AllowRoot)
	rootAbs, err := filepath.Abs(rootNFC)
	if err != nil {
		return "", fmt.Errorf("%w: abs allow-root: %v", ErrWorkspaceForbidden, err)
	}
	rootClean := filepath.Clean(rootAbs)
	rootCanon, err := filepath.EvalSymlinks(rootClean)
	if err != nil {
		// If the configured root does not exist, the operator's config is
		// broken. Surface that as forbidden rather than letting it bypass.
		return "", fmt.Errorf("%w: resolve allow-root %q: %v", ErrWorkspaceForbidden, rootClean, err)
	}

	// APFS is typically case-insensitive: EvalSymlinks returns whichever
	// casing was used to create the path, so the operator may have
	// different casing on the canonical vs. the root. filepath.Rel does
	// byte-comparison, so we pre-canonicalize to a common casing via
	// EqualFold: first check a case-insensitive prefix match, then
	// recompute Rel against the root spelled with the canonical's prefix
	// casing so the returned rel is "subdir/leaf" not "../ROOT/subdir".
	if !isRootOrUnderFold(canonical, rootCanon) {
		return "", fmt.Errorf("%w: %q is not under allow-root %q", ErrWorkspaceForbidden, canonical, rootCanon)
	}

	return canonical, nil
}

// isRootOrUnderFold reports whether canonical is the root itself or a
// descendant of the root, matching case-insensitively for APFS.
func isRootOrUnderFold(canonical, root string) bool {
	c := filepath.Clean(canonical)
	r := filepath.Clean(root)
	if strings.EqualFold(c, r) {
		return true
	}
	prefix := r
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if len(c) < len(prefix) {
		return false
	}
	return strings.EqualFold(c[:len(prefix)], prefix)
}

// checkForbiddenRunes rejects paths containing NUL, newlines, or ASCII
// control characters (except tab). NUL defeats C-string-level injection
// in any downstream library; newlines defeat audit-log poisoning.
func checkForbiddenRunes(raw string) error {
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == 0x00 {
			return fmt.Errorf("%w: NUL byte at index %d", ErrWorkspaceForbidden, i)
		}
		if c == '\n' || c == '\r' {
			return fmt.Errorf("%w: newline at index %d", ErrWorkspaceForbidden, i)
		}
		// Reject C0 control chars except tab (0x09). Tab is legal in POSIX
		// paths and shows up occasionally in automated tools.
		if c < 0x20 && c != '\t' {
			return fmt.Errorf("%w: control byte 0x%02x at index %d", ErrWorkspaceForbidden, c, i)
		}
	}
	return nil
}

// pathEqualFold compares two paths case-insensitively. On APFS, filesystem
// case-insensitivity means "/Etc" and "/etc" resolve to the same inode;
// EvalSymlinks returns whichever casing was used to create the path, so
// we cannot rely on exact-match for the denylist.
func pathEqualFold(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// isUnderFold reports whether child is a descendant of parent, using
// EqualFold on each segment. Matches the intent "reject anything under
// /etc" regardless of APFS case mangling.
func isUnderFold(child, parent string) bool {
	c := filepath.Clean(child)
	p := filepath.Clean(parent)
	if p == "/" {
		// Everything is under /; but Validate has already matched the
		// exact "/" case via pathEqualFold, so descendant-of-/ would
		// reject every path. Skip.
		return false
	}
	// Append separator so "/Library/Preferences" does not match
	// "/LibraryOther".
	prefix := p + string(filepath.Separator)
	if len(c) < len(prefix) {
		return false
	}
	return strings.EqualFold(c[:len(prefix)], prefix)
}
