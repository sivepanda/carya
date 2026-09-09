package shared

import (
	"carya/internal/tui"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// BuildFilteredIndices returns stable indices matching a case-insensitive query.
func BuildFilteredIndices(candidates []string, query string) []int {
	q := strings.TrimSpace(strings.ToLower(query))
	indices := make([]int, 0, len(candidates))
	for i, c := range candidates {
		if q == "" || strings.Contains(strings.ToLower(c), q) {
			indices = append(indices, i)
		}
	}
	return indices
}

// RenderSearchBar renders a prominent search/filter bar for list-style TUIs.
func RenderSearchBar(searching bool, searchView, query string, shown, total, width int) string {
	barStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorBorderAccent).
		Background(tui.ColorSubtle).
		Padding(0, 1)

	frameWidth, _ := barStyle.GetFrameSize()
	minWidth := frameWidth + 1
	if width < minWidth {
		width = minWidth
	}
	innerWidth := width - frameWidth

	left := tui.HelpKeyStyle.Render("/") + tui.HelpDescStyle.Render(" filter files")
	if searching {
		left = tui.WarningStyle.Render("FILTER MODE") + tui.SubtleTextStyle.Render("  ") + searchView
	} else if strings.TrimSpace(query) != "" {
		left = tui.InfoStyle.Render("QUERY") + tui.SubtleTextStyle.Render(": ") + tui.TextStyle.Bold(true).Render(query)
	} else {
		left = tui.SubtleTextStyle.Render("QUERY: (none)  press / to filter")
	}

	right := tui.SubtleTextStyle.Render(fmt.Sprintf("showing %d/%d", shown, total))

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)

	if rightWidth >= innerWidth {
		right = truncateStyled(right, innerWidth)
		left = ""
		leftWidth = 0
		rightWidth = lipgloss.Width(right)
	}

	space := innerWidth - leftWidth - rightWidth
	if space < 1 {
		maxLeft := innerWidth - rightWidth - 1
		if maxLeft < 0 {
			maxLeft = 0
		}
		left = truncateStyled(left, maxLeft)
		leftWidth = lipgloss.Width(left)
		space = innerWidth - leftWidth - rightWidth
		if space < 1 {
			space = 1
		}
	}

	content := left + strings.Repeat(" ", space) + right

	bar := barStyle.
		Width(innerWidth).
		Render(content)

	return bar
}

func truncateStyled(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(value)
}
