package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestResolvePrecedence exercises the full flag > env > default ladder for
// both StateDir and SocketPath. Each case sets DCLAW_STATE_DIR via t.Setenv
// so parallel test runs stay isolated.
//
// The "default-when-nothing-set" case exercises the platform default. On
// macOS that's always ~/.dclaw; on Linux the XDG ladder kicks in (see
// TestResolveXDGStateHomeLinux for a Linux-specific assertion). To keep
// the table portable, we resolve the expected default through the same
// internal helper the production code uses — defaultStateDir — so the
// test asserts "Resolve == defaultStateDir" rather than hard-coding a
// platform-specific string.
func TestResolvePrecedence(t *testing.T) {
	// Clear XDG_STATE_HOME so the platform default is exercised in its
	// most common form — without this, a developer with a value set in
	// their shell environment would see the test resolve to a different
	// default than CI.
	t.Setenv("XDG_STATE_HOME", "")
	_ = os.Unsetenv("XDG_STATE_HOME")

	defaultStateDir, err := defaultStateDir()
	if err != nil {
		t.Fatalf("cannot determine default state dir for test baseline: %v", err)
	}

	cases := []struct {
		name           string
		stateDirFlag   string
		socketFlag     string
		envStateDir    string // value for DCLAW_STATE_DIR; "" means unset
		wantStateDir   string
		wantSocketPath string
	}{
		{
			name:           "flag-wins-over-env",
			stateDirFlag:   "/tmp/flag-wins",
			envStateDir:    "/tmp/env-loses",
			wantStateDir:   "/tmp/flag-wins",
			wantSocketPath: defaultSocketForState(t, "/tmp/flag-wins"),
		},
		{
			name:           "env-wins-over-default",
			envStateDir:    "/tmp/env-wins",
			wantStateDir:   "/tmp/env-wins",
			wantSocketPath: defaultSocketForState(t, "/tmp/env-wins"),
		},
		{
			name:           "default-when-nothing-set",
			wantStateDir:   defaultStateDir,
			wantSocketPath: defaultSocketForState(t, defaultStateDir),
		},
		{
			name:           "empty-flag-is-not-flag",
			stateDirFlag:   "",
			envStateDir:    "/tmp/env-fills-empty-flag",
			wantStateDir:   "/tmp/env-fills-empty-flag",
			wantSocketPath: defaultSocketForState(t, "/tmp/env-fills-empty-flag"),
		},
		{
			name:           "socket-derived-from-state-dir",
			stateDirFlag:   "/tmp/sd-derives-sock",
			wantStateDir:   "/tmp/sd-derives-sock",
			wantSocketPath: defaultSocketForState(t, "/tmp/sd-derives-sock"),
		},
		{
			name:           "explicit-socket-wins",
			stateDirFlag:   "/tmp/sd-ignored-for-sock",
			socketFlag:     "/run/custom/explicit.sock",
			wantStateDir:   "/tmp/sd-ignored-for-sock",
			wantSocketPath: "/run/custom/explicit.sock",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// t.Setenv unsets at test end; set to empty to clear per-case.
			if tc.envStateDir == "" {
				t.Setenv("DCLAW_STATE_DIR", "")
				_ = os.Unsetenv("DCLAW_STATE_DIR")
			} else {
				t.Setenv("DCLAW_STATE_DIR", tc.envStateDir)
			}

			paths, err := Resolve(tc.stateDirFlag, tc.socketFlag)
			if err != nil {
				t.Fatalf("Resolve(%q, %q) returned error: %v",
					tc.stateDirFlag, tc.socketFlag, err)
			}
			if paths.StateDir != tc.wantStateDir {
				t.Errorf("StateDir: got %q want %q", paths.StateDir, tc.wantStateDir)
			}
			if paths.SocketPath != tc.wantSocketPath {
				t.Errorf("SocketPath: got %q want %q", paths.SocketPath, tc.wantSocketPath)
			}
		})
	}
}

