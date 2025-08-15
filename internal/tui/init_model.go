package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Screen states
const (
	StateWelcome = iota
	StateForm
	StateExecute
	StateComplete
)

// InitModel represents the Bubble Tea model for the init command
type InitModel struct {
	help           help.Model
	keys           KeyMap
	state          int
	form           *huh.Form
	showAll        bool
	selectedFields []string
	width          int
	height         int
}

// NewInitModel creates a new init model with configured help and keys
func NewInitModel() InitModel {
	h := help.New()
	h.Styles.ShortDesc = HelpDescStyle
	h.Styles.ShortKey = HelpKeyStyle
	h.Styles.FullDesc = HelpDescStyle
	h.Styles.FullKey = HelpKeyStyle

	m := InitModel{
		help:  h,
		keys:  DefaultKeys(),
		state: StateWelcome,
		width: 80,
	}

	return m
}

// Init initializes the model
func (m *InitModel) Init() tea.Cmd {
	return nil
}

// createForm builds the huh form with all fields
func (m *InitModel) createForm() {
	confirm := true // Default to Yes

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select Carya features you would like to enable").
				Options(
					huh.NewOption("Feature-Based Commits", "featcom"),
					huh.NewOption("Automated Housekeeping", "housekeep"),
				).
				Value(&m.selectedFields),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Proceed with setup?").
				Affirmative("Yes").
				Negative("No").
				Value(&confirm),
		),
	).WithWidth(m.width - 4).WithShowHelp(true)
}

// handleFormSubmission processes the form data and executes the setup
func (m *InitModel) handleFormSubmission() tea.Cmd {
	// Clear the form when we transition to execute state
	m.form = nil

	// TODO: Replace this placeholder with actual implementation logic
	// This function will be called when the form is submitted
	// It should process the selected features and perform the actual setup

	// For now, we'll just return a command that simulates some work
	return func() tea.Msg {
		// Simulate some processing time
		// In a real implementation, this would:
		// - Initialize selected features
		// - Configure the repository
		// - Set up any necessary files
		// - Return success/error status
		return FormSubmittedMsg{}
	}
}

// FormSubmittedMsg indicates that form processing is complete
type FormSubmittedMsg struct{}

// Update handles messages and updates the model
func (m *InitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case FormSubmittedMsg:
		m.state = StateComplete
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.form != nil {
			m.form = m.form.WithWidth(m.width - 4)
		}
		return m, nil

	case tea.KeyMsg:
		// Handle form state separately - let form handle ALL keys when active
		if m.state == StateForm && m.form != nil {
			// Let the form handle the key message first
			newForm, cmd := m.form.Update(msg)
			if f, ok := newForm.(*huh.Form); ok {
				m.form = f

				// Check if form is completed using the proper FormState
				if m.form.State == huh.StateCompleted {
					m.state = StateExecute
					return m, m.handleFormSubmission()
				}
			}

			// Only handle quit when form is active
			if key.Matches(msg, m.keys.Quit) {
				return m, tea.Quit
			}

			return m, cmd
		}

		// Global key handlers for non-form states
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.showAll = !m.showAll
			return m, nil

		case key.Matches(msg, m.keys.Enter):
			switch m.state {
			case StateWelcome:
				m.state = StateForm
				m.createForm()
				// Initialize the form after creation
				if m.form != nil {
					return m, m.form.Init()
				}
				return m, nil

			case StateExecute:
				// Allow skipping execution with Enter (for debugging/testing)
				m.state = StateComplete
				return m, nil

			case StateComplete:
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

// View renders the model
func (m *InitModel) View() string {
	var content string

	switch m.state {
	case StateWelcome:
		title := TitleStyle.Render(CaryaASCII)
		welcomeText := TextStyle.Render("Hit 'Enter' to begin the setup process!")
		content = lipgloss.JoinVertical(lipgloss.Center, title, welcomeText)

	case StateForm:
		if m.form != nil {
			content = m.form.View()
		}

	case StateExecute:
		title := TitleStyle.Render("Processing Your Selection")

		// Build a detailed summary of what was selected and what's happening
		var summary string
		if len(m.selectedFields) == 0 {
			summary = "You didn't select any features.\nInitializing basic Carya configuration..."
		} else {
			summary = "You selected the following features:\n\n"
			for _, feature := range m.selectedFields {
				switch feature {
				case "featcom":
					summary += "✓ Feature-Based Commits\n"
				case "housekeep":
					summary += "✓ Automated Housekeeping\n"
				}
			}
			summary += "\nSetting up your repository..."
		}

		executionText := TextStyle.Render(summary)
		helpText := HelpDescStyle.Render("\nPress Enter to complete setup")
		content = lipgloss.JoinVertical(lipgloss.Center, title, executionText, helpText)

	case StateComplete:
		// Build a summary of selected features
		var summary string
		if len(m.selectedFields) == 0 {
			summary = "No features selected"
		} else {
			summary = "Selected features:\n"
			for _, feature := range m.selectedFields {
				switch feature {
				case "featcom":
					summary += "• Feature-Based Commits\n"
				case "housekeep":
					summary += "• Automated Housekeeping\n"
				}
			}
		}

		title := TitleStyle.Render("Setup Complete!")
		completionText := TextStyle.Render(summary + "\nPress Enter to exit.")
		content = lipgloss.JoinVertical(lipgloss.Center, title, completionText)
	}

	// Add help view at the bottom
	m.help.ShowAll = m.showAll
	helpView := m.help.View(m.keys)
	help := HelpStyle.Render(helpView)

	return lipgloss.JoinVertical(lipgloss.Left, content, help)
}
