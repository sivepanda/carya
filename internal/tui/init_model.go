package tui

import (
	"fmt"
	"strings"

	initializer "carya/internal/init"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Screen states
const (
	StateWelcome = iota
	StateFeatureSelect
	StateConfirm
	StateExecute
	StateComplete
)

// Feature options
type Feature struct {
	Name        string
	Key         string
	Description string
}

var availableFeatures = []Feature{
	{"Feature-Based Commits", "featcom", "Enable feature-based commit workflows"},
	{"Automated Housekeeping", "housekeep", "Automated repository maintenance"},
}

// InitModel represents the Bubble Tea model for the init command
type InitModel struct {
	help               help.Model
	keys               KeyMap
	state              int
	cursor             int
	selectedFeatures   map[string]bool
	showAll            bool
	width              int
	height             int
	confirmSelection   bool
	err                error
	launchHousekeeping bool
}

// NewInitModel creates a new init model
func NewInitModel() InitModel {
	h := help.New()
	h.Styles.ShortDesc = HelpDescStyle
	h.Styles.ShortKey = HelpKeyStyle
	h.Styles.FullDesc = HelpDescStyle
	h.Styles.FullKey = HelpKeyStyle

	m := InitModel{
		help:             h,
		keys:             DefaultKeys(),
		state:            StateWelcome,
		width:            80,
		selectedFeatures: make(map[string]bool),
		confirmSelection: true, // Default to Yes
	}

	return m
}

// Init initializes the model
func (m *InitModel) Init() tea.Cmd {
	return nil
}

// handleFormSubmission processes the form data and executes the setup
func (m *InitModel) handleFormSubmission() tea.Cmd {
	return func() tea.Msg {
		// Get the selected features from the model
		selectedFeatures := m.getSelectedFeatures()

		// Special case: If ONLY housekeeping is selected, launch the housekeeping TUI
		if len(selectedFeatures) == 1 && selectedFeatures[0] == "housekeep" {
			// Create .carya directory first
			init, err := initializer.NewInitializer([]string{})
			if err != nil {
				return FormSubmittedMsg{Error: err, LaunchHousekeeping: false}
			}

			if err := init.Initialize(); err != nil {
				return FormSubmittedMsg{Error: err, LaunchHousekeeping: false}
			}

			return FormSubmittedMsg{Error: nil, LaunchHousekeeping: true}
		}

		// Create initializer with selected features
		init, err := initializer.NewInitializer(selectedFeatures)
		if err != nil {
			return FormSubmittedMsg{Error: err, LaunchHousekeeping: false}
		}

		// Initialize the repository
		if err := init.Initialize(); err != nil {
			return FormSubmittedMsg{Error: err, LaunchHousekeeping: false}
		}

		return FormSubmittedMsg{Error: nil, LaunchHousekeeping: false}
	}
}

// FormSubmittedMsg indicates that form processing is complete
type FormSubmittedMsg struct {
	Error              error
	LaunchHousekeeping bool
}

// ShouldLaunchHousekeeping returns true if the housekeeping TUI should be launched
func (m *InitModel) ShouldLaunchHousekeeping() bool {
	return m.launchHousekeeping
}

// IsFeatureEnabled returns true if a feature is enabled
func (m *InitModel) IsFeatureEnabled(featureKey string) bool {
	return m.selectedFeatures[featureKey]
}

// Update handles messages and updates the model
func (m *InitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case FormSubmittedMsg:
		if msg.Error != nil {
			// Store error and still go to complete state to show it
			m.err = msg.Error
		}
		m.launchHousekeeping = msg.LaunchHousekeeping
		m.state = StateComplete
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.showAll = !m.showAll
			return m, nil

		case key.Matches(msg, m.keys.Up):
			if m.state == StateFeatureSelect {
				if m.cursor > 0 {
					m.cursor--
				}
			} else if m.state == StateConfirm {
				m.confirmSelection = !m.confirmSelection
			}

		case key.Matches(msg, m.keys.Down):
			if m.state == StateFeatureSelect {
				if m.cursor < len(availableFeatures)-1 {
					m.cursor++
				}
			} else if m.state == StateConfirm {
				m.confirmSelection = !m.confirmSelection
			}

		case key.Matches(msg, m.keys.Select):
			if m.state == StateFeatureSelect {
				feature := availableFeatures[m.cursor]
				m.selectedFeatures[feature.Key] = !m.selectedFeatures[feature.Key]
			}

		case key.Matches(msg, m.keys.Enter):
			switch m.state {
			case StateWelcome:
				m.state = StateFeatureSelect
				return m, nil

			case StateFeatureSelect:
				m.state = StateConfirm
				return m, nil

			case StateConfirm:
				if m.confirmSelection {
					m.state = StateExecute
					return m, m.handleFormSubmission()
				} else {
					// User said No, go back to feature selection
					m.state = StateFeatureSelect
					return m, nil
				}

			case StateExecute:
				m.state = StateComplete
				return m, nil

			case StateComplete:
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

// getSelectedFeatures returns a slice of selected feature keys
func (m *InitModel) getSelectedFeatures() []string {
	var selected []string
	for _, feature := range availableFeatures {
		if m.selectedFeatures[feature.Key] {
			selected = append(selected, feature.Key)
		}
	}
	return selected
}

// View renders the model
func (m *InitModel) View() string {
	var content string

	switch m.state {
	case StateWelcome:
		title := TitleStyle.Render(CaryaASCII)
		welcomeText := TextStyle.Render("Hit 'Enter' to begin the setup process!")
		content = lipgloss.JoinVertical(lipgloss.Center, title, welcomeText)

	case StateFeatureSelect:
		title := TitleStyle.Render("═══ SELECT FEATURES ═══")

		var options []string
		for i, feature := range availableFeatures {
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}

			checked := " "
			if m.selectedFeatures[feature.Key] {
				checked = "✓"
			}

			line := fmt.Sprintf("%s [%s] %s", cursor, checked, feature.Name)
			if m.cursor == i {
				line = SelectedItemStyle.Render(line)
			} else {
				line = ItemStyle.Render(line)
			}
			options = append(options, line)
		}

		optionsText := strings.Join(options, "\n")
		instructions := HelpDescStyle.Render("\nUse ↑/↓ to navigate, 'x' to select/deselect, Enter to continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, optionsText, instructions)

	case StateConfirm:
		title := TitleStyle.Render("═══ CONFIRM SELECTION ═══")

		// Show selected features
		selected := m.getSelectedFeatures()
		var summary string
		if len(selected) == 0 {
			summary = "You haven't selected any features.\nBasic Carya configuration will be initialized.\n"
		} else {
			summary = "You selected the following features:\n\n"
			for _, featureKey := range selected {
				for _, feature := range availableFeatures {
					if feature.Key == featureKey {
						summary += fmt.Sprintf("• %s\n", feature.Name)
						break
					}
				}
			}
			summary += "\n"
		}

		// Show confirmation options
		yesOption := "  Yes, proceed with setup"
		noOption := "  No, go back to feature selection"

		if m.confirmSelection {
			yesOption = SelectedItemStyle.Render("> Yes, proceed with setup")
			noOption = ItemStyle.Render("  No, go back to feature selection")
		} else {
			yesOption = ItemStyle.Render("  Yes, proceed with setup")
			noOption = SelectedItemStyle.Render("> No, go back to feature selection")
		}

		confirmText := summary + "Proceed with setup?\n\n" + yesOption + "\n" + noOption
		instructions := HelpDescStyle.Render("\nUse ↑/↓ to navigate, Enter to confirm")

		content = lipgloss.JoinVertical(lipgloss.Left, title, TextStyle.Render(confirmText), instructions)

	case StateExecute:
		title := TitleStyle.Render("═══ PROCESSING ═══")

		selected := m.getSelectedFeatures()
		var summary string
		if len(selected) == 0 {
			summary = "You didn't select any features.\nInitializing basic Carya configuration..."
		} else {
			summary = "You selected the following features:\n\n"
			for _, featureKey := range selected {
				for _, feature := range availableFeatures {
					if feature.Key == featureKey {
						summary += fmt.Sprintf("✓ %s\n", feature.Name)
						break
					}
				}
			}
			summary += "\nSetting up your repository..."
		}

		executionText := TextStyle.Render(summary)
		helpText := HelpDescStyle.Render("\nPress Enter to complete setup")
		content = lipgloss.JoinVertical(lipgloss.Center, title, executionText, helpText)

	case StateComplete:
		if m.err != nil {
			// Show error state
			title := TitleStyle.Render("═══ SETUP FAILED ═══")
			errorText := TextStyle.Render(fmt.Sprintf("Error: %v\n\nPress Enter to exit.", m.err))
			content = lipgloss.JoinVertical(lipgloss.Center, title, errorText)
		} else if m.launchHousekeeping {
			// Show housekeeping launch message
			title := TitleStyle.Render("═══ SETUP COMPLETE ═══")
			completionText := TextStyle.Render("Basic Carya repository initialized.\n\nLaunching housekeeping setup...\n\nPress Enter to continue.")
			content = lipgloss.JoinVertical(lipgloss.Center, title, completionText)
		} else {
			// Show success state
			selected := m.getSelectedFeatures()
			var summary string
			if len(selected) == 0 {
				summary = "Basic Carya repository initialized (no features enabled)\n"
			} else {
				summary = "Successfully initialized with features:\n\n"
				for _, featureKey := range selected {
					for _, feature := range availableFeatures {
						if feature.Key == featureKey {
							summary += fmt.Sprintf("✓ %s\n", feature.Name)
							break
						}
					}
				}
				summary += "\n"
			}

			title := TitleStyle.Render("═══ SETUP COMPLETE ═══")
			completionText := TextStyle.Render(summary + "Press Enter to exit.")
			content = lipgloss.JoinVertical(lipgloss.Center, title, completionText)
		}
	}

	// Only show help view if explicitly toggled on
	if m.showAll {
		m.help.ShowAll = true
		helpView := m.help.View(m.keys)
		help := HelpStyle.Render(helpView)
		return lipgloss.JoinVertical(lipgloss.Left, content, help)
	}

	return content
}
