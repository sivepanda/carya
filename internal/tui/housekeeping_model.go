package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"carya/internal/housekeeping"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Screen states for housekeeping
const (
	HKStateDetecting = iota
	HKStateCategorySelect
	HKStateCommandSelect
	HKStateConfirm
	HKStateExecute
	HKStateComplete
)

// SuggestionItem represents a command suggestion with selection state
type SuggestionItem struct {
	Command  housekeeping.Command
	Selected bool
}

// HousekeepingModel represents the Bubble Tea model for housekeeping setup
type HousekeepingModel struct {
	help              help.Model
	keys              KeyMap
	state             int
	cursor            int
	detector          *housekeeping.Detector
	detected          []housekeeping.DetectedPackage
	selectedCategory  string
	categoryOptions   []string
	categoryCursor    int
	suggestions       []SuggestionItem
	err               error
	width             int
	height            int
	showAll           bool
	config            *housekeeping.Config
	addedCount        int
}

// NewHousekeepingModel creates a new housekeeping model
func NewHousekeepingModel() HousekeepingModel {
	h := help.New()
	h.Styles.ShortDesc = HelpDescStyle
	h.Styles.ShortKey = HelpKeyStyle
	h.Styles.FullDesc = HelpDescStyle
	h.Styles.FullKey = HelpKeyStyle

	detector := housekeeping.NewDetector(".")

	m := HousekeepingModel{
		help:            h,
		keys:            DefaultKeys(),
		state:           HKStateDetecting,
		detector:        detector,
		width:           80,
		categoryOptions: []string{"post-pull", "post-checkout"},
	}

	return m
}

// Init initializes the model
func (m HousekeepingModel) Init() tea.Cmd {
	return m.detectPackages()
}

// ensureCaryaDirectory creates .carya directory and adds it to .gitignore if needed
func ensureCaryaDirectory() error {
	// Create .carya directory
	caryaDir := ".carya"
	if err := os.MkdirAll(caryaDir, 0755); err != nil {
		return fmt.Errorf("failed to create .carya directory: %w", err)
	}

	// Ensure .carya/ is in .gitignore
	gitignorePath := ".gitignore"
	caryaEntry := ".carya/"

	// Check if .gitignore exists and if .carya/ is already in it
	content := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		content = string(data)

		// Check if .carya/ is already in .gitignore
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == caryaEntry || line == ".carya" {
				// Already present
				return nil
			}
		}
	}

	// Add .carya/ to .gitignore
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// Don't fail if we can't update .gitignore
		return nil
	}
	defer f.Close()

	// Add newline before entry if file doesn't end with one
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		f.WriteString("\n")
	}

	// Add comment and entry
	if len(content) == 0 {
		f.WriteString("# Carya directory\n")
	}

	f.WriteString(caryaEntry + "\n")

	return nil
}

// detectPackages runs package detection
func (m HousekeepingModel) detectPackages() tea.Cmd {
	return func() tea.Msg {
		// Ensure .carya directory exists first
		if err := ensureCaryaDirectory(); err != nil {
			return DetectionCompleteMsg{Error: err}
		}

		detected, err := m.detector.DetectPackages()
		if err != nil {
			return DetectionCompleteMsg{Error: err}
		}

		config, err := housekeeping.LoadConfig()
		if err != nil {
			return DetectionCompleteMsg{Error: err}
		}

		return DetectionCompleteMsg{
			Detected: detected,
			Config:   config,
			Error:    nil,
		}
	}
}

// getSuggestions retrieves suggestions for the selected category
func (m HousekeepingModel) getSuggestions() tea.Cmd {
	return func() tea.Msg {
		suggestions, err := m.detector.GetSuggestedCommands(m.selectedCategory)
		if err != nil {
			return SuggestionsLoadedMsg{Error: err}
		}

		items := make([]SuggestionItem, len(suggestions))
		for i, cmd := range suggestions {
			items[i] = SuggestionItem{
				Command:  cmd,
				Selected: true, // Default to all selected
			}
		}

		return SuggestionsLoadedMsg{
			Suggestions: items,
			Error:       nil,
		}
	}
}

