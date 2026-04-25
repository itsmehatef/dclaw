package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/paths"
)

// resetDoctorState clears the package-level state doctor's checks rely
// on so subsequent tests do not see leftover values from earlier runs.
// Specifically:
//   - stateDirFlag and daemonSocket are persistent flag values; the
//     normal CLI run sets them via PersistentPreRunE, but tests that
//     skip rootCmd.Execute must set them by hand and unset on cleanup.
//   - doctorDenylist is replaced with a t.TempDir-friendly subset for
//     the test's lifetime, mirroring init_cmd_test.go's resetInitFlags.
//   - outputFormat is restored to "table" so a downstream test seeing
//     a stale "json" does not silently switch to JSON output.
func resetDoctorState(t *testing.T) {
	t.Helper()
	prevStateDirFlag := stateDirFlag
	prevDaemonSocket := daemonSocket
	prevDenylist := doctorDenylist
	prevOutputFormat := outputFormat
	doctorDenylist = stripTempDirEntriesForDoctorTest(paths.DefaultDenylist)
	t.Cleanup(func() {
		stateDirFlag = prevStateDirFlag
		daemonSocket = prevDaemonSocket
		doctorDenylist = prevDenylist
		outputFormat = prevOutputFormat
	})
}

// stripTempDirEntriesForDoctorTest removes denylist entries that overlap
// with t.TempDir's storage on macOS. Mirrors the helper in
// init_cmd_test.go and internal/paths/policy_test.go: t.TempDir() on
// macOS resolves under /private/var/folders/..., which is descendant
// of /var and /private/var. Without this strip, every doctor workspace
// OK test would be rejected by the validator because the temp dir is
// "under denylisted root /var".
func stripTempDirEntriesForDoctorTest(in []string) []string {
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

// findCheck returns the result for the given check name or fails the
// test if the check was missing — guards against typos in the test
// expectation that would otherwise silently pass.
func findCheck(t *testing.T, results []CheckResult, name string) CheckResult {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("check %q not found in results: %+v", name, results)
	return CheckResult{}
}

// TestDoctorAllChecksWithoutDaemon — fresh state-dir, no daemon, no
// docker reachable, no config. Asserts:
//   - config_resolved=OK
//   - workspace_root_configured=FAIL (no init done yet)
//   - workspace_root_valid=WARN (skipped: prior FAIL)
//   - daemon_reachable=WARN (not running)
//   - docker_reachable=OK or WARN (host-dependent; FAIL would be a regression)
//   - agent_image_present=OK or WARN (host-dependent; FAIL is a regression)
//   - audit_log_writable=OK (state-dir is writable t.TempDir)
//
// Top-level exit decision: at least one FAIL ⇒ exit 1. We assert the
// FAIL by counting failures rather than re-deriving the exit code,
// because runAllChecks is the unit under test here; runDoctor's
// os.Exit is exercised separately by the workspace-forbidden subprocess
// test.
func TestDoctorAllChecksWithoutDaemon(t *testing.T) {
	resetDoctorState(t)

	stateDir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", stateDir)
	t.Setenv("HOME", t.TempDir())

	stateDirFlag = stateDir
	// Point the daemon socket at a path inside the temp state-dir that
	// definitely does not exist, so daemon_reachable WARNs deterministically.
	daemonSocket = filepath.Join(stateDir, "dclaw.sock")

	results := runAllChecks(context.Background())

	if got := findCheck(t, results, "config_resolved"); got.State != CheckOK {
		t.Errorf("config_resolved: state=%s msg=%q, want OK", got.State, got.Message)
	}
	if got := findCheck(t, results, "workspace_root_configured"); got.State != CheckFail {
		t.Errorf("workspace_root_configured: state=%s msg=%q, want FAIL", got.State, got.Message)
	}
	if got := findCheck(t, results, "workspace_root_valid"); got.State != CheckWarn {
		t.Errorf("workspace_root_valid: state=%s msg=%q, want WARN (skipped)", got.State, got.Message)
	}
	if got := findCheck(t, results, "daemon_reachable"); got.State != CheckWarn {
		t.Errorf("daemon_reachable: state=%s msg=%q, want WARN", got.State, got.Message)
	}
	// docker_reachable depends on the host: CI may have docker, dev
	// macs may not. We only assert the state is OK or WARN; FAIL would
	// be a regression in our error classification.
	if got := findCheck(t, results, "docker_reachable"); got.State != CheckOK && got.State != CheckWarn {
		t.Errorf("docker_reachable: state=%s msg=%q, want OK or WARN", got.State, got.Message)
	}
	// agent_image_present is OK (image present locally) or WARN (image
	// missing or docker unreachable). FAIL is a regression.
	if got := findCheck(t, results, "agent_image_present"); got.State != CheckOK && got.State != CheckWarn {
		t.Errorf("agent_image_present: state=%s msg=%q, want OK or WARN", got.State, got.Message)
	}
	if got := findCheck(t, results, "audit_log_writable"); got.State != CheckOK {
		t.Errorf("audit_log_writable: state=%s msg=%q, want OK", got.State, got.Message)
	}

	failCount := 0
	for _, r := range results {
		if r.State == CheckFail {
			failCount++
		}
	}
	if failCount == 0 {
		t.Errorf("expected ≥1 FAIL (workspace_root_configured), got none: %+v", results)
	}
}

// TestDoctorAfterInit — pre-write a config.toml with workspace-root
// pointing at a t.TempDir. Asserts:
//   - workspace_root_configured=OK
//   - workspace_root_valid=OK
//   - daemon_reachable=WARN (not running)
//   - audit_log_writable=OK
//   - exit-code derivation: zero FAILs ⇒ exit 0
//
// Replicates the post-`dclaw init` happy path for a developer running
// `dclaw doctor` before they start the daemon for the first time.
func TestDoctorAfterInit(t *testing.T) {
	resetDoctorState(t)

	stateDir := t.TempDir()
	wsRoot := t.TempDir()

	t.Setenv("DCLAW_STATE_DIR", stateDir)
	t.Setenv("HOME", t.TempDir())

	stateDirFlag = stateDir
	daemonSocket = filepath.Join(stateDir, "dclaw.sock")

	if err := config.WriteConfigFile(stateDir, config.FileConfig{WorkspaceRoot: wsRoot}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	results := runAllChecks(context.Background())

	if got := findCheck(t, results, "workspace_root_configured"); got.State != CheckOK {
		t.Errorf("workspace_root_configured: state=%s msg=%q, want OK", got.State, got.Message)
	}
	if got := findCheck(t, results, "workspace_root_valid"); got.State != CheckOK {
		t.Errorf("workspace_root_valid: state=%s msg=%q, want OK", got.State, got.Message)
	}
	if got := findCheck(t, results, "daemon_reachable"); got.State != CheckWarn {
		t.Errorf("daemon_reachable: state=%s msg=%q, want WARN", got.State, got.Message)
	}
	if got := findCheck(t, results, "audit_log_writable"); got.State != CheckOK {
		t.Errorf("audit_log_writable: state=%s msg=%q, want OK", got.State, got.Message)
	}

	failCount := 0
	for _, r := range results {
		if r.State == CheckFail {
			failCount++
		}
	}
	if failCount != 0 {
		t.Errorf("expected zero FAILs, got %d: %+v", failCount, results)
	}
}

// dclawTestBinary is the cached path to a `go build`-produced dclaw
// binary used by TestDoctorWorkspaceForbidden. Built once on demand
// (lazy because not every doctor test needs a subprocess) and reused
// across subprocess test invocations within a single test run.
//
// Why not `go run`: `go run` does not propagate the subprocess's exit
// code; it always exits 1 on a non-zero subprocess exit. Doctor's
// rejection path returns ExitDataErr (65), and the test must verify
// the exact code, so we build the binary first and exec it directly.
var dclawTestBinary string

// buildDclawForTest builds the dclaw CLI to a temp path and returns
// it. Cached in dclawTestBinary so a multi-test run pays the build
// cost only once. The binary lives in os.TempDir() (not t.TempDir())
// so it survives across subtests; t.TempDir would clean up between
// each test case.
func buildDclawForTest(t *testing.T) string {
	t.Helper()
	if dclawTestBinary != "" {
		if _, err := os.Stat(dclawTestBinary); err == nil {
			return dclawTestBinary
		}
	}
	dir, err := os.MkdirTemp("", "dclaw-doctor-bin-*")
	if err != nil {
		t.Fatalf("mkdir bin tempdir: %v", err)
	}
	bin := filepath.Join(dir, "dclaw")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/dclaw")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
	dclawTestBinary = bin
	return bin
}

// TestDoctorWorkspaceForbidden — `dclaw doctor workspace /etc` returns
// exit 65 with denylist rejection. We re-exec a freshly built dclaw
// binary so the os.Exit path is actually taken — an in-process call
// would still invoke os.Exit and terminate the test process — and so
// we can read the precise exit code. (Using `go run` here would always
// see exit 1 because go run does not propagate child exit codes.)
func TestDoctorWorkspaceForbidden(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipping in -short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("paths.DefaultDenylist is unix-shaped; doctor workspace is exercised on unix only")
	}

	bin := buildDclawForTest(t)

	stateDir := t.TempDir()
	cmd := exec.Command(bin,
		"--state-dir", stateDir,
		"doctor", "workspace", "/etc",
	)
	// Inherit the runtime env (PATH, GOCACHE, etc.) but redirect
	// DCLAW_STATE_DIR. We deliberately do NOT redirect HOME so the
	// subprocess does not pollute t.TempDir's cleanup with module
	// caches; setting only the state-dir suffices.
	cmd.Env = append(os.Environ(),
		"DCLAW_STATE_DIR="+stateDir,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err == nil {
		t.Fatalf("expected exit code 65, got 0; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != ExitDataErr {
		t.Fatalf("expected exit %d, got %d; stdout=%q stderr=%q", ExitDataErr, exitErr.ExitCode(), stdout.String(), stderr.String())
	}

	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "denylist") && !strings.Contains(combined, "denylisted") {
		t.Errorf("expected 'denylist' in output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// TestDoctorWorkspaceOK — `doctor workspace <tmpdir>` with a configured
// workspace-root + a non-denylisted tmpdir under that root returns 0.
// Tmp dir on macOS resolves under /private/var/folders/...; the
// validator's denylist would normally reject that, but resetDoctorState
// strips the macOS-temp-dir-overlapping entries from doctorDenylist.
//
// We configure workspace-root to the parent of the target so the path
// passes the AllowRoot prefix check (AllowTrust=false on the doctor
// workspace subcommand mirrors the daemon's posture exactly).
//
// This test exercises the in-process RunE rather than re-exec'ing
// because RunE returns nil on success; only the os.Exit-on-rejection
// path needs the subprocess form (covered by TestDoctorWorkspaceForbidden).
func TestDoctorWorkspaceOK(t *testing.T) {
	resetDoctorState(t)

	stateDir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", stateDir)
	t.Setenv("HOME", t.TempDir())

	stateDirFlag = stateDir

	parent := t.TempDir()
	target := filepath.Join(parent, "ws")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	if err := config.WriteConfigFile(stateDir, config.FileConfig{WorkspaceRoot: parent}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	doctorWorkspaceCmd.SetOut(&out)
	doctorWorkspaceCmd.SetErr(&out)
	if err := runDoctorWorkspace(doctorWorkspaceCmd, []string{target}); err != nil {
		t.Fatalf("runDoctorWorkspace: %v", err)
	}
	if !strings.Contains(out.String(), "OK:") {
		t.Errorf("expected 'OK:' in output, got %q", out.String())
	}

	// JSON variant — same target should produce ok=true.
	outputFormat = "json"
	out.Reset()
	if err := runDoctorWorkspace(doctorWorkspaceCmd, []string{target}); err != nil {
		t.Fatalf("runDoctorWorkspace JSON: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json decode: %v; raw=%q", err, out.String())
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Errorf("expected ok=true, got payload=%+v", payload)
	}
}
