//go:build darwin

package paths

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// maxPathDarwin is MAXPATHLEN on macOS. Kept local so we do not grow the
// package public surface. xnu defines PATH_MAX = 1024 in <sys/syslimits.h>;
// we use 4096 to match the validator's MaxPathLen, because F_GETPATH will
// truncate silently if the buffer is undersized. PATH_MAX is advisory, not
// enforced at the syscall level, and buffers this size are cheap.
const maxPathDarwin = 4096

// canonicalFromFd returns the kernel's notion of the path for the given
// open fd on darwin via fcntl(F_GETPATH). fcntl writes a NUL-terminated
// C string into the caller-supplied buffer; we trim to the first NUL.
//
// This is the authoritative post-open canonical for the fd. If an attacker
// swapped a symlink between Validate and OpenSafe's O_NOFOLLOW open,
// F_GETPATH returns the path of the inode the caller actually got, not
// the symlink name. That string is then re-fed through Policy.Validate
// to reject any path that has escaped the allow-root or hit the denylist.
func canonicalFromFd(f *os.File) (string, error) {
	buf := make([]byte, maxPathDarwin)
	// Raw fcntl(F_GETPATH, buf) syscall. The x/sys/unix helpers assume
	// an int argument; F_GETPATH wants a pointer to a char buffer. Use
	// syscall.Syscall directly so we can pass &buf[0].
	_, _, errno := syscall.Syscall(
		syscall.SYS_FCNTL,
		f.Fd(),
		uintptr(unix.F_GETPATH),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if errno != 0 {
		return "", fmt.Errorf("fcntl(F_GETPATH): %v", errno)
	}
	// Trim to first NUL.
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n]), nil
}
