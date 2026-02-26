package shared

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

// NewDefaultSpinner creates a spinner with default Carya styling
func NewDefaultSpinner(color lipgloss.Color) spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(color)
	return s
}
