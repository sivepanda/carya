package tui

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	ColorPrimary   = lipgloss.Color("#9CFFB9")
	ColorSecondary = lipgloss.Color("#888888")
)

// Common styles
var (
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Padding(1, 2).
			Margin(1, 0)

	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)
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