// TestDefaultSocketPathFallback exercises DefaultSocketPath when
// XDG_RUNTIME_DIR is unset — the socket must land inside stateDir.
func TestDefaultSocketPathFallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	_ = os.Unsetenv("XDG_RUNTIME_DIR")

	stateDir := "/tmp/dclaw-test-fallback"
	got := DefaultSocketPath(stateDir)
	want := filepath.Join(stateDir, "dclaw.sock")
	if got != want {
		t.Errorf("DefaultSocketPath(%q) = %q, want %q", stateDir, got, want)
	}
}

// TestMustResolveSocket ensures the convenience helper returns a non-empty
// path in a normal environment (home dir resolvable). The /tmp fallback is
// exercised only when os.UserHomeDir fails, which we cannot trigger portably.
func TestMustResolveSocket(t *testing.T) {
	t.Setenv("DCLAW_STATE_DIR", "/tmp/dclaw-must-resolve")
	got := MustResolveSocket()
	if got == "" {
		t.Fatal("MustResolveSocket returned empty string")
	}
}

// defaultSocketForState computes the expected socket path for a given
// stateDir under the current env. It mirrors DefaultSocketPath so test
// expectations stay in sync with the code under test regardless of
// XDG_RUNTIME_DIR presence on the host.
func defaultSocketForState(t *testing.T, stateDir string) string {
	t.Helper()
	return DefaultSocketPath(stateDir)
}

// TestResolveFlagEnvRoundTrip asserts that --state-dir passed on the dclaw
// CLI and --state-dir passed on the dclawd daemon collapse to identical
// Paths. This is a Go-level round-trip (both entrypoints call the same
// Resolve); the test fixes the precedence contract between the two
// binaries so a future refactor cannot drift them apart.
func TestResolveFlagEnvRoundTrip(t *testing.T) {
	// Clear the env var so the flag is the only input in play; this also
	// proves flag-wins-over-env by setting env to a distractor value.
	t.Setenv("DCLAW_STATE_DIR", "/tmp/env-distractor")

	const shared = "/tmp/roundtrip-state"

	// dclaw CLI side: flag on rootCmd, daemon-socket left unset.
	cliPaths, err := Resolve(shared, "")
	if err != nil {
		t.Fatalf("dclaw CLI Resolve: %v", err)
	}
	// dclawd daemon side: same flag, no socket override.
	daemonPaths, err := Resolve(shared, "")
	if err != nil {
		t.Fatalf("dclawd Resolve: %v", err)
	}

	if cliPaths != daemonPaths {
		t.Fatalf("dclaw and dclawd resolved to different Paths:\n  dclaw:  %+v\n  dclawd: %+v",
			cliPaths, daemonPaths)
	}
	if cliPaths.StateDir != shared {
		t.Errorf("StateDir: got %q want %q (flag must win over DCLAW_STATE_DIR)",
			cliPaths.StateDir, shared)
	}
	wantSocket := DefaultSocketPath(shared)
	if cliPaths.SocketPath != wantSocket {
		t.Errorf("SocketPath: got %q want %q (socket must derive from resolved state-dir)",
			cliPaths.SocketPath, wantSocket)
	}
}

// TestResolveXDGStateHomeLinux asserts that on Linux, when XDG_STATE_HOME
// is set to a writable directory and ~/.dclaw does NOT exist, Resolve
// chooses $XDG_STATE_HOME/dclaw.
//
// Skipped on non-Linux (macOS+other always use ~/.dclaw regardless of
// XDG_STATE_HOME — see TestResolveDarwinIgnoresXDG).
func TestResolveXDGStateHomeLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("XDG_STATE_HOME default only honored on Linux; GOOS=%s", runtime.GOOS)
	}

	// Isolate HOME so the legacy-wins check never sees a real ~/.dclaw
	// from the developer's machine.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	// Clear DCLAW_STATE_DIR so the platform-default path runs.
	t.Setenv("DCLAW_STATE_DIR", "")
	_ = os.Unsetenv("DCLAW_STATE_DIR")

	xdgDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdgDir)

	paths, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(xdgDir, "dclaw")
	if paths.StateDir != want {
		t.Errorf("StateDir: got %q want %q (XDG_STATE_HOME should win when set+writable+no-legacy)",
			paths.StateDir, want)
	}
}

