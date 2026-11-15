package shared

import (
	"carya/internal/tui"
	"image/color"

	"charm.land/lipgloss/v2"
)

var titledPanelStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 1)

// RenderTitledPanel renders a consistent titled panel for split-view TUI layouts.
func RenderTitledPanel(title, body string, width, height int, borderColor color.Color) string {
	frameWidth, frameHeight := titledPanelStyle.GetFrameSize()
	innerWidth := width - frameWidth
	innerHeight := height - frameHeight
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	return titledPanelStyle.
		Width(innerWidth).
		Height(innerHeight).
		BorderForeground(borderColor).
		Render(tui.HeaderStyle.Render(title) + "\n" + body)
}

// TitledPanelViewportSize returns the viewport size that fits inside RenderTitledPanel.
// bodyHeaderLines is the number of non-viewport lines prepended in panel body content.
func TitledPanelViewportSize(panelWidth, panelHeight, bodyHeaderLines int) (int, int) {
	frameWidth, frameHeight := titledPanelStyle.GetFrameSize()
	viewportWidth := panelWidth - frameWidth
	viewportHeight := panelHeight - frameHeight - 1 - bodyHeaderLines

	if viewportWidth < 1 {
		viewportWidth = 1
	}
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	return viewportWidth, viewportHeight
}
