package model

import (
	"carya/internal/tui"
	"fmt"

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

// Init represents the Bubble Tea model for the init command
type Init struct {
	help               help.Model
	keys               tui.KeyMap
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

// NewInit creates a new init model
func NewInit() Init {
	h := help.New()
	h.Styles.ShortDesc = tui.HelpDescStyle
	h.Styles.ShortKey = tui.HelpKeyStyle
	h.Styles.FullDesc = tui.HelpDescStyle
	h.Styles.FullKey = tui.HelpKeyStyle

	m := Init{
		help:             h,
		keys:             tui.DefaultKeys(),
		state:            StateWelcome,
		width:            80,
		selectedFeatures: make(map[string]bool),
		confirmSelection: true, // Default to Yes
	}

	return m
}

// Init initializes the model
func (m *Init) Init() tea.Cmd {
	return nil
}

// handleFormSubmission processes the form data and executes the setup
func (m *Init) handleFormSubmission() tea.Cmd {
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
func (m *Init) ShouldLaunchHousekeeping() bool {
	return m.launchHousekeeping
}

// IsFeatureEnabled returns true if a feature is enabled
func (m *Init) IsFeatureEnabled(featureKey string) bool {
	return m.selectedFeatures[featureKey]
}

// Update handles messages and updates the model
func (m *Init) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
func (m *Init) getSelectedFeatures() []string {
	var selected []string
	for _, feature := range availableFeatures {
		if m.selectedFeatures[feature.Key] {
			selected = append(selected, feature.Key)
		}
	}
	return selected
}

// View renders the model
func (m *Init) View() string {
	var content string

	switch m.state {
	case StateWelcome:
		asciiStyle := lipgloss.NewStyle().
			Foreground(tui.ColorTitle).
			Bold(true)
		title := asciiStyle.Render(tui.CaryaASCII)

		welcomeBox := tui.BoxStyle.
			Width(60).
			Align(lipgloss.Center).
			Render(tui.HeaderStyle.Render("Hit 'Enter' to begin the setup process!"))

		content = lipgloss.JoinVertical(lipgloss.Center, title, "", welcomeBox)

	case StateFeatureSelect:
		title := tui.TitleStyle.Render("⚙ SELECT FEATURES")

		var options []string
		for i, feature := range availableFeatures {
			cursor := "  "
			if m.cursor == i {
				cursor = "❯ "
			}

			checkbox := "☐"
			if m.selectedFeatures[feature.Key] {
				checkbox = "☑"
			}

			line := cursor + checkbox + " " + feature.Name
			desc := "    " + feature.Description

			if m.cursor == i {
				line = tui.SelectedItemStyle.Render(line)
				desc = tui.SubtleTextStyle.Render(desc)
			} else {
				line = tui.ItemStyle.Render(line)
				desc = tui.HelpDescStyle.Render(desc)
			}

			options = append(options, line)
			options = append(options, desc)
			if i < len(availableFeatures)-1 {
				options = append(options, "")
			}
		}

		featuresBox := tui.ActiveBoxStyle.Width(70).Render(
			lipgloss.JoinVertical(lipgloss.Left, options...),
		)

		instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("↑/↓ navigate • x toggle • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", featuresBox, instructions)

	case StateConfirm:
		title := tui.TitleStyle.Render("✓ CONFIRM SELECTION")

		// Show selected features
		selected := m.getSelectedFeatures()
		var summaryContent []string
		if len(selected) == 0 {
			summaryContent = append(summaryContent, tui.SubtleTextStyle.Render("No features selected"))
			summaryContent = append(summaryContent, tui.TextStyle.Render("Basic Carya configuration will be initialized"))
		} else {
			for _, featureKey := range selected {
				for _, feature := range availableFeatures {
					if feature.Key == featureKey {
						summaryContent = append(summaryContent, tui.SubtleTextStyle.Render("  ●")+" "+tui.TextStyle.Render(feature.Name))
						break
					}
				}
			}
		}

		summaryBox := tui.DimBoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, summaryContent...),
		)

		// Show confirmation options
		questionHeader := tui.HeaderStyle.Margin(2, 0, 1, 0).Render("Proceed with setup?")

		yesOption := "  Yes, proceed with setup"
		noOption := "  No, go back to feature selection"

		if m.confirmSelection {
			yesOption = tui.SelectedItemStyle.Render("❯ Yes, proceed with setup")
			noOption = tui.ItemStyle.Render("  No, go back to feature selection")
		} else {
			yesOption = tui.ItemStyle.Render("  Yes, proceed with setup")
			noOption = tui.SelectedItemStyle.Render("❯ No, go back to feature selection")
		}

		choicesBox := tui.BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, yesOption, noOption),
		)

		instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("↑/↓ navigate • enter confirm")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", summaryBox, questionHeader, choicesBox, instructions)

	case StateExecute:
		title := tui.TitleStyle.Render("⚙ PROCESSING")

		selected := m.getSelectedFeatures()
		var summaryLines []string

		spinner := tui.SubtleTextStyle.Render("◐")

		if len(selected) == 0 {
			summaryLines = append(summaryLines, spinner+" "+tui.TextStyle.Render("Initializing basic Carya configuration..."))
		} else {
			for _, featureKey := range selected {
				for _, feature := range availableFeatures {
					if feature.Key == featureKey {
						summaryLines = append(summaryLines, tui.SuccessStyle.Render("✓")+" "+tui.TextStyle.Render(feature.Name))
						break
					}
				}
			}
			summaryLines = append(summaryLines, "")
			summaryLines = append(summaryLines, spinner+" "+tui.TextStyle.Render("Setting up your repository..."))
		}

		processingBox := tui.BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, summaryLines...),
		)

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", processingBox)

	case StateComplete:
		if m.err != nil {
			// Show error state
			title := tui.ErrorStyle.Render("✗ SETUP FAILED")
			errorMsg := tui.ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err))

			errorBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(tui.ColorError).
				Padding(1, 2).
				Width(60).
				Render(errorMsg)

			instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("enter exit")
			content = lipgloss.JoinVertical(lipgloss.Left, title, "", errorBox, instructions)
		} else if m.launchHousekeeping {
			// Show housekeeping launch message
			title := tui.SuccessStyle.Render("✓ SETUP COMPLETE")

			var msgLines []string
			msgLines = append(msgLines, tui.TextStyle.Render("Basic Carya repository initialized"))
			msgLines = append(msgLines, "")
			msgLines = append(msgLines, tui.HeaderStyle.Render("→ Launching housekeeping setup..."))

			launchBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(tui.ColorSuccess).
				Padding(1, 2).
				Width(60).
				Render(lipgloss.JoinVertical(lipgloss.Left, msgLines...))

			instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("enter continue")
			content = lipgloss.JoinVertical(lipgloss.Left, title, "", launchBox, instructions)
		} else {
			// Show success state
			title := tui.SuccessStyle.Render("✓ SETUP COMPLETE")

			selected := m.getSelectedFeatures()
			var summaryLines []string
			if len(selected) == 0 {
				summaryLines = append(summaryLines, tui.TextStyle.Render("Basic Carya repository initialized"))
				summaryLines = append(summaryLines, tui.SubtleTextStyle.Render("(no features enabled)"))
			} else {
				for _, featureKey := range selected {
					for _, feature := range availableFeatures {
						if feature.Key == featureKey {
							summaryLines = append(summaryLines, tui.SuccessStyle.Render("✓")+" "+tui.TextStyle.Render(feature.Name))
							break
						}
					}
				}
			}

			successBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(tui.ColorSuccess).
				Padding(1, 2).
				Width(60).
				Render(lipgloss.JoinVertical(lipgloss.Left, summaryLines...))

			instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("enter exit")
			content = lipgloss.JoinVertical(lipgloss.Left, title, "", successBox, instructions)
		}
	}

	// Only show help view if explicitly toggled on
	if m.showAll {
		m.help.ShowAll = true
		helpView := m.help.View(m.keys)
		help := tui.HelpStyle.Render(helpView)
		return lipgloss.JoinVertical(lipgloss.Left, content, help)
	}

	return content
}