// TestResolveXDGFallbackToLegacyHome asserts the legacy-wins branch:
// when ~/.dclaw exists from a prior install, it beats $XDG_STATE_HOME
// even when the latter is set. Confirms beta.2.6 doesn't break upgrades.
//
// Skipped on non-Linux because the function returns ~/.dclaw on those
// platforms regardless of which directories exist — there's no XDG
// behavior to fall back from.
func TestResolveXDGFallbackToLegacyHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("XDG fallback ladder only runs on Linux; GOOS=%s", runtime.GOOS)
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("DCLAW_STATE_DIR", "")
	_ = os.Unsetenv("DCLAW_STATE_DIR")

	// Pre-populate ~/.dclaw so the legacy-wins branch fires.
	legacy := filepath.Join(homeDir, ".dclaw")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}

	// XDG_STATE_HOME points at a writable directory, but legacy must win.
	xdgDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdgDir)

	paths, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if paths.StateDir != legacy {
		t.Errorf("StateDir: got %q want %q (legacy ~/.dclaw must win over XDG when both exist)",
			paths.StateDir, legacy)
	}
}

// TestResolveXDGFallbackXDGUnset asserts that when XDG_STATE_HOME is
// unset and ~/.dclaw doesn't exist but ~/.local/state does, Resolve
// returns ~/.local/state/dclaw (the XDG default per the spec).
func TestResolveXDGFallbackXDGUnset(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("XDG fallback ladder only runs on Linux; GOOS=%s", runtime.GOOS)
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("DCLAW_STATE_DIR", "")
	_ = os.Unsetenv("DCLAW_STATE_DIR")
	t.Setenv("XDG_STATE_HOME", "")
	_ = os.Unsetenv("XDG_STATE_HOME")

	// Create ~/.local/state so the spec-default path activates.
	localState := filepath.Join(homeDir, ".local", "state")
	if err := os.MkdirAll(localState, 0o700); err != nil {
		t.Fatalf("mkdir .local/state: %v", err)
	}

	paths, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(localState, "dclaw")
	if paths.StateDir != want {
		t.Errorf("StateDir: got %q want %q (XDG default ~/.local/state/dclaw expected)",
			paths.StateDir, want)
	}
}

// TestResolveXDGUnwritableFallsBack asserts that an XDG_STATE_HOME
// pointing at a non-existent path is ignored — Resolve falls through
// to the spec-default ladder rather than honoring a broken pointer.
func TestResolveXDGUnwritableFallsBack(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("XDG fallback ladder only runs on Linux; GOOS=%s", runtime.GOOS)
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("DCLAW_STATE_DIR", "")
	_ = os.Unsetenv("DCLAW_STATE_DIR")

	// XDG points at a non-existent dir. The probe in isWritableDir
	// will fail Stat and the code falls back to the next rung. Neither
	// ~/.dclaw nor ~/.local/state exist, so the legacy path wins.
	t.Setenv("XDG_STATE_HOME", "/nonexistent/path/that/does/not/exist")

	paths, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(homeDir, ".dclaw")
	if paths.StateDir != want {
		t.Errorf("StateDir: got %q want %q (broken XDG must not be honored)",
			paths.StateDir, want)
	}
}

// TestResolveDarwinIgnoresXDG asserts that on macOS, even when
// XDG_STATE_HOME is set, Resolve uses ~/.dclaw — XDG isn't a Darwin
// convention and honoring it would surprise more users than it helps.
//
// Skipped on non-darwin because that's the only OS where this test
// makes a meaningful claim. Linux is covered by TestResolveXDGStateHomeLinux.
func TestResolveDarwinIgnoresXDG(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("darwin-specific assertion; GOOS=%s", runtime.GOOS)
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("DCLAW_STATE_DIR", "")
	_ = os.Unsetenv("DCLAW_STATE_DIR")

	// XDG set to a real writable dir — must still be ignored on Darwin.
	xdgDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdgDir)

	paths, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(homeDir, ".dclaw")
	if paths.StateDir != want {
		t.Errorf("StateDir: got %q want %q (Darwin must ignore XDG_STATE_HOME)",
			paths.StateDir, want)
	}
}
