//go:build windows

package paths

import (
	"fmt"
	"os"
)

// OpenSafe is a Windows stub for the POSIX-only TOCTOU-closing helper.
// dclaw is not actively tested on Windows (beta.2.6 / Plan §12 #5 added
// the Windows denylist as defensive scaffolding, not a supported runtime).
// The real implementation requires O_NOFOLLOW + O_DIRECTORY plus the
// fd-canonicalization fcntl(F_GETPATH) / readlink(/proc/self/fd) calls,
// neither of which has a clean Windows equivalent.
//
// Returning an error here keeps the daemon failing closed: any caller
// that reaches OpenSafe on Windows surfaces a clear "not supported"
// rather than silently bypassing the post-Validate re-canonicalize
// step that the lifecycle layer relies on.
//
// When dclaw gains a Windows port that includes the workspace
// validator + lifecycle, this file will be replaced with a real
// implementation backed by `windows.CreateFile` + `windows.GetFinalPathNameByHandle`.
// Until then the build-tag split lives here so `GOOS=windows go vet`
// stays clean.
func OpenSafe(path string, policy Policy) (*os.File, string, error) {
	return nil, "", fmt.Errorf("%w: paths.OpenSafe not supported on Windows; dclaw's Windows posture is denylist-only scaffolding (beta.2.6)", ErrWorkspaceForbidden)
}
