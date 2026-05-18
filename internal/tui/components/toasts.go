package components

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	ToastDuration = 3 * time.Second
	ToastMaxStack = 3
)

// Toast is one transient TUI notification.
type Toast struct {
	ID      int
	Level   string
	Message string
	Expiry  time.Time
}

// Stack holds the currently visible notifications.
type Stack struct {
	items  []Toast
	nextID int
}

// Push appends a toast and trims the stack to ToastMaxStack.
func (s *Stack) Push(level, message string, now time.Time) Toast {
	s.nextID++
	t := Toast{
		ID:      s.nextID,
		Level:   level,
		Message: message,
		Expiry:  now.Add(ToastDuration),
	}
	s.items = append(s.items, t)
	if len(s.items) > ToastMaxStack {
		s.items = append([]Toast(nil), s.items[len(s.items)-ToastMaxStack:]...)
	}
	return t
}

// Tick removes expired toasts.
func (s *Stack) Tick(now time.Time) {
	out := s.items[:0]
	for _, item := range s.items {
		if now.Before(item.Expiry) {
			out = append(out, item)
		}
	}
	s.items = out
}

// DismissTop removes the newest toast.
func (s *Stack) DismissTop() {
	if len(s.items) == 0 {
		return
	}
	s.items = s.items[:len(s.items)-1]
}

// Len returns the visible toast count.
func (s *Stack) Len() int { return len(s.items) }

// Render returns a compact toast stack. The root model is responsible for
// positioning this stack over the main content.
func (s *Stack) Render(width, height int) string {
	if len(s.items) == 0 || width <= 0 || height <= 0 {
		return ""
	}
	boxes := make([]string, 0, len(s.items))
	maxWidth := width - 4
	if maxWidth > 48 {
		maxWidth = 48
	}
	if maxWidth < 12 {
		maxWidth = width
	}
	for _, item := range s.items {
		style := toastStyle(item.Level).MaxWidth(maxWidth)
		boxes = append(boxes, style.Render(item.Message))
	}
	return lipgloss.JoinVertical(lipgloss.Right, boxes...)
}

func toastStyle(level string) lipgloss.Style {
	color := lipgloss.Color("39")
	switch level {
	case "warning":
		color = lipgloss.Color("214")
	case "error":
		color = lipgloss.Color("196")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(color).
		Foreground(color).
		Padding(0, 1)
}
