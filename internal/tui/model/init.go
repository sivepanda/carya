package model

import (
	"carya/internal/config"
	"carya/internal/identity"
	"carya/internal/tui"
	"carya/internal/tui/shared"
	"fmt"
	"strings"

	initializer "carya/internal/init"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Screen states
const (
	StateWelcome = iota
	StateGlobalDevice
	StateFeatureSelect
	StateConfirm
	StateExecute
	StateComplete
)

var globalConfigExistsFn = config.GlobalConfigExists
var saveGlobalConfigFn = config.SaveGlobalConfig
var defaultDeviceIDFn = identity.DefaultDeviceID

// Feature options
type Feature struct {
	Name        string
	Key         string
	Description string
}

var availableFeatures = []Feature{
	{"Feature-Based Commits", "featcom", "Enable feature-based commit workflows"},
	{"Automated Housekeeping", "housekeep", "Automated repository maintenance"},
	{"Team Sync", "teamsync", "Auto-share working state with teammates via git refs"},
	{"LSP Server", "lsp", "Editor integration for team conflict diagnostics"},
}

// Init represents the Bubble Tea model for the init command
type Init struct {
	help               help.Model
	keys               tui.KeyMap
	spinner            spinner.Model
	state              int
	cursor             int
	selectedFeatures   map[string]bool
	showAll            bool
	width              int
	height             int
	confirmSelection   bool
	err                error
	featureErrors      map[string]error
	launchHousekeeping bool
	needsGlobalSetup   bool
	globalSetupError   string
	deviceInput        textinput.Model
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
		spinner:          shared.NewDefaultSpinner(tui.ColorAccent),
		state:            StateWelcome,
		width:            80,
		selectedFeatures: make(map[string]bool),
		confirmSelection: true, // Default to Yes
	}

	m.needsGlobalSetup = !globalConfigExistsFn()
	m.deviceInput = textinput.New()
	m.deviceInput.Prompt = "Device ID: "
	m.deviceInput.CharLimit = 120
	m.deviceInput.SetWidth(42)
	m.deviceInput.SetValue(defaultDeviceIDFn())

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

		// Create initializer with selected features
		init, err := initializer.NewInitializer(selectedFeatures)
		if err != nil {
			return FormSubmittedMsg{Error: err}
		}

		// Initialize the repository (base failures are hard errors;
		// individual feature failures are collected separately)
		if err := init.Initialize(); err != nil {
			return FormSubmittedMsg{Error: err}
		}

		return FormSubmittedMsg{
			FeatureErrors:      init.FeatureErrors(),
			LaunchHousekeeping: m.IsFeatureEnabled("housekeep"),
		}
	}
}

