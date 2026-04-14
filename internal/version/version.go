// Package version exposes build metadata stamped in at link time via -ldflags.
// A binary built without -X flags reports "dev" for every field.
package version

import "runtime"

// These vars are overwritten by -ldflags at build time. Keep them as `var`,
// not `const`, so the linker can set them.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// GoVersion returns the Go version this binary was compiled with.
func GoVersion() string {
	return runtime.Version()
}