// addSelectedCommands adds the selected commands to the config
func (m HousekeepingModel) addSelectedCommands() tea.Cmd {
	return func() tea.Msg {
		count := 0
		for _, item := range m.suggestions {
			if item.Selected {
				err := m.config.AddCommand(
					m.selectedCategory,
					item.Command.Command,
					item.Command.WorkingDir,
					item.Command.Description,
				)
				if err != nil {
					return CommandsAddedMsg{Error: err}
				}
				count++
			}
		}

		if count > 0 {
			err := m.config.Save()
			if err != nil {
				return CommandsAddedMsg{Error: err}
			}
		}

		return CommandsAddedMsg{Count: count, Error: nil}
	}
}

// DetectionCompleteMsg indicates package detection is complete
type DetectionCompleteMsg struct {
	Detected []housekeeping.DetectedPackage
	Config   *housekeeping.Config
	Error    error
}

// SuggestionsLoadedMsg indicates suggestions have been loaded
type SuggestionsLoadedMsg struct {
	Suggestions []SuggestionItem
	Error       error
}

// CommandsAddedMsg indicates commands have been added
type CommandsAddedMsg struct {
	Count int
	Error error
}

// Update handles messages and updates the model
func (m HousekeepingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DetectionCompleteMsg:
		if msg.Error != nil {
			m.err = msg.Error
			m.state = HKStateComplete
			return m, nil
		}
		m.detected = msg.Detected
		m.config = msg.Config

		if len(m.detected) == 0 {
			m.err = fmt.Errorf("no package managers detected")
			m.state = HKStateComplete
			return m, nil
		}

		m.state = HKStateCategorySelect
		return m, nil

	case SuggestionsLoadedMsg:
		if msg.Error != nil {
			m.err = msg.Error
			m.state = HKStateComplete
			return m, nil
		}
		m.suggestions = msg.Suggestions
		m.cursor = 0

		if len(m.suggestions) == 0 {
			m.err = fmt.Errorf("no suggestions for %s", m.selectedCategory)
			m.state = HKStateComplete
			return m, nil
		}

		m.state = HKStateCommandSelect
		return m, nil

	case CommandsAddedMsg:
		if msg.Error != nil {
			m.err = msg.Error
		}
		m.addedCount = msg.Count
		m.state = HKStateComplete
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
			if m.state == HKStateCategorySelect {
				if m.categoryCursor > 0 {
					m.categoryCursor--
				}
			} else if m.state == HKStateCommandSelect {
				if m.cursor > 0 {
					m.cursor--
				}
			}

		case key.Matches(msg, m.keys.Down):
			if m.state == HKStateCategorySelect {
				if m.categoryCursor < len(m.categoryOptions)-1 {
					m.categoryCursor++
				}
			} else if m.state == HKStateCommandSelect {
				if m.cursor < len(m.suggestions)-1 {
					m.cursor++
				}
			}

		case key.Matches(msg, m.keys.Select):
			if m.state == HKStateCommandSelect {
				m.suggestions[m.cursor].Selected = !m.suggestions[m.cursor].Selected
			}

		case key.Matches(msg, m.keys.Enter):
			switch m.state {
			case HKStateCategorySelect:
				m.selectedCategory = m.categoryOptions[m.categoryCursor]
				return m, m.getSuggestions()

			case HKStateCommandSelect:
				// Check if any commands are selected
				hasSelected := false
				for _, item := range m.suggestions {
					if item.Selected {
						hasSelected = true
						break
					}
				}

				if !hasSelected {
					m.err = fmt.Errorf("no commands selected")
					m.state = HKStateComplete
					return m, nil
				}

				m.state = HKStateConfirm
				return m, nil

			case HKStateConfirm:
				m.state = HKStateExecute
				return m, m.addSelectedCommands()

			case HKStateComplete:
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

// View renders the model
func (m HousekeepingModel) View() string {
	var content string

	switch m.state {
	case HKStateDetecting:
		title := TitleStyle.Render("═══ HOUSEKEEPING SETUP ═══")
		detectingText := TextStyle.Render("Detecting package managers and build systems...")
		content = lipgloss.JoinVertical(lipgloss.Left, title, detectingText)

	case HKStateCategorySelect:
		title := TitleStyle.Render("═══ DETECTED PACKAGES ═══")

		// Show detected packages
		var detectedList []string
		for _, pkg := range m.detected {
			detectedList = append(detectedList, fmt.Sprintf("  • %s", pkg.Type.Description))
		}
		detectedText := TextStyle.Render(strings.Join(detectedList, "\n"))

		// Show category selection
		categoryTitle := TextStyle.Render("\nSelect category for housekeeping commands:\n")

		var options []string
		for i, category := range m.categoryOptions {
			cursor := " "
			if m.categoryCursor == i {
				cursor = ">"
			}

			line := fmt.Sprintf("%s %s", cursor, category)
			if m.categoryCursor == i {
				line = SelectedItemStyle.Render(line)
			} else {
				line = ItemStyle.Render(line)
			}
			options = append(options, line)
		}

		optionsText := strings.Join(options, "\n")
		instructions := HelpDescStyle.Render("\nUse ↑/↓ to navigate, Enter to select")

		content = lipgloss.JoinVertical(lipgloss.Left, title, detectedText, categoryTitle, optionsText, instructions)

	case HKStateCommandSelect:
		title := TitleStyle.Render(fmt.Sprintf("═══ SUGGESTED %s COMMANDS ═══", strings.ToUpper(m.selectedCategory)))

		var options []string
		for i, item := range m.suggestions {
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}

			checked := " "
			if item.Selected {
				checked = "✓"
			}

			line := fmt.Sprintf("%s [%s] %s", cursor, checked, item.Command.Description)
			cmdLine := fmt.Sprintf("       %s", item.Command.Command)

			if m.cursor == i {
				line = SelectedItemStyle.Render(line)
				cmdLine = HelpDescStyle.Render(cmdLine)
			} else {
				line = ItemStyle.Render(line)
				cmdLine = HelpDescStyle.Render(cmdLine)
			}

			options = append(options, line)
			options = append(options, cmdLine)
		}

		optionsText := strings.Join(options, "\n")
		instructions := HelpDescStyle.Render("\nUse ↑/↓ to navigate, 'x' to toggle selection, Enter to continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, optionsText, instructions)

	case HKStateConfirm:
		title := TitleStyle.Render("═══ CONFIRM SELECTION ═══")

		// Count selected
		selectedCount := 0
		var selectedList []string
		for _, item := range m.suggestions {
			if item.Selected {
				selectedCount++
				selectedList = append(selectedList, fmt.Sprintf("  • %s", item.Command.Description))
			}
		}

		summary := fmt.Sprintf("Ready to add %d %s commands:\n\n%s\n\n",
			selectedCount,
			m.selectedCategory,
			strings.Join(selectedList, "\n"))

		confirmText := summary + "Press Enter to add these commands, or 'q' to cancel."
		instructions := HelpDescStyle.Render("\nEnter to confirm, q to quit")

		content = lipgloss.JoinVertical(lipgloss.Left, title, TextStyle.Render(confirmText), instructions)

	case HKStateExecute:
		title := TitleStyle.Render("═══ ADDING COMMANDS ═══")
		executionText := TextStyle.Render("Adding selected commands to configuration...")
		content = lipgloss.JoinVertical(lipgloss.Left, title, executionText)

	case HKStateComplete:
		if m.err != nil {
			title := TitleStyle.Render("═══ ERROR ═══")
			errorText := TextStyle.Render(fmt.Sprintf("Error: %v\n\nPress Enter to exit.", m.err))
			content = lipgloss.JoinVertical(lipgloss.Left, title, errorText)
		} else {
			title := TitleStyle.Render("═══ COMPLETE ═══")
			successText := TextStyle.Render(fmt.Sprintf(
				"Successfully added %d %s commands!\n\nPress Enter to exit.",
				m.addedCount,
				m.selectedCategory,
			))
			content = lipgloss.JoinVertical(lipgloss.Left, title, successText)
		}
	}

	// Add help view at the bottom
	m.help.ShowAll = m.showAll
	helpView := m.help.View(m.keys)
	helpText := HelpStyle.Render(helpView)

	return lipgloss.JoinVertical(lipgloss.Left, content, helpText)
}
