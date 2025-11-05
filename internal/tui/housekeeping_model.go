package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"carya/internal/housekeeping"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Screen states for housekeeping
const (
	HKStateDetecting = iota
	HKStatePackageSelect
	HKStateCategorySelect
	HKStateCommandSelect
	HKStateManualInput
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

// PackageItem represents a detected package with selection state
type PackageItem struct {
	Package  housekeeping.DetectedPackage
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
	packages          []PackageItem // Detected packages with selection state
	packageCursor     int
	categories        []CategoryItem
	categoryCursor    int
	currentCategory   int // Index for multi-category processing
	suggestions       []SuggestionItem
	manualInput       textinput.Model
	manualInputs      []textinput.Model // For command, workingDir, description
	manualInputFocus  int
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

	// Initialize text inputs for manual command entry
	commandInput := textinput.New()
	commandInput.Placeholder = "e.g., npm run build"
	commandInput.Focus()
	commandInput.CharLimit = 256
	commandInput.Width = 50

	workingDirInput := textinput.New()
	workingDirInput.Placeholder = "e.g., ."
	workingDirInput.CharLimit = 256
	workingDirInput.Width = 50

	descriptionInput := textinput.New()
	descriptionInput.Placeholder = "e.g., Build the project"
	descriptionInput.CharLimit = 256
	descriptionInput.Width = 50

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
		manualInputs: []textinput.Model{commandInput, workingDirInput, descriptionInput},
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

		// Get suggestions only from selected packages
		var suggestions []housekeeping.Command
		for _, pkgItem := range m.packages {
			if pkgItem.Selected {
				for _, pkgType := range housekeeping.PackageTypes {
					if pkgType.Name == pkgItem.Package.Type.Name {
						if commands, exists := pkgType.Commands[categoryName]; exists {
							suggestions = append(suggestions, commands...)
						}
						break
					}
				}
			}
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

		// Initialize package items with all selected by default
		m.packages = make([]PackageItem, len(m.detected))
		for i, pkg := range m.detected {
			m.packages[i] = PackageItem{
				Package:  pkg,
				Selected: true,
			}
		}

		m.state = HKStatePackageSelect
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
		// Handle manual input state specially
		if m.state == HKStateManualInput {
			switch msg.String() {
			case "esc":
				// Cancel and go back to command select
				m.state = HKStateCommandSelect
				// Reset inputs
				for i := range m.manualInputs {
					m.manualInputs[i].SetValue("")
				}
				m.manualInputFocus = 0
				m.manualInputs[0].Focus()
				for i := 1; i < len(m.manualInputs); i++ {
					m.manualInputs[i].Blur()
				}
				return m, nil
			case "tab", "down":
				// Move to next input
				m.manualInputs[m.manualInputFocus].Blur()
				m.manualInputFocus = (m.manualInputFocus + 1) % len(m.manualInputs)
				m.manualInputs[m.manualInputFocus].Focus()
				return m, nil
			case "shift+tab", "up":
				// Move to previous input
				m.manualInputs[m.manualInputFocus].Blur()
				m.manualInputFocus = (m.manualInputFocus - 1 + len(m.manualInputs)) % len(m.manualInputs)
				m.manualInputs[m.manualInputFocus].Focus()
				return m, nil
			case "enter":
				// Add the manual command
				cmd := m.manualInputs[0].Value()
				workingDir := m.manualInputs[1].Value()
				desc := m.manualInputs[2].Value()

				if cmd == "" {
					m.err = fmt.Errorf("command cannot be empty")
					m.state = HKStateComplete
					return m, nil
				}
				if workingDir == "" {
					workingDir = "."
				}
				if desc == "" {
					desc = cmd
				}

				// Add to suggestions
				m.suggestions = append(m.suggestions, SuggestionItem{
					Command: housekeeping.Command{
						Command:     cmd,
						WorkingDir:  workingDir,
						Description: desc,
					},
					Selected: true,
				})

				// Reset inputs and go back
				for i := range m.manualInputs {
					m.manualInputs[i].SetValue("")
				}
				m.manualInputFocus = 0
				m.manualInputs[0].Focus()
				for i := 1; i < len(m.manualInputs); i++ {
					m.manualInputs[i].Blur()
				}
				m.state = HKStateCommandSelect
				return m, nil
			default:
				// Update the focused input
				var cmd tea.Cmd
				m.manualInputs[m.manualInputFocus], cmd = m.manualInputs[m.manualInputFocus].Update(msg)
				return m, cmd
			}
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.showAll = !m.showAll
			return m, nil

		case key.Matches(msg, m.keys.Up):
			if m.state == HKStatePackageSelect {
				if m.packageCursor > 0 {
					m.packageCursor--
				}
			} else if m.state == HKStateCategorySelect {
				if m.categoryCursor > 0 {
					m.categoryCursor--
				}
			} else if m.state == HKStateCommandSelect {
				if m.cursor > 0 {
					m.cursor--
				}
			}

		case key.Matches(msg, m.keys.Down):
			if m.state == HKStatePackageSelect {
				if m.packageCursor < len(m.packages)-1 {
					m.packageCursor++
				}
			} else if m.state == HKStateCategorySelect {
				if m.categoryCursor < len(m.categories)-1 {
					m.categoryCursor++
				}
			} else if m.state == HKStateCommandSelect {
				if m.cursor < len(m.suggestions)-1 {
					m.cursor++
				}
			}

		case key.Matches(msg, m.keys.Select):
			if m.state == HKStatePackageSelect {
				m.packages[m.packageCursor].Selected = !m.packages[m.packageCursor].Selected
			} else if m.state == HKStateCategorySelect {
				m.categories[m.categoryCursor].Selected = !m.categories[m.categoryCursor].Selected
			} else if m.state == HKStateCommandSelect {
				m.suggestions[m.cursor].Selected = !m.suggestions[m.cursor].Selected
			}

		case msg.String() == "i":
			// Manual input mode - only in command select state
			if m.state == HKStateCommandSelect {
				m.state = HKStateManualInput
				m.manualInputFocus = 0
				m.manualInputs[0].Focus()
				return m, nil
			}

		case key.Matches(msg, m.keys.Enter):
			switch m.state {
			case HKStatePackageSelect:
				// Check if any packages are selected
				hasSelected := false
				for _, pkg := range m.packages {
					if pkg.Selected {
						hasSelected = true
						break
					}
				}

				if !hasSelected {
					m.err = fmt.Errorf("no packages selected")
					m.state = HKStateComplete
					return m, nil
				}

				m.state = HKStateCategorySelect
				return m, nil

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

	case HKStatePackageSelect:
		title := TitleStyle.Render(IconCheck + " DETECTED PACKAGES")

		packageTitle := HeaderStyle.Margin(0, 0, ComponentGap, 0).Render("Select which package managers to use:")

		// Show package selection
		var options []string
		for i, pkgItem := range m.packages {
			cursor := "  "
			if m.packageCursor == i {
				cursor = IconCursor + " "
			}

			checkbox := IconCheckbox
			if pkgItem.Selected {
				checkbox = IconChecked
			}

			line := cursor + checkbox + " " + pkgItem.Package.Type.Description
			if m.packageCursor == i {
				line = SelectedItemStyle.Render(line)
			} else {
				line = ItemStyle.Render(line)
			}
			options = append(options, line)
		}

		packagesBox := BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, options...),
		)

		instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("↑/↓ navigate • x toggle • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", packageTitle, packagesBox, instructions)

	case HKStateCategorySelect:
		title := TitleStyle.Render(IconCheck + " SELECTED PACKAGES")

		// Show selected packages in a box
		var selectedList []string
		for _, pkgItem := range m.packages {
			if pkgItem.Selected {
				selectedList = append(selectedList, SubtleTextStyle.Render("  "+IconBullet)+" "+TextStyle.Render(pkgItem.Package.Type.Description))
			}
		}

		packagesBox := DimBoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, selectedList...),
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

		instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("↑/↓ navigate • x toggle • i add manual • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", commandsBox, instructions)

	case HKStateManualInput:
		currentCategoryName := m.categories[m.currentCategory].Name
		title := TitleStyle.Render(fmt.Sprintf(IconSettings+" ADD MANUAL COMMAND (%s)", strings.ToUpper(currentCategoryName)))

		formTitle := HeaderStyle.Margin(0, 0, ComponentGap, 0).Render("Enter command details:")

		// Build the form
		var formFields []string

		labels := []string{"Command:", "Working Directory:", "Description:"}
		for i, input := range m.manualInputs {
			label := labels[i]
			if i == m.manualInputFocus {
				label = SelectedItemStyle.Render(label)
			} else {
				label = TextStyle.Render(label)
			}
			formFields = append(formFields, label)
			formFields = append(formFields, "  "+input.View())
			if i < len(m.manualInputs)-1 {
				formFields = append(formFields, "")
			}
		}

		formBox := BoxStyle.Width(70).Render(
			lipgloss.JoinVertical(lipgloss.Left, formFields...),
		)

		instructions := HelpDescStyle.Margin(ComponentGap, 0, 0, 0).Render("tab/↑/↓ navigate fields • enter submit • esc cancel")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", formTitle, formBox, instructions)

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
