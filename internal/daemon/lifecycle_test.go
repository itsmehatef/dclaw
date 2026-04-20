package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmehatef/dclaw/internal/audit"
	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/paths"
	"github.com/itsmehatef/dclaw/internal/protocol"
)

// TestAgentCreateRejectsForbiddenWorkspace exercises the validator reject
// path end-to-end through Lifecycle.AgentCreate. A policy with a real
// allow-root under t.TempDir, an input path outside it — AgentCreate
// must return an error wrapping paths.ErrWorkspaceForbidden BEFORE any
// Docker call (so the nil docker client in the test is never touched).
func TestAgentCreateRejectsForbiddenWorkspace(t *testing.T) {
	repo := newTestRepo(t)
	stateDir := t.TempDir()
	auditLog, err := audit.New(stateDir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	defer auditLog.Close()

	allowRoot := filepath.Join(stateDir, "ws-root")
	if err := os.Mkdir(allowRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	policy := paths.Policy{
		AllowRoot: allowRoot,
		Denylist:  []string{"/etc", "/private/etc"},
	}

	// Docker is nil — the validator must reject before reaching CreateAgent.
	lc := daemon.NewLifecycle(silentLogger(), repo, nil, policy, auditLog)

	_, err = lc.AgentCreate(context.Background(), protocol.AgentCreateParams{
		Name:      "forbidden-agent",
		Image:     "dclaw-agent:v0.1",
		Workspace: "/etc",
	})
	if err == nil {
		t.Fatalf("expected error on forbidden workspace")
	}
	if !errors.Is(err, paths.ErrWorkspaceForbidden) {
		t.Fatalf("expected ErrWorkspaceForbidden wrap, got %v", err)
	}

	// Audit should have exactly one "forbidden" entry.
	entries := readAuditLines(t, stateDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].Outcome != "forbidden" {
		t.Fatalf("expected outcome=forbidden, got %q", entries[0].Outcome)
	}
	if entries[0].AgentName != "forbidden-agent" {
		t.Fatalf("expected agent_name=forbidden-agent, got %q", entries[0].AgentName)
	}
	if entries[0].PolicyVersion != paths.PolicyVersion {
		t.Fatalf("policy_version mismatch: got %d want %d", entries[0].PolicyVersion, paths.PolicyVersion)
	}
}

// TestAgentCreateTrustBypassesAllowRoot exercises the --workspace-trust
// bypass of the allow-root Rel check. A path OUTSIDE the allow-root plus
// a non-empty trust reason should pass validation; Denylist is still
// enforced (that's a separate test below). As with the forbidden test,
// docker is nil because validation happens before the Docker call — but
// this test confirms Validate+OpenSafe succeed and record one "trust"
// audit line. We catch the subsequent nil-docker panic with a recover.
func TestAgentCreateTrustBypassesAllowRoot(t *testing.T) {
	repo := newTestRepo(t)
	stateDir := t.TempDir()
	auditLog, err := audit.New(stateDir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	defer auditLog.Close()

	allowRoot := filepath.Join(stateDir, "ws-root")
	if err := os.Mkdir(allowRoot, 0o755); err != nil {
		t.Fatalf("mkdir allow-root: %v", err)
	}
	// Target is OUTSIDE the allow-root but on the filesystem so
	// EvalSymlinks succeeds. Without trust this would fail the Rel check.
	trusted := filepath.Join(stateDir, "outside-trusted")
	if err := os.Mkdir(trusted, 0o755); err != nil {
		t.Fatalf("mkdir trusted: %v", err)
	}

	policy := paths.Policy{
		AllowRoot: allowRoot,
		Denylist:  []string{"/etc", "/private/etc"},
	}
	lc := daemon.NewLifecycle(silentLogger(), repo, nil, policy, auditLog)

	// Call AgentCreate with trust. A nil docker means the code will
	// panic once validation passes — recover, then assert the validator
	// got past its gate and wrote the "trust" audit line.
	defer func() {
		_ = recover()
		entries := readAuditLines(t, stateDir)
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}
		if entries[0].Outcome != "trust" {
			t.Fatalf("expected outcome=trust, got %q (entry=%+v)", entries[0].Outcome, entries[0])
		}
		if entries[0].Reason != "testing" {
			t.Fatalf("expected reason=testing, got %q", entries[0].Reason)
		}
	}()

	_, _ = lc.AgentCreate(context.Background(), protocol.AgentCreateParams{
		Name:                 "trust-agent",
		Image:                "dclaw-agent:v0.1",
		Workspace:            trusted,
		WorkspaceTrustReason: "testing",
	})
}

// TestAgentCreateWritesAuditEntry is subsumed by the two tests above
// (each asserts exactly one audit line with the expected outcome). We
// keep this as a focused assertion that the audit writer is wired
// through the Lifecycle at all: empty workspace → no audit line (create
// still runs, but skips the validator branch entirely).
func TestAgentCreateNoAuditOnEmptyWorkspace(t *testing.T) {
	repo := newTestRepo(t)
	stateDir := t.TempDir()
	auditLog, err := audit.New(stateDir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	defer auditLog.Close()

	policy := paths.Policy{Denylist: []string{"/etc"}}
	lc := daemon.NewLifecycle(silentLogger(), repo, nil, policy, auditLog)

	// Empty Workspace + nil docker → validator skipped, CreateAgent(nil)
	// panics. Recover and assert no audit line was written.
	defer func() {
		_ = recover()
		entries := readAuditLines(t, stateDir)
		if len(entries) != 0 {
			t.Fatalf("expected no audit entries for empty-workspace path, got %d: %+v", len(entries), entries)
		}
	}()

	_, _ = lc.AgentCreate(context.Background(), protocol.AgentCreateParams{
		Name:  "empty-ws",
		Image: "dclaw-agent:v0.1",
		// Workspace omitted.
	})
}

// readAuditLines opens $stateDir/audit.log and returns every NDJSON line
// parsed into an audit.Record. Fresh file handle so any O_APPEND buffering
// in the logger has been flushed.
func readAuditLines(t *testing.T, stateDir string) []audit.Record {
	t.Helper()
	// The logger instance is still open in the test; O_SYNC flushes each
	// write so a separate open-for-read sees them. On some systems we need
	// to force any OS cache flush — not needed here because O_SYNC handles it.
	f, err := os.Open(filepath.Join(stateDir, "audit.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open audit.log: %v", err)
	}
	defer f.Close()

	var out []audit.Record
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		var rec audit.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("parse line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}
