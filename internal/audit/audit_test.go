package audit_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmehatef/dclaw/internal/audit"
)

// TestAuditLogAppendOnly writes three records, closes, reopens for read,
// and asserts that the three records are present in insertion order.
// Re-opening after Close ensures we are reading what actually hit disk
// (via O_SYNC) rather than a buffered view.
func TestAuditLogAppendOnly(t *testing.T) {
	dir := t.TempDir()
	logger, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}

	records := []struct {
		agent    string
		raw      string
		canon    string
		outcome  string
		reason   string
		polyVers int
	}{
		{"alice", "/work/alice", "/work/alice", "pass", "", 1},
		{"risky", "/etc", "/etc", "forbidden", "", 1},
		{"legacy", "/old", "/old", "trust", "migration", 1},
	}
	for _, r := range records {
		if err := logger.LogDecision(r.agent, r.raw, r.canon, r.outcome, r.reason, r.polyVers); err != nil {
			t.Fatalf("LogDecision: %v", err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Readback.
	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("open for read: %v", err)
	}
	defer f.Close()

	var got []audit.Record
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec audit.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %q not valid JSON: %v", line, err)
		}
		got = append(got, rec)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan err: %v", err)
	}

	if len(got) != len(records) {
		t.Fatalf("expected %d records, got %d", len(records), len(got))
	}
	for i, r := range records {
		if got[i].AgentName != r.agent {
			t.Fatalf("record %d agent mismatch: got %q want %q", i, got[i].AgentName, r.agent)
		}
		if got[i].RawInput != r.raw {
			t.Fatalf("record %d raw mismatch: got %q want %q", i, got[i].RawInput, r.raw)
		}
		if got[i].Outcome != r.outcome {
			t.Fatalf("record %d outcome mismatch: got %q want %q", i, got[i].Outcome, r.outcome)
		}
		if got[i].Reason != r.reason {
			t.Fatalf("record %d reason mismatch: got %q want %q", i, got[i].Reason, r.reason)
		}
		if got[i].PolicyVersion != r.polyVers {
			t.Fatalf("record %d policy_version mismatch: got %d want %d", i, got[i].PolicyVersion, r.polyVers)
		}
	}
}

// TestAuditLogJSONShape asserts every emitted line is NDJSON with every
// required field present and correctly typed, matching §7 of the plan.
func TestAuditLogJSONShape(t *testing.T) {
	dir := t.TempDir()
	logger, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	defer logger.Close()

	if err := logger.LogDecision("a", "raw", "canon", "pass", "", 1); err != nil {
		t.Fatalf("LogDecision: %v", err)
	}

	// Close so content is flushed and visible.
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		t.Fatalf("audit.log empty")
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatalf("line not valid JSON: %v\nline=%s", err, line)
	}
	// Every required field must be present.
	required := []string{"ts", "agent_name", "raw_input", "canonical", "outcome", "reason", "policy_version"}
	for _, k := range required {
		if _, ok := obj[k]; !ok {
			t.Fatalf("missing field %q in %s", k, line)
		}
	}
	// Shape: ts/string, agent_name/string, policy_version/number.
	if _, ok := obj["ts"].(string); !ok {
		t.Fatalf("ts is not string: %T", obj["ts"])
	}
	if _, ok := obj["agent_name"].(string); !ok {
		t.Fatalf("agent_name is not string: %T", obj["agent_name"])
	}
	if v, ok := obj["policy_version"].(float64); !ok || int(v) != 1 {
		t.Fatalf("policy_version not int=1: %v", obj["policy_version"])
	}
}

// TestAuditLogMissingStateDir verifies New refuses to operate without
// a state-dir (defensive: the daemon always supplies one, but we don't
// want a silent failure if a caller forgets).
func TestAuditLogMissingStateDir(t *testing.T) {
	if _, err := audit.New(""); err == nil {
		t.Fatalf("expected error on empty state dir")
	}
}
