package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/itsmehatef/dclaw/internal/config"
)

// TestConfigSetGetRoundTrip exercises the CLI surface end-to-end:
// `dclaw config set workspace-root /tmp/x` writes the TOML, then
// `dclaw config get workspace-root` reads it back. Uses DCLAW_STATE_DIR
// via t.Setenv so the test does not touch $HOME/.dclaw.
func TestConfigSetGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", dir)

	// SET
	var setOut bytes.Buffer
	configSetCmd.SetOut(&setOut)
	configSetCmd.SetErr(&setOut)
	configSetCmd.SetArgs([]string{"workspace-root", "/tmp/testroot"})
	if err := configSetCmd.RunE(configSetCmd, []string{"workspace-root", "/tmp/testroot"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(setOut.String(), "/tmp/testroot") {
		t.Fatalf("set output missing value: %q", setOut.String())
	}

	// Verify file on disk via the config package directly.
	cfg, err := config.ReadConfigFile(dir)
	if err != nil {
		t.Fatalf("read after set: %v", err)
	}
	if cfg.WorkspaceRoot != "/tmp/testroot" {
		t.Fatalf("file contents mismatch: %+v", cfg)
	}

	// GET
	var getOut bytes.Buffer
	configGetCmd.SetOut(&getOut)
	configGetCmd.SetErr(&getOut)
	if err := configGetCmd.RunE(configGetCmd, []string{"workspace-root"}); err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(getOut.String(), "/tmp/testroot") {
		t.Fatalf("get output missing value: %q", getOut.String())
	}
}

// TestConfigGetUnconfigured verifies that `dclaw config get workspace-root`
// before any `set` prints "(not configured)" rather than a blank line.
func TestConfigGetUnconfigured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", dir)

	var out bytes.Buffer
	configGetCmd.SetOut(&out)
	configGetCmd.SetErr(&out)
	if err := configGetCmd.RunE(configGetCmd, []string{"workspace-root"}); err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out.String(), "not configured") {
		t.Fatalf("expected '(not configured)' output, got %q", out.String())
	}
}

// TestConfigSetEmptyValueRejected asserts the empty-value guard.
func TestConfigSetEmptyValueRejected(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", dir)

	err := configSetCmd.RunE(configSetCmd, []string{"workspace-root", ""})
	if err == nil {
		t.Fatalf("expected error on empty value")
	}
}

// TestConfigUnknownKey asserts unrecognized keys are rejected with a
// helpful message.
func TestConfigUnknownKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DCLAW_STATE_DIR", dir)

	err := configSetCmd.RunE(configSetCmd, []string{"unknown-key", "value"})
	if err == nil {
		t.Fatalf("expected error on unknown key")
	}
	if !strings.Contains(err.Error(), "workspace-root") {
		t.Fatalf("expected message to list supported keys, got %v", err)
	}
}
