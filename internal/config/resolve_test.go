package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolvePrecedence exercises the full flag > env > default ladder for
// both StateDir and SocketPath. Each case sets DCLAW_STATE_DIR via t.Setenv
// so parallel test runs stay isolated.
func TestResolvePrecedence(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot determine home dir for test baseline: %v", err)
	}
	defaultStateDir := filepath.Join(home, ".dclaw")

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
