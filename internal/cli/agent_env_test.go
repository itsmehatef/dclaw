package cli

import (
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
