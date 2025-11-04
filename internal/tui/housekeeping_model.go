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

// CategoryItem represents a category with selection state
type CategoryItem struct {
	Name     string
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
	categories        []CategoryItem
	categoryCursor    int
	currentCategory   int // Index for multi-category processing
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
		help:     h,
		keys:     DefaultKeys(),
		state:    HKStateDetecting,
		detector: detector,
		width:    80,
		categories: []CategoryItem{
			{Name: "post-pull", Selected: true},
			{Name: "post-checkout", Selected: true},
		},
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

// getSuggestions retrieves suggestions for the current category being processed
func (m HousekeepingModel) getSuggestions() tea.Cmd {
	return func() tea.Msg {
		categoryName := m.categories[m.currentCategory].Name
		suggestions, err := m.detector.GetSuggestedCommands(categoryName)
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
			Category:    categoryName,
			Suggestions: items,
			Error:       nil,
		}
	}
}

// addSelectedCommands adds the selected commands to the config
func (m HousekeepingModel) addSelectedCommands() tea.Cmd {
	return func() tea.Msg {
		categoryName := m.categories[m.currentCategory].Name
		count := 0
		for _, item := range m.suggestions {
			if item.Selected {
				err := m.config.AddCommand(
					categoryName,
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

		return CommandsAddedMsg{
			Count:    count,
			Category: categoryName,
			Error:    nil,
		}
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
	Category    string
	Suggestions []SuggestionItem
	Error       error
}

// CommandsAddedMsg indicates commands have been added
type CommandsAddedMsg struct {
	Count    int
	Category string
	Error    error
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
			m.err = fmt.Errorf("no suggestions for %s", msg.Category)
			m.state = HKStateComplete
			return m, nil
		}

		m.state = HKStateCommandSelect
		return m, nil

	case CommandsAddedMsg:
		if msg.Error != nil {
			m.err = msg.Error
			m.state = HKStateComplete
			return m, nil
		}
		m.addedCount += msg.Count

		// Find next selected category
		m.currentCategory++
		for m.currentCategory < len(m.categories) && !m.categories[m.currentCategory].Selected {
			m.currentCategory++
		}

		// If there are more categories to process, get suggestions for the next one
		if m.currentCategory < len(m.categories) {
			return m, m.getSuggestions()
		}

		// All categories processed
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
				if m.categoryCursor < len(m.categories)-1 {
					m.categoryCursor++
				}
			} else if m.state == HKStateCommandSelect {
				if m.cursor < len(m.suggestions)-1 {
					m.cursor++
				}
			}

		case key.Matches(msg, m.keys.Select):
			if m.state == HKStateCategorySelect {
				m.categories[m.categoryCursor].Selected = !m.categories[m.categoryCursor].Selected
			} else if m.state == HKStateCommandSelect {
				m.suggestions[m.cursor].Selected = !m.suggestions[m.cursor].Selected
			}

		case key.Matches(msg, m.keys.Enter):
			switch m.state {
			case HKStateCategorySelect:
				// Check if any categories are selected
				hasSelected := false
				for i, cat := range m.categories {
					if cat.Selected {
						hasSelected = true
						m.currentCategory = i
						break
					}
				}

				if !hasSelected {
					m.err = fmt.Errorf("no categories selected")
					m.state = HKStateComplete
					return m, nil
				}

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
		title := TitleStyle.Render(IconSettings + " HOUSEKEEPING SETUP")

		spinner := SubtleTextStyle.Render(IconSpinner)
		detectingText := TextStyle.Render("  Detecting package managers and build systems...")

		box := BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				spinner+" "+detectingText,
			),
		)

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", box)

	case HKStateCategorySelect:
		title := TitleStyle.Render(IconCheck + " DETECTED PACKAGES")

		// Show detected packages in a box
		var detectedList []string
		for _, pkg := range m.detected {
			detectedList = append(detectedList, SubtleTextStyle.Render("  "+IconBullet)+" "+TextStyle.Render(pkg.Type.Description))
		}

		packagesBox := DimBoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, detectedList...),
		)

		// Show category selection
		categoryTitle := HeaderStyle.Margin(SectionGap, 0, ComponentGap, 0).Render("Select categories to configure:")

		var options []string
		for i, category := range m.categories {
			cursor := "  "
			if m.categoryCursor == i {
				cursor = IconCursor + " "
			}

			checkbox := IconCheckbox
			if category.Selected {
				checkbox = IconChecked
			}

			line := cursor + checkbox + " " + category.Name
			if m.categoryCursor == i {
				line = SelectedItemStyle.Render(line)
			} else {
				line = ItemStyle.Render(line)
			}
			options = append(options, line)
		}

		optionsBox := BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, options...),
		)

		instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("↑/↓ navigate • x toggle • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", packagesBox, categoryTitle, optionsBox, instructions)

	case HKStateCommandSelect:
		currentCategoryName := m.categories[m.currentCategory].Name
		title := TitleStyle.Render(fmt.Sprintf(IconSettings+" %s COMMANDS", strings.ToUpper(currentCategoryName)))

		var options []string
		for i, item := range m.suggestions {
			cursor := "  "
			if m.cursor == i {
				cursor = IconCursor + " "
			}

			checkbox := IconCheckbox
			if item.Selected {
				checkbox = IconChecked
			}

			line := cursor + checkbox + " " + item.Command.Description
			cmdLine := "    " + item.Command.Command

			if m.cursor == i {
				line = SelectedItemStyle.Render(line)
				cmdLine = SubtleTextStyle.Render(cmdLine)
			} else {
				line = ItemStyle.Render(line)
				cmdLine = HelpDescStyle.Render(cmdLine)
			}

			options = append(options, line)
			options = append(options, cmdLine)
			if i < len(m.suggestions)-1 {
				options = append(options, "")
			}
		}

		commandsBox := ActiveBoxStyle.Width(70).Render(
			lipgloss.JoinVertical(lipgloss.Left, options...),
		)

		instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("↑/↓ navigate • x toggle • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", commandsBox, instructions)

	case HKStateConfirm:
		title := TitleStyle.Render(IconCheck + " CONFIRM SELECTION")

		// Count selected
		selectedCount := 0
		var selectedList []string
		for _, item := range m.suggestions {
			if item.Selected {
				selectedCount++
				selectedList = append(selectedList, SubtleTextStyle.Render("  "+IconBullet)+" "+TextStyle.Render(item.Command.Description))
			}
		}

		currentCategoryName := m.categories[m.currentCategory].Name
		countHeader := HeaderStyle.Render(fmt.Sprintf("Ready to add %d %s commands:", selectedCount, currentCategoryName))

		summaryBox := BoxStyle.Width(70).Render(
			lipgloss.JoinVertical(lipgloss.Left, selectedList...),
		)

		instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("enter confirm • q cancel")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", countHeader, "", summaryBox, instructions)

	case HKStateExecute:
		title := TitleStyle.Render(IconSettings + " PROCESSING")

		spinner := SubtleTextStyle.Render(IconSpinner)
		executionText := TextStyle.Render("  Adding selected commands to configuration...")

		box := BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				spinner+" "+executionText,
			),
		)

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", box)

	case HKStateComplete:
		if m.err != nil {
			title := ErrorStyle.Render(IconCross + " ERROR")
			errorMsg := ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err))

			errorBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorError).
				Padding(DefaultPadding, DefaultPadding*2).
				Width(60).
				Render(errorMsg)

			instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("enter exit")
			content = lipgloss.JoinVertical(lipgloss.Left, title, "", errorBox, instructions)
		} else {
			title := SuccessStyle.Render(IconCheck + " COMPLETE")

			// Count how many categories were selected
			selectedCategories := []string{}
			for _, cat := range m.categories {
				if cat.Selected {
					selectedCategories = append(selectedCategories, cat.Name)
				}
			}

			categoryText := strings.Join(selectedCategories, " and ")
			successMsg := SuccessStyle.Render(fmt.Sprintf("Successfully added %d commands for %s!", m.addedCount, categoryText))

			successBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorSuccess).
				Padding(DefaultPadding, DefaultPadding*2).
				Width(60).
				Render(successMsg)

			instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("enter exit")
			content = lipgloss.JoinVertical(lipgloss.Left, title, "", successBox, instructions)
		}
	}

	// Add help view at the bottom
	m.help.ShowAll = m.showAll
	helpView := m.help.View(m.keys)
	helpText := HelpStyle.Render(helpView)

	return lipgloss.JoinVertical(lipgloss.Left, content, helpText)
}
