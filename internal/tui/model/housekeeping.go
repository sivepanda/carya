package model

import (
	"bufio"
	"carya/internal/housekeeping"
	"carya/internal/tui"
	"fmt"
	"os"
	"strings"

<<<<<<< Updated upstream:internal/tui/model/housekeeping.go
=======
	"carya/internal/housekeeping"
	"carya/internal/tui/shared"

>>>>>>> Stashed changes:internal/tui/housekeeping_model.go
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
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

// Housekeeping represents the Bubble Tea model for housekeeping setup
type Housekeeping struct {
	help              help.Model
<<<<<<< Updated upstream:internal/tui/model/housekeeping.go
	keys              tui.KeyMap
=======
	keys              KeyMap
	spinner           spinner.Model
>>>>>>> Stashed changes:internal/tui/housekeeping_model.go
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

// NewHousekeeping creates a new housekeeping model
func NewHousekeeping() Housekeeping {
	h := help.New()
	h.Styles.ShortDesc = tui.HelpDescStyle
	h.Styles.ShortKey = tui.HelpKeyStyle
	h.Styles.FullDesc = tui.HelpDescStyle
	h.Styles.FullKey = tui.HelpKeyStyle

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

	m := Housekeeping{
		help:     h,
<<<<<<< Updated upstream:internal/tui/model/housekeeping.go
		keys:     tui.DefaultKeys(),
=======
		keys:     DefaultKeys(),
		spinner:  shared.NewDefaultSpinner(ColorAccent),
>>>>>>> Stashed changes:internal/tui/housekeeping_model.go
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
<<<<<<< Updated upstream:internal/tui/model/housekeeping.go
func (m Housekeeping) Init() tea.Cmd {
	return m.detectPackages()
=======
func (m HousekeepingModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.detectPackages())
>>>>>>> Stashed changes:internal/tui/housekeeping_model.go
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
func (m Housekeeping) detectPackages() tea.Cmd {
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
func (m Housekeeping) getSuggestions() tea.Cmd {
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
func (m Housekeeping) addSelectedCommands() tea.Cmd {
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
<<<<<<< Updated upstream:internal/tui/model/housekeeping.go
func (m Housekeeping) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
=======
func (m HousekeepingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

>>>>>>> Stashed changes:internal/tui/housekeeping_model.go
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

	// Update spinner if we're in a loading state
	if m.state == HKStateDetecting || m.state == HKStateExecute {
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the model
func (m Housekeeping) View() string {
	var content string

	switch m.state {
	case HKStateDetecting:
		title := tui.TitleStyle.Render(tui.IconSettings + " HOUSEKEEPING SETUP")

<<<<<<< Updated upstream:internal/tui/model/housekeeping.go
		spinner := tui.SubtleTextStyle.Render(tui.IconSpinner)
		detectingText := tui.TextStyle.Render("  Detecting package managers and build systems...")
=======
		detectingText := TextStyle.Render("Detecting package managers and build systems...")
>>>>>>> Stashed changes:internal/tui/housekeeping_model.go

		box := tui.BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				m.spinner.View()+" "+detectingText,
			),
		)

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", box)

	case HKStatePackageSelect:
		title := tui.TitleStyle.Render(tui.IconCheck + " DETECTED PACKAGES")

		packageTitle := tui.HeaderStyle.Margin(0, 0, tui.ComponentGap, 0).Render("Select which package managers to use:")

		// Show package selection
		var options []string
		for i, pkgItem := range m.packages {
			cursor := "  "
			if m.packageCursor == i {
				cursor = tui.IconCursor + " "
			}

			checkbox := tui.IconCheckbox
			if pkgItem.Selected {
				checkbox = tui.IconChecked
			}

			line := cursor + checkbox + " " + pkgItem.Package.Type.Description
			if m.packageCursor == i {
				line = tui.SelectedItemStyle.Render(line)
			} else {
				line = tui.ItemStyle.Render(line)
			}
			options = append(options, line)
		}

		packagesBox := tui.BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, options...),
		)

		instructions := tui.HelpDescStyle.Margin(tui.ComponentGap, 0, 0, 0).Render("↑/↓ navigate • x toggle • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", packageTitle, packagesBox, instructions)

	case HKStateCategorySelect:
		title := tui.TitleStyle.Render(tui.IconCheck + " SELECTED PACKAGES")

		// Show selected packages in a box
		var selectedList []string
		for _, pkgItem := range m.packages {
			if pkgItem.Selected {
				selectedList = append(selectedList, tui.SubtleTextStyle.Render("  "+tui.IconBullet)+" "+tui.TextStyle.Render(pkgItem.Package.Type.Description))
			}
		}

		packagesBox := tui.DimBoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, selectedList...),
		)

		// Show category selection
		categoryTitle := tui.HeaderStyle.Margin(tui.SectionGap, 0, tui.ComponentGap, 0).Render("Select categories to configure:")

		var options []string
		for i, category := range m.categories {
			cursor := "  "
			if m.categoryCursor == i {
				cursor = tui.IconCursor + " "
			}

			checkbox := tui.IconCheckbox
			if category.Selected {
				checkbox = tui.IconChecked
			}

			line := cursor + checkbox + " " + category.Name
			if m.categoryCursor == i {
				line = tui.SelectedItemStyle.Render(line)
			} else {
				line = tui.ItemStyle.Render(line)
			}
			options = append(options, line)
		}

		optionsBox := tui.BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left, options...),
		)

		instructions := tui.HelpDescStyle.Margin(tui.ComponentGap, 0, 0, 0).Render("↑/↓ navigate • x toggle • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", packagesBox, categoryTitle, optionsBox, instructions)

	case HKStateCommandSelect:
		currentCategoryName := m.categories[m.currentCategory].Name
		title := tui.TitleStyle.Render(fmt.Sprintf(tui.IconSettings+" %s COMMANDS", strings.ToUpper(currentCategoryName)))

		var options []string
		for i, item := range m.suggestions {
			cursor := "  "
			if m.cursor == i {
				cursor = tui.IconCursor + " "
			}

			checkbox := tui.IconCheckbox
			if item.Selected {
				checkbox = tui.IconChecked
			}

			line := cursor + checkbox + " " + item.Command.Description
			cmdLine := "    " + item.Command.Command

			if m.cursor == i {
				line = tui.SelectedItemStyle.Render(line)
				cmdLine = tui.SubtleTextStyle.Render(cmdLine)
			} else {
				line = tui.ItemStyle.Render(line)
				cmdLine = tui.HelpDescStyle.Render(cmdLine)
			}

			options = append(options, line)
			options = append(options, cmdLine)
			if i < len(m.suggestions)-1 {
				options = append(options, "")
			}
		}

		commandsBox := tui.ActiveBoxStyle.Width(70).Render(
			lipgloss.JoinVertical(lipgloss.Left, options...),
		)

		instructions := tui.HelpDescStyle.Margin(tui.ComponentGap, 0, 0, 0).Render("↑/↓ navigate • x toggle • i add manual • enter continue")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", commandsBox, instructions)

	case HKStateManualInput:
		currentCategoryName := m.categories[m.currentCategory].Name
		title := tui.TitleStyle.Render(fmt.Sprintf(tui.IconSettings+" ADD MANUAL COMMAND (%s)", strings.ToUpper(currentCategoryName)))

		formTitle := tui.HeaderStyle.Margin(0, 0, tui.ComponentGap, 0).Render("Enter command details:")

		// Build the form
		var formFields []string

		labels := []string{"Command:", "Working Directory:", "Description:"}
		for i, input := range m.manualInputs {
			label := labels[i]
			if i == m.manualInputFocus {
				label = tui.SelectedItemStyle.Render(label)
			} else {
				label = tui.TextStyle.Render(label)
			}
			formFields = append(formFields, label)
			formFields = append(formFields, "  "+input.View())
			if i < len(m.manualInputs)-1 {
				formFields = append(formFields, "")
			}
		}

		formBox := tui.BoxStyle.Width(70).Render(
			lipgloss.JoinVertical(lipgloss.Left, formFields...),
		)

		instructions := tui.HelpDescStyle.Margin(tui.ComponentGap, 0, 0, 0).Render("tab/↑/↓ navigate fields • enter submit • esc cancel")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", formTitle, formBox, instructions)

	case HKStateConfirm:
		title := tui.TitleStyle.Render(tui.IconCheck + " CONFIRM SELECTION")

		// Count selected
		selectedCount := 0
		var selectedList []string
		for _, item := range m.suggestions {
			if item.Selected {
				selectedCount++
				selectedList = append(selectedList, tui.SubtleTextStyle.Render("  "+tui.IconBullet)+" "+tui.TextStyle.Render(item.Command.Description))
			}
		}

		currentCategoryName := m.categories[m.currentCategory].Name
		countHeader := tui.HeaderStyle.Render(fmt.Sprintf("Ready to add %d %s commands:", selectedCount, currentCategoryName))

		summaryBox := tui.BoxStyle.Width(70).Render(
			lipgloss.JoinVertical(lipgloss.Left, selectedList...),
		)

		instructions := tui.HelpDescStyle.Margin(tui.ComponentGap, 0, 0, 0).Render("enter confirm • q cancel")

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", countHeader, "", summaryBox, instructions)

	case HKStateExecute:
		title := tui.TitleStyle.Render(tui.IconSettings + " PROCESSING")

<<<<<<< Updated upstream:internal/tui/model/housekeeping.go
		spinner := tui.SubtleTextStyle.Render(tui.IconSpinner)
		executionText := tui.TextStyle.Render("  Adding selected commands to configuration...")
=======
		executionText := TextStyle.Render("Adding selected commands to configuration...")
>>>>>>> Stashed changes:internal/tui/housekeeping_model.go

		box := tui.BoxStyle.Width(60).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				m.spinner.View()+" "+executionText,
			),
		)

		content = lipgloss.JoinVertical(lipgloss.Left, title, "", box)

	case HKStateComplete:
		if m.err != nil {
			title := tui.ErrorStyle.Render(tui.IconCross + " ERROR")
			errorMsg := tui.ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err))

			errorBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(tui.ColorError).
				Padding(tui.DefaultPadding, tui.DefaultPadding*2).
				Width(60).
				Render(errorMsg)

			instructions := tui.HelpDescStyle.Margin(tui.ComponentGap, 0, 0, 0).Render("enter exit")
			content = lipgloss.JoinVertical(lipgloss.Left, title, "", errorBox, instructions)
		} else {
			title := tui.SuccessStyle.Render(tui.IconCheck + " COMPLETE")

			// Count how many categories were selected
			selectedCategories := []string{}
			for _, cat := range m.categories {
				if cat.Selected {
					selectedCategories = append(selectedCategories, cat.Name)
				}
			}

			categoryText := strings.Join(selectedCategories, " and ")
			successMsg := tui.SuccessStyle.Render(fmt.Sprintf("Successfully added %d commands for %s!", m.addedCount, categoryText))

			successBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(tui.ColorSuccess).
				Padding(tui.DefaultPadding, tui.DefaultPadding*2).
				Width(60).
				Render(successMsg)

			instructions := tui.HelpDescStyle.Margin(tui.ComponentGap, 0, 0, 0).Render("enter exit")
			content = lipgloss.JoinVertical(lipgloss.Left, title, "", successBox, instructions)
		}
	}

	// Add help view at the bottom
	m.help.ShowAll = m.showAll
	helpView := m.help.View(m.keys)
	helpText := tui.HelpStyle.Render(helpView)

	return lipgloss.JoinVertical(lipgloss.Left, content, helpText)
}
