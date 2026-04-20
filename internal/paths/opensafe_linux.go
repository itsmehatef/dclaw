//go:build linux

package paths

import (
	"fmt"
	"os"
	"strconv"
)

// canonicalFromFd returns the kernel's notion of the path for the given
// open fd on Linux by reading /proc/self/fd/<N>. procfs exposes a symlink
// whose target is the current path of the inode the fd points to. Reading
// that link after an O_NOFOLLOW open is the Linux equivalent of darwin's
// fcntl(F_GETPATH) — it reveals the real path of the actual open target
// even if a symlink swap changed what walking the original path would
// resolve to.
//
// Callers must hand the returned path back to Policy.Validate; a
// symlink-swap that relocates the target to a denylisted directory will
// show up there and fail the OpenSafe call.
func canonicalFromFd(f *os.File) (string, error) {
	target, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	if err != nil {
		return "", fmt.Errorf("readlink /proc/self/fd: %v", err)
	}
	return target, nil
}
