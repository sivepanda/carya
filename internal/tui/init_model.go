package tui

import (
	// "carya/internal/features"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Screen Definitions
const (
	StateWelcome = iota
	StateSelection
	StateExecute
	StateComplete
)

// InitModel represents the Bubble Tea model for the init command
type InitModel struct {
	help           help.Model
	keys           KeyMap
	state          int
	form           *huh.MultiSelect[string]
	showAll        bool
	selectedFields []string
}

func (m *InitModel) initForm() {
	var featureSelect []string
	m.form = huh.NewMultiSelect[string]().
		Options(
			huh.NewOption("Feature-Based Commits", "featcom"),
			huh.NewOption("Automated Housekeeping", "housekeep"),
		).
		Title("Select Carya features you would like to enable.").
		Limit(2).
		Value(&featureSelect)
	// .WithShowHelp(false)
}

func (m InitModel) activeView() string {
	switch m.state {
	case StateWelcome:
		title := TitleStyle.Render(CaryaASCII)
		welcomeText := TextStyle.Render("Hit 'Enter' to begin the setup process!")
		welcomeView := lipgloss.JoinVertical(lipgloss.Center, title, welcomeText)
		return welcomeView
	case StateSelection:
		return m.form.View()
	case StateExecute:
		return TextStyle.Render("State Execute")
	case StateComplete:
		// return m.completedView()
		return TextStyle.Render("State Complete")
	}
	// Panic if the active view somehow goes past the number of cases we have (it shouldn't but it's currently a little "gentleman's agreement")
	panic("Active View has exceeded its upper bounds")
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
		// Handle enter (advance)
		case key.Matches(msg, m.keys.Enter):
			switch m.state {
			case StateWelcome:
				m.state = StateSelection
				m.initForm()
				return m, m.form.Init()
			case StateSelection:
				if m.form.State == huh.StateCompleted {
					m.state = StateExecute
				}
			case StateExecute:
				m.state = StateComplete
			case StateComplete:
				return m, tea.Quit
			}

		// Handle quit
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		// Handle help
		case key.Matches(msg, m.keys.Help):
			m.showAll = !m.showAll
			return m, nil
		}
	}
	if m.state == 1 && m.form != nil {
		frm, cmd := m.form.Update(msg)
		if f, ok := frm.(*huh.Form); ok {
			m.form = f
		}
		return m, cmd
	}
	return m, nil
}

// View renders the model
func (m InitModel) View() string {
	activeView := m.activeView()

	m.help.ShowAll = m.showAll
	helpView := m.help.View(m.keys)
	help := HelpStyle.Render(helpView)

	return lipgloss.JoinVertical(lipgloss.Left, activeView, help)
}
