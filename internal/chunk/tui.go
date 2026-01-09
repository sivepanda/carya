package chunk

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	addedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff87")).Bold(false)
	removedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f87")).Bold(false)
	contextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc"))
	headerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#87d7ff")).Bold(true)
	rangeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf5f")).Bold(true)
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	binaryStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf5f")).Bold(true)
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
)

func FormatDiff(diff string) string {
	if strings.HasPrefix(diff, "Binary file ") {
		lines := strings.Split(diff, "\n")
		var formatted []string
		for i, line := range lines {
			if i == 0 {
				formatted = append(formatted, binaryStyle.Render("⚠ "+line))
			} else if strings.TrimSpace(line) != "" {
				formatted = append(formatted, infoStyle.Render("  "+line))
			}
		}
		return strings.Join(formatted, "\n")
	}

	if strings.TrimSpace(diff) == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Render("No changes detected")
	}

	lines := strings.Split(diff, "\n")
	var formatted []string

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			formatted = append(formatted, headerStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			formatted = append(formatted, addedStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			formatted = append(formatted, removedStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			formatted = append(formatted, rangeStyle.Render(line))
		case strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index"):
			formatted = append(formatted, subtleStyle.Render(line))
		case strings.HasPrefix(line, "File:") || strings.HasPrefix(line, "Time:") || strings.HasPrefix(line, "Hash:"):
			formatted = append(formatted, contextStyle.Render(line))
		default:
			formatted = append(formatted, contextStyle.Render(line))
		}
	}

	return strings.Join(formatted, "\n")
}
