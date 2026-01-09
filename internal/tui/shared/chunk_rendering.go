package shared

import (
	"carya/internal/chunk"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

// RenderChunkHeader creates a formatted header for a chunk with file path and time range
func RenderChunkHeader(c chunk.Chunk, subtleStyle, boldStyle lipgloss.Style) string {
	fileLabel := subtleStyle.Render("File:")
	filePath := boldStyle.Render(c.FilePath)
	timeLabel := subtleStyle.Render("Time:")
	timeRange := boldStyle.Render(fmt.Sprintf("%s → %s",
		c.StartTime.Format("15:04:05"),
		c.EndTime.Format("15:04:05")))

	return lipgloss.NewStyle().
		Padding(1, 2).
		Render(fileLabel + " " + filePath + "  " + timeLabel + " " + timeRange)
}

// RenderDiffPanel creates a bordered panel with diff content
func RenderDiffPanel(header, viewportContent string, width, height int, borderColor lipgloss.Color) string {
	diffStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	return diffStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, viewportContent))
}

// FormatChunkListItem formats a chunk item for list display with cursor and selection state
func FormatChunkListItem(
	c chunk.Chunk,
	index int,
	cursor int,
	isSelected bool,
	showCheckbox bool,
	maxFilenameLen int,
	subtleStyle, selectedStyle, itemStyle lipgloss.Style,
) string {
	// Format cursor
	cursorStr := "  "
	if cursor == index {
		cursorStr = "❯ "
	}

	// Format checkbox if needed
	checkboxStr := ""
	if showCheckbox {
		if isSelected {
			checkboxStr = " [✓] "
		} else {
			checkboxStr = " [ ] "
		}
	}

	// Format filename
	filename := filepath.Base(c.FilePath)
	if len(filename) > maxFilenameLen {
		filename = filename[:maxFilenameLen-3] + "..."
	}

	// Format time
	timeStr := subtleStyle.Render(c.StartTime.Format("15:04"))

	line := cursorStr + checkboxStr + filename + " " + timeStr

	// Apply styling based on cursor position
	if cursor == index {
		return selectedStyle.Render(line)
	}
	return itemStyle.Render(line)
}

// RenderChunkListPanel creates a bordered panel with a list of chunks
func RenderChunkListPanel(
	title string,
	items []string,
	viewportContent string,
	width, height int,
	borderColor lipgloss.Color,
) string {
	listStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	return listStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, viewportContent))
}
