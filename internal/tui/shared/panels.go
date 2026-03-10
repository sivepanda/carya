package shared

import (
	"carya/internal/tui"

	"github.com/charmbracelet/lipgloss"
)

// RenderTitledPanel renders a consistent titled panel for split-view TUI layouts.
func RenderTitledPanel(title, body string, width, height int, borderColor lipgloss.Color) string {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Render(tui.HeaderStyle.Render(title) + "\n" + body)
}
