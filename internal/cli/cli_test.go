package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpDoesNotError(t *testing.T) {
	cases := []string{
		"--help",
		"version --help",
		"agent --help",
		"agent create --help",
		"agent list --help",
		"channel --help",
		"daemon --help",
		"status --help",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			var out, errb bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errb)
			rootCmd.SetArgs(strings.Fields(c))
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("unexpected error for %q: %v (stderr=%q)", c, err, errb.String())
			}
		})
	}
}

func TestInvalidOutputFormat(t *testing.T) {
	outputFormat = "bogus"
	t.Cleanup(func() { outputFormat = "table" })
	if err := validateOutputFormat(); err == nil {
		t.Fatal("expected error for bogus -o")
	}
}
