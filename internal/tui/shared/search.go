package shared

import (
	"carya/internal/tui"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	left := tui.HelpKeyStyle.Render("/") + tui.HelpDescStyle.Render(" filter files")
	if searching {
		left = tui.WarningStyle.Render("FILTER MODE") + tui.SubtleTextStyle.Render("  ") + searchView
	} else if strings.TrimSpace(query) != "" {
		left = tui.InfoStyle.Render("QUERY") + tui.SubtleTextStyle.Render(": ") + tui.TextStyle.Bold(true).Render(query)
	} else {
		left = tui.SubtleTextStyle.Render("QUERY: (none)  press / to filter")
	}

	right := tui.SubtleTextStyle.Render(fmt.Sprintf("showing %d/%d", shown, total))
	content := left + tui.SubtleTextStyle.Render("   ") + right
	if width > 4 {
		innerWidth := width - 4
		space := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
		if space < 1 {
			space = 1
		}
		content = left + strings.Repeat(" ", space) + right
	}

	bar := lipgloss.NewStyle().
		Width(max(width, lipgloss.Width(content)+4)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorBorderAccent).
		Background(tui.ColorSubtle).
		Padding(0, 1).
		Render(content)

	return bar
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
