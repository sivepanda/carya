package tui

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	ColorTitle     = lipgloss.Color("#9CFFB9")
	ColorPrimary   = lipgloss.Color("#DDDDDD")
	ColorSecondary = lipgloss.Color("#A6A6A6")
	ColorHelp      = lipgloss.Color("#808080")
)

// Common styles
var (
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorTitle).
			Padding(1, 2).
			Margin(1, 0)

	TextStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary)

	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorHelp)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	ItemStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary)

	SelectedItemStyle = lipgloss.NewStyle().
			Foreground(ColorTitle).
			Bold(true)
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
