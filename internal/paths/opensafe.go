//go:build !windows

package paths

import (
	"fmt"
	"os"
	"syscall"
)

// OpenSafe opens path with O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC, re-canonicalizes
// the open fd through the kernel, and re-validates against the supplied
// policy. The returned *os.File holds the directory open; callers pass the
// returned canonical path to Docker bind-mount and keep the fd alive until
// the container is created. This closes the TOCTOU window: between Validate
// and mount, a swap of the target to a symlinked denylisted directory is
// caught because OpenSafe re-resolves through the held fd rather than
// re-walking the path.
//
// Returns (fd, canonical, error). Caller owns fd.Close().
//
//   - On Linux, canonical is read from /proc/self/fd/<N> via os.Readlink.
//   - On darwin, canonical is obtained via fcntl(F_GETPATH).
//
// O_NOFOLLOW ensures the final component is not itself a symlink; combined
// with Policy.Validate's EvalSymlinks pass, that means a symlink path
// swapped in after Validate succeeds either fails open (NOFOLLOW) or the
// canonical path this function returns reveals the escape for the policy
// re-check.
func OpenSafe(path string, policy Policy) (*os.File, string, error) {
	// O_DIRECTORY: fail if path is not a directory.
	// O_NOFOLLOW: do not traverse a terminal symlink.
	// O_CLOEXEC: fd closed on exec; irrelevant for dclawd (no exec before
	// container create) but standard hygiene.
	flags := os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_DIRECTORY | syscall.O_CLOEXEC
	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, "", fmt.Errorf("%w: open %q: %v", ErrWorkspaceForbidden, path, err)
	}

	canonical, err := canonicalFromFd(f)
	if err != nil {
		f.Close()
		return nil, "", fmt.Errorf("%w: canonicalize fd: %v", ErrWorkspaceForbidden, err)
	}

	// Re-run the policy against the canonical path the kernel reports.
	// If a symlink swap happened between Validate and the O_NOFOLLOW open,
	// the kernel's notion of the path differs from the input and the
	// re-validation will catch a denylist/rel escape.
	reCanonical, err := policy.Validate(canonical)
	if err != nil {
		f.Close()
		return nil, "", fmt.Errorf("re-validate %q: %w", canonical, err)
	}

	return f, reCanonical, nil
}