// FormSubmittedMsg indicates that form processing is complete
type FormSubmittedMsg struct {
	Error              error
	FeatureErrors      map[string]error
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

// HasFeatureError returns true if a specific feature failed to initialize
func (m *Init) HasFeatureError(featureKey string) bool {
	_, has := m.featureErrors[featureKey]
	return has
}

// Update handles messages and updates the model
func (m *Init) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case FormSubmittedMsg:
		if msg.Error != nil {
			m.err = msg.Error
		}
		m.featureErrors = msg.FeatureErrors
		m.launchHousekeeping = msg.LaunchHousekeeping
		m.state = StateComplete
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.state == StateGlobalDevice {
			switch {
			case key.Matches(msg, m.keys.Enter):
				deviceID := strings.TrimSpace(m.deviceInput.Value())
				if deviceID == "" {
					m.globalSetupError = "Device ID cannot be empty"
					return m, nil
				}

				if err := saveGlobalConfigFn(config.GlobalConfig{DeviceID: deviceID}); err != nil {
					m.globalSetupError = fmt.Sprintf("Failed to save global settings: %v", err)
					return m, nil
				}

				m.globalSetupError = ""
				m.state = StateFeatureSelect
				m.deviceInput.Blur()
				return m, nil
			}

			var inputCmd tea.Cmd
			m.deviceInput, inputCmd = m.deviceInput.Update(msg)
			return m, inputCmd
		}

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
				if m.needsGlobalSetup {
					m.state = StateGlobalDevice
					m.deviceInput.Focus()
					return m, textinput.Blink
				}
				m.state = StateFeatureSelect
				return m, nil

			case StateFeatureSelect:
				m.state = StateConfirm
				return m, nil

			case StateConfirm:
				if m.confirmSelection {
					m.state = StateExecute
					return m, tea.Batch(m.spinner.Tick, m.handleFormSubmission())
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

	// Update spinner when in execute state
	if m.state == StateExecute {
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
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
func (m *Init) View() tea.View {
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

	case StateGlobalDevice:
		title := tui.TitleStyle.Render("⚙ GLOBAL DEVICE SETUP")
		desc := tui.HelpDescStyle.Render("This setting is shared across all Carya projects on this machine.")
		desc2 := tui.SubtleTextStyle.Render("Shown in team views as: username (device-id)")

		inputBlock := tui.ActiveBoxStyle.Width(72).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				tui.TextStyle.Render("Choose a global device id:"),
				"",
				m.deviceInput.View(),
			),
		)

		instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("type value • enter continue")
		if m.globalSetupError != "" {
			instructions = tui.ErrorStyle.Render(m.globalSetupError)
		}

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", desc, desc2, "", inputBlock, instructions)

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

		if len(selected) == 0 {
			summaryLines = append(summaryLines, m.spinner.View()+" "+tui.TextStyle.Render("Initializing basic Carya configuration..."))
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
			summaryLines = append(summaryLines, m.spinner.View()+" "+tui.TextStyle.Render("Setting up your repository..."))
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
		} else {
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
							if ferr, has := m.featureErrors[featureKey]; has {
								summaryLines = append(summaryLines, tui.WarningStyle.Render("⚠")+" "+tui.TextStyle.Render(feature.Name))
								summaryLines = append(summaryLines, tui.SubtleTextStyle.Render("    "+ferr.Error()))
							} else {
								summaryLines = append(summaryLines, tui.SuccessStyle.Render("✓")+" "+tui.TextStyle.Render(feature.Name))
							}
							break
						}
					}
				}
			}

			if m.launchHousekeeping {
				summaryLines = append(summaryLines, "")
				summaryLines = append(summaryLines, tui.HeaderStyle.Render("→ Launching housekeeping setup..."))
			}

			successBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(tui.ColorSuccess).
				Padding(1, 2).
				Width(60).
				Render(lipgloss.JoinVertical(lipgloss.Left, summaryLines...))

			var nextSteps []string
			if m.selectedFeatures["teamsync"] && m.featureErrors["teamsync"] == nil {
				nextSteps = append(nextSteps,
					tui.HeaderStyle.Render("Team Sync"),
					tui.SubtleTextStyle.Render("  The daemon will auto-publish and fetch team state."),
					tui.SubtleTextStyle.Render("  Pending changes are flushed every 2 minutes,"),
					tui.SubtleTextStyle.Render("  or manually via ")+tui.TextStyle.Render("carya flush")+tui.SubtleTextStyle.Render("."),
					tui.SubtleTextStyle.Render("  Use ")+tui.TextStyle.Render("carya team")+tui.SubtleTextStyle.Render(" to see teammates."),
					"",
				)
			}
			if m.selectedFeatures["lsp"] && m.featureErrors["lsp"] == nil {
				nextSteps = append(nextSteps,
					tui.HeaderStyle.Render("LSP Server"),
					tui.SubtleTextStyle.Render("  Add to your editor's LSP config:"),
					"",
					tui.TextStyle.Render("  Neovim (lspconfig)"),
					tui.SubtleTextStyle.Render("    cmd = { \"carya\", \"lsp\" }"),
					"",
					tui.TextStyle.Render("  VS Code (settings.json)"),
					tui.SubtleTextStyle.Render("    \"carya.lsp.command\": \"carya lsp\""),
					"",
					tui.SubtleTextStyle.Render("  Hover over diagnostics to see teammate diffs."),
					"",
				)
			}

			var sections []string
			sections = append(sections, title, "", successBox)
			if len(nextSteps) > 0 {
				nextBox := lipgloss.NewStyle().
					Padding(1, 2).
					Width(60).
					Render(lipgloss.JoinVertical(lipgloss.Left, nextSteps...))
				sections = append(sections, "", nextBox)
			}

			hint := "enter exit"
			if m.launchHousekeeping {
				hint = "enter continue"
			}
			sections = append(sections, tui.HelpDescStyle.Margin(1, 0, 0, 0).Render(hint))
			content = lipgloss.JoinVertical(lipgloss.Left, sections...)
		}
	}

	// Only show help view if explicitly toggled on
	if m.showAll {
		m.help.ShowAll = true
		helpView := m.help.View(m.keys)
		help := tui.HelpStyle.Render(helpView)
		v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, content, help))
		v.AltScreen = true
		return v
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
