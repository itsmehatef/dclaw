package audit_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestAuditLogConcurrentWrites spawns N goroutines that each call
// LogDecision once with a unique agent_name. After they all complete we
// read audit.log back and assert: every record is valid NDJSON, the line
// count equals N, and every expected agent_name (goroutine-0 through
// goroutine-(N-1)) appears exactly once. Run under -race to catch any
// future regression in the Logger's mutex; the combination of mutex +
// O_APPEND + sub-PIPE_BUF record sizes is what guarantees no torn writes
// or interleaved bytes between concurrent callers.
func TestAuditLogConcurrentWrites(t *testing.T) {
	const N = 20
	dir := t.TempDir()
	logger, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("goroutine-%d", idx)
			if err := logger.LogDecision(name, "/raw", "/canon", "pass", "", 1); err != nil {
				t.Errorf("LogDecision goroutine-%d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Readback. Each line must be valid NDJSON; agent_name set must equal
	// the expected set exactly (no duplicates, no losses).
	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("open audit.log: %v", err)
	}
	defer f.Close()

	seen := make(map[string]int, N)
	lines := 0
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines++
		var rec audit.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d not valid JSON: %v\nline=%s", lines, err, line)
		}
		seen[rec.AgentName]++
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan err: %v", err)
	}

	if lines != N {
		t.Fatalf("expected %d audit lines, got %d", N, lines)
	}
	for i := 0; i < N; i++ {
		name := fmt.Sprintf("goroutine-%d", i)
		count, ok := seen[name]
		if !ok {
			t.Errorf("expected agent_name %q in audit.log, missing", name)
			continue
		}
		if count != 1 {
			t.Errorf("expected agent_name %q exactly once, got %d", name, count)
		}
	}
}

// TestAuditLogRotationDefaults confirms the production constructor
// applies the documented defaults (10MB / 5 files). Inspection-only —
// no rotation actually fires here.
func TestAuditLogRotationDefaults(t *testing.T) {
	dir := t.TempDir()
	logger, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	defer logger.Close()

	if got, want := logger.MaxSize, int64(10*1024*1024); got != want {
		t.Fatalf("MaxSize: got %d, want %d", got, want)
	}
	if got, want := logger.MaxFiles, 5; got != want {
		t.Fatalf("MaxFiles: got %d, want %d", got, want)
	}
	// Sanity-check the exported constants too — if a future patch
	// changes the defaults, both sites should change together.
	if audit.DefaultMaxSize != 10*1024*1024 {
		t.Fatalf("DefaultMaxSize: got %d, want %d", audit.DefaultMaxSize, 10*1024*1024)
	}
	if audit.DefaultMaxFiles != 5 {
		t.Fatalf("DefaultMaxFiles: got %d, want 5", audit.DefaultMaxFiles)
	}
}

// TestAuditLogRotationSizeTriggers forces several rotations with
// MaxSize=200, MaxFiles=4 and asserts the resulting filesystem layout:
// audit.log holds the most recent record; audit.log.1, audit.log.2,
// audit.log.3 each hold older records; audit.log.4 does NOT exist
// (oldest cohort dropped on the rotation that would have created it).
//
// Convention: MaxFiles=N retains N total files — the active audit.log
// plus N-1 historical siblings audit.log.1 .. audit.log.{N-1}. The
// docs section in docs/workspace-root.md uses the same convention
// (default MaxFiles=5 ⇒ audit.log .. audit.log.4).
func TestAuditLogRotationSizeTriggers(t *testing.T) {
	dir := t.TempDir()
	logger, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	logger.MaxSize = 200
	logger.MaxFiles = 4

	// Each record (with our test names) is ~160 bytes JSON+newline.
	// With MaxSize=200 every second write forces a rotation: the
	// active file already holds the previous ~160-byte record, so
	// 160+160 > 200 trips the threshold check. 10 writes therefore
	// fire ~9 rotations — well past the 4 required to fully populate
	// (and then drop) the audit.log.4 slot in a 4-file chain.
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("agent-%03d", i)
		if err := logger.LogDecision(name, "/raw", "/canon", "pass", "", 1); err != nil {
			t.Fatalf("LogDecision %d: %v", i, err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// audit.log must exist (always opened fresh after a rotation).
	if _, err := os.Stat(filepath.Join(dir, "audit.log")); err != nil {
		t.Fatalf("audit.log missing: %v", err)
	}
	// audit.log.1, audit.log.2, audit.log.3 must exist.
	for _, n := range []int{1, 2, 3} {
		p := filepath.Join(dir, fmt.Sprintf("audit.log.%d", n))
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("audit.log.%d missing: %v", n, err)
		}
	}
	// audit.log.4 must NOT exist (would exceed MaxFiles=4).
	tooOld := filepath.Join(dir, "audit.log.4")
	if _, err := os.Stat(tooOld); !os.IsNotExist(err) {
		t.Fatalf("audit.log.4 should NOT exist, got err=%v", err)
	}
}

// TestAuditLogRotationOrderingPreserved writes 30 entries with monotonic
// agent_names, reads them back across all rotated files, and asserts the
// chronological ordering survived the rename chain. Newest entries live
// in audit.log; oldest live in audit.log.{MaxFiles-1}.
func TestAuditLogRotationOrderingPreserved(t *testing.T) {
	dir := t.TempDir()
	logger, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	logger.MaxSize = 200
	logger.MaxFiles = 5

	const N = 30
	for i := 1; i <= N; i++ {
		name := fmt.Sprintf("agent-%03d", i)
		if err := logger.LogDecision(name, "/raw", "/canon", "pass", "", 1); err != nil {
			t.Fatalf("LogDecision %d: %v", i, err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read files in chronological order: oldest first
	// (audit.log.{MaxFiles-1} .. audit.log.1, then audit.log).
	files := []string{}
	for n := logger.MaxFiles - 1; n >= 1; n-- {
		p := filepath.Join(dir, fmt.Sprintf("audit.log.%d", n))
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", p, err)
		}
	}
	files = append(files, filepath.Join(dir, "audit.log"))

	var got []string
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var rec audit.Record
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("parse %s: %v\nline=%s", p, err, line)
			}
			got = append(got, rec.AgentName)
		}
	}

	if len(got) == 0 {
		t.Fatalf("no records read back across rotated files")
	}
	// Each retained agent_name must be strictly increasing — both
	// within each file and across the rename chain. We don't assert
	// `got` covers all 30 names: rotation drops the oldest cohort
	// once the chain is full. We DO assert the most recent entry is
	// the last write.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("ordering broken at %d: %q >= %q (full sequence: %v)", i, got[i-1], got[i], got)
		}
	}
	if want := fmt.Sprintf("agent-%03d", N); got[len(got)-1] != want {
		t.Fatalf("last record: got %q, want %q", got[len(got)-1], want)
	}
}
