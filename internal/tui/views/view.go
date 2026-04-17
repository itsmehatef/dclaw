// Package views contains per-view models for the dclaw TUI. Each view is
// a self-contained struct with a View(...) method that renders into
// width×height cells and a small set of mutating helpers called by the root
// model's Update() loop.
package views

// View identifies the current main-pane content.
type View int

const (
	// ViewList is the default agent-list view.
	ViewList View = iota
	// ViewDetail is the single-agent detail pane.
	ViewDetail
	// ViewDescribe is the one-shot container-inspect pane.
	ViewDescribe
	// ViewNoDaemon is shown when the daemon is not reachable.
	ViewNoDaemon
	// ViewChat is the interactive chat pane for a selected agent.
	// Introduced in alpha.3.
	ViewChat
)
