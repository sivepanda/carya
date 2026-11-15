package shared

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"image/color"
)

// NewDefaultSpinner creates a spinner with default Carya styling
func NewDefaultSpinner(color color.Color) spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(color)
	return s
}
