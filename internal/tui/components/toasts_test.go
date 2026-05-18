package components

import (
	"strings"
	"testing"
	"time"
)

func TestStackPushTrimsOldest(t *testing.T) {
	var s Stack
	now := time.Unix(100, 0)
	s.Push("info", "one", now)
	s.Push("info", "two", now)
	s.Push("info", "three", now)
	s.Push("info", "four", now)
	if s.Len() != ToastMaxStack {
		t.Fatalf("len=%d want %d", s.Len(), ToastMaxStack)
	}
	rendered := s.Render(80, 20)
	if strings.Contains(rendered, "one") {
		t.Fatalf("oldest toast was not trimmed: %q", rendered)
	}
	for _, want := range []string{"two", "three", "four"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("render missing %q: %q", want, rendered)
		}
	}
}

func TestStackTickExpiresEntries(t *testing.T) {
	var s Stack
	now := time.Unix(100, 0)
	s.Push("info", "hello", now)
	s.Tick(now.Add(ToastDuration + time.Nanosecond))
	if s.Len() != 0 {
		t.Fatalf("len=%d want 0", s.Len())
	}
}

func TestStackDismissTop(t *testing.T) {
	var s Stack
	now := time.Unix(100, 0)
	s.Push("info", "old", now)
	s.Push("info", "new", now)
	s.DismissTop()
	if s.Len() != 1 {
		t.Fatalf("len=%d want 1", s.Len())
	}
	rendered := s.Render(80, 20)
	if strings.Contains(rendered, "new") || !strings.Contains(rendered, "old") {
		t.Fatalf("dismiss removed wrong toast: %q", rendered)
	}
}

func TestStackRenderEmpty(t *testing.T) {
	var s Stack
	if got := s.Render(80, 20); got != "" {
		t.Fatalf("empty render=%q want empty", got)
	}
}

func TestStackRenderBounded(t *testing.T) {
	var s Stack
	s.Push("error", strings.Repeat("x", 200), time.Unix(100, 0))
	got := s.Render(30, 10)
	if got == "" {
		t.Fatalf("expected rendered toast")
	}
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 80 {
			t.Fatalf("render line unexpectedly wide: %q", line)
		}
	}
}
