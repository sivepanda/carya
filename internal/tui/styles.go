package tui

import "github.com/charmbracelet/lipgloss"

// Spacing constants (inspired by Crush)
const (
	SpacingUnit    = 1
	DefaultMargin  = 2
	DefaultPadding = 1
	ListIndent     = 2
	ComponentGap   = 1
	SectionGap     = 2
)

// Color palette - modern terminal aesthetics with extended range
var (
	// Primary brand colors
	ColorTitle     = lipgloss.Color("#7DCFFF") // Bright cyan
	ColorTitleAlt  = lipgloss.Color("#2AC3DE") // Darker cyan for gradients
	ColorAccent    = lipgloss.Color("#BB9AF7") // Purple
	ColorAccentAlt = lipgloss.Color("#9D7CD8") // Darker purple

	// Semantic colors
	ColorSuccess    = lipgloss.Color("#9ECE6A") // Green
	ColorSuccessAlt = lipgloss.Color("#73DACA") // Teal green
	ColorWarning    = lipgloss.Color("#E0AF68") // Orange
	ColorWarningAlt = lipgloss.Color("#FF9E64") // Bright orange
	ColorError      = lipgloss.Color("#F7768E") // Red
	ColorErrorAlt   = lipgloss.Color("#DB4B4B") // Darker red
	ColorInfo       = lipgloss.Color("#7AA2F7") // Blue

	// Text hierarchy
	ColorPrimary   = lipgloss.Color("#C0CAF5") // Light blue-white
	ColorSecondary = lipgloss.Color("#565F89") // Muted blue-gray
	ColorTertiary  = lipgloss.Color("#414868") // Dark blue-gray
	ColorSubtle    = lipgloss.Color("#3B4261") // Very dark blue-gray
	ColorMuted     = lipgloss.Color("#545c7e") // Muted gray

	// UI elements
	ColorHighlight    = lipgloss.Color("#FF9E64") // Bright orange
	ColorSelected     = lipgloss.Color("#ff9e64") // Selection highlight
	ColorBorder       = lipgloss.Color("#7AA2F7") // Medium blue
	ColorBorderDim    = lipgloss.Color("#3D59A1") // Darker blue
	ColorBorderAccent = lipgloss.Color("#BB9AF7") // Purple border

	// Background shades
	ColorBase        = lipgloss.Color("#1a1b26") // Base dark
	ColorBaseLighter = lipgloss.Color("#24283b") // Slightly lighter
	ColorOverlay     = lipgloss.Color("#292e42") // Overlay shade
)

// Icon set (inspired by Crush)
const (
	IconCheck    = "✓"
	IconCross    = "×"
	IconWarning  = "⚠"
	IconInfo     = "ⓘ"
	IconHint     = "∵"
	IconSpinner  = "◐"
	IconLoading  = "⟳"
	IconDocument = "📄"
	IconFolder   = "📁"
	IconSettings = "⚙"
	IconSuccess  = "✓"
	IconError    = "×"
	IconPending  = "●"
	IconArrow    = "→"
	IconCursor   = "❯"
	IconBullet   = "•"
	IconCheckbox = "☐"
	IconChecked  = "☑"
)

// Common styles with improved hierarchy
var (
	// Titles and headers
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorTitle).
			Bold(true).
			Padding(0, DefaultPadding).
			Margin(DefaultMargin, 0)

	HeaderStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			Padding(0, DefaultPadding)

	SubheaderStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	// Text styles with hierarchy
	TextStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary)

	SubtleTextStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	MutedTextStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	DimTextStyle = lipgloss.NewStyle().
			Foreground(ColorTertiary)

	// Help and hints
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Padding(DefaultPadding, 0)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorTertiary)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	HintStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	// List items
	ItemStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			PaddingLeft(ListIndent)

	SelectedItemStyle = lipgloss.NewStyle().
			Foreground(ColorHighlight).
			Bold(true).
			PaddingLeft(ListIndent)

	ItemDescStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			PaddingLeft(ListIndent * 2)

	// Status styles
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	WarningStyle = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	InfoStyle = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Bold(true)

	// Boxes and containers
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(DefaultPadding, DefaultPadding*2)

	ActiveBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(ColorTitle).
			Padding(DefaultPadding, DefaultPadding*2)

	DimBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorBorderDim).
			Padding(DefaultPadding, DefaultPadding*2)

	AccentBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorderAccent).
			Padding(DefaultPadding, DefaultPadding*2)

	// Metadata and labels
	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Bold(false)

	ValueStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	// Separator
	SeparatorStyle = lipgloss.NewStyle().
			Foreground(ColorBorderDim)
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
