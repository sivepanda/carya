package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InitModel represents the Bubble Tea model for the init command
type InitModel struct {
	help    help.Model
	keys    KeyMap
	showAll bool
}

// NewInitModel creates a new init model with configured help and keys
func NewInitModel() InitModel {
	h := help.New()
	h.Styles.ShortDesc = HelpDescStyle
	h.Styles.ShortKey = HelpKeyStyle
	h.Styles.FullDesc = HelpDescStyle
	h.Styles.FullKey = HelpKeyStyle

	return InitModel{
		help: h,
		keys: DefaultKeys(),
	}
}

// Init initializes the model
func (m InitModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model
func (m InitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.showAll = !m.showAll
			return m, nil
		}
	}
	return m, nil
}

// View renders the model
func (m InitModel) View() string {
	title := TitleStyle.Render(CaryaASCII)

	m.help.ShowAll = m.showAll
	helpView := m.help.View(m.keys)
	help := HelpStyle.Render(helpView)

	return lipgloss.JoinVertical(lipgloss.Left, title, help)
}
