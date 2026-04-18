package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeShellEnvInheritsWellKnown(t *testing.T) {
	// Set up a well-known env var.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	result := mergeShellEnv(nil)
	found := false
	for _, kv := range result {
		if kv == "ANTHROPIC_API_KEY=sk-ant-test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ANTHROPIC_API_KEY to be inherited, got %v", result)
	}
}

func TestMergeShellEnvExplicitWins(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "should-not-appear")
	explicit := []string{"ANTHROPIC_API_KEY=explicit-value"}
	result := mergeShellEnv(explicit)
	count := 0
	for _, kv := range result {
		if len(kv) >= 17 && kv[:17] == "ANTHROPIC_API_KEY" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 ANTHROPIC_API_KEY entry, got %d in %v", count, result)
	}
	if result[0] != "ANTHROPIC_API_KEY=explicit-value" {
		t.Fatalf("expected explicit value to win, got %v", result)
	}
}

func TestMergeShellEnvNoInheritWhenUnset(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	result := mergeShellEnv(nil)
	for _, kv := range result {
		if len(kv) >= 17 && kv[:17] == "ANTHROPIC_API_KEY" {
			t.Fatalf("expected no ANTHROPIC_API_KEY when unset, got %v in %v", kv, result)
		}
	}
}

// writeTempFile writes content to a file in t.TempDir and returns the path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// TestParseDotenvBasic verifies that parseDotenv correctly parses a file with
// KEY=VAL lines, skips '# comment' lines and blank lines, and handles values
// containing '='.
func TestParseDotenvBasic(t *testing.T) {
	content := `# leading comment
KEY1=val1

KEY2=val2
# another comment
KEY3=prefix=with=equals
   # indented comment is still a comment after trim
KEY4=
`
	path := writeTempFile(t, ".env", content)
	got, err := parseDotenv(path)
	if err != nil {
		t.Fatalf("parseDotenv: %v", err)
	}
	want := []string{
		"KEY1=val1",
		"KEY2=val2",
		"KEY3=prefix=with=equals",
		"KEY4=",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestParseDotenvMissingFile verifies that a missing file returns a helpful error.
func TestParseDotenvMissingFile(t *testing.T) {
	_, err := parseDotenv("/nonexistent/path/to/.env.does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	msg := err.Error()
	if !contains(msg, "--env-file") {
		t.Errorf("expected error to mention --env-file, got %q", msg)
	}
}

// TestParseDotenvRejectsMissingEquals verifies a line without '=' is an error.
func TestParseDotenvRejectsMissingEquals(t *testing.T) {
	path := writeTempFile(t, ".env", "KEY1=val1\nBROKEN_LINE_NO_EQUALS\n")
	_, err := parseDotenv(path)
	if err == nil {
		t.Fatal("expected error for line missing '=', got nil")
	}
}

// TestParseDotenvEmptyPath verifies an empty path returns empty slice, nil error.
func TestParseDotenvEmptyPath(t *testing.T) {
	got, err := parseDotenv("")
	if err != nil {
		t.Fatalf("parseDotenv(\"\"): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

// TestEnvFilePrecedence verifies that an explicit --env value wins over a
// --env-file value for the same key, and a file-only key is preserved.
func TestEnvFilePrecedence(t *testing.T) {
	// Isolate well-known shell keys so this test only exercises file + explicit.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "")

	path := writeTempFile(t, ".env", "FOO=file\nBAR=from-file\n")
	explicit := []string{"FOO=explicit"}

	result, err := composeEnv(path, explicit)
	if err != nil {
		t.Fatalf("composeEnv: %v", err)
	}
	if result["FOO"] != "explicit" {
		t.Errorf("--env should win over --env-file: got FOO=%q, want %q", result["FOO"], "explicit")
	}
	if result["BAR"] != "from-file" {
		t.Errorf("file-only key should be preserved: got BAR=%q, want %q", result["BAR"], "from-file")
	}
}

// TestEnvFileLowerThanShellInheritedOrNot verifies that file values take
// precedence over the well-known shell inheritance (file wins because it's
// processed AFTER shell in composeEnv's stack).
func TestEnvFileBeatsShell(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "shell-value")
	path := writeTempFile(t, ".env", "ANTHROPIC_API_KEY=file-value\n")

	result, err := composeEnv(path, nil)
	if err != nil {
		t.Fatalf("composeEnv: %v", err)
	}
	if result["ANTHROPIC_API_KEY"] != "file-value" {
		t.Errorf("--env-file should beat shell: got %q, want %q", result["ANTHROPIC_API_KEY"], "file-value")
	}
}

// contains is a tiny helper so the file doesn't depend on strings.Contains
// being imported (it already is elsewhere, but keep the test file clean).
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
