package shared

import (
	"carya/internal/chunk"
	"fmt"

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
