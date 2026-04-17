package main

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/itsmehatef/dclaw/internal/cli"
	"github.com/itsmehatef/dclaw/internal/tui"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "dclaw: panic: %v\n", r)
			os.Exit(1)
		}
	}()

	// Bare invocation on an interactive terminal = launch TUI.
	// Any args, any flags, non-TTY stdin/stdout = cobra.
	if shouldLaunchTUI(os.Args) {
		// Resolve --no-mouse before handing off. We do a manual scan of
		// os.Args here rather than parsing through cobra so the TUI launch
		// path is independent of cobra flag registration.
		for _, a := range os.Args[1:] {
			if a == "--no-mouse" {
				tui.NoMouse = true
			}
		}
		// Resolve the daemon socket path from the env/default (same logic as
		// internal/client.DefaultSocketPath, duplicated to avoid a circular
		// import between cmd/dclaw and internal/tui).
		socket := resolveSocket()
		if err := tui.Run(socket); err != nil {
			fmt.Fprintf(os.Stderr, "dclaw tui: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

// shouldLaunchTUI returns true only for the literal bare invocation
// (len(argv)==1) on an interactive terminal pair (stdin and stdout both TTY).
// Any argument, flag, or non-TTY context falls through to cobra.
func shouldLaunchTUI(argv []string) bool {
	if len(argv) != 1 {
		return false
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
		return false
	}
	return true
}

// resolveSocket mirrors daemon.DefaultSocketPath without importing internal/daemon,
// which would create an oversized dependency in main. The canonical copy lives in
// internal/client.DefaultSocketPath(); keep both in sync.
func resolveSocket() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return home + "/.dclaw/dclaw.sock"
}
