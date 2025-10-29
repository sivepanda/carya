package tui

import "github.com/charmbracelet/lipgloss"

// Color palette - modern terminal aesthetics
var (
	ColorTitle       = lipgloss.Color("#7DCFFF") // Bright cyan
	ColorAccent      = lipgloss.Color("#BB9AF7") // Purple
	ColorSuccess     = lipgloss.Color("#9ECE6A") // Green
	ColorWarning     = lipgloss.Color("#E0AF68") // Orange
	ColorError       = lipgloss.Color("#F7768E") // Red
	ColorPrimary     = lipgloss.Color("#C0CAF5") // Light blue-white
	ColorSecondary   = lipgloss.Color("#565F89") // Muted blue-gray
	ColorTertiary    = lipgloss.Color("#414868") // Dark blue-gray
	ColorHighlight   = lipgloss.Color("#FF9E64") // Bright orange
	ColorDim         = lipgloss.Color("#3B4261") // Very dark blue-gray
	ColorBorder      = lipgloss.Color("#7AA2F7") // Medium blue
	ColorBorderDim   = lipgloss.Color("#3D59A1") // Darker blue
)

// Common styles
var (
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorTitle).
			Bold(true).
			Padding(0, 1).
			Margin(1, 0)

	HeaderStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			Padding(0, 1)

	TextStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary)

	SubtleTextStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Padding(1, 0)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorTertiary)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	ItemStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Padding(0, 2)

	SelectedItemStyle = lipgloss.NewStyle().
			Foreground(ColorHighlight).
			Bold(true).
			Padding(0, 2)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	ActiveBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(ColorTitle).
			Padding(1, 2)

	DimBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorBorderDim).
			Padding(1, 2)
)

// ASCII art for Carya
const CaryaASCII = `
 __    __     _                            _            ___                         _ 
/ / /\ \ \___| | ___ ___  _ __ ___   ___  | |_ ___     / __\__ _ _ __ _   _  __ _  / \
\ \/  \/ / _ \ |/ __/ _ \| '_ ' _ \ / _ \ | __/ _ \   / /  / _' | '__| | | |/ _' |/  /
 \  /\  /  __/ | (_| (_) | | | | | |  __/ | || (_) | / /__| (_| | |  | |_| | (_| /\_/ 
  \/  \/ \___|_|\___\___/|_| |_| |_|\___|  \__\___/  \____/\__,_|_|   \__, |\__,_\/   
                                                                      |___/           
`
