package tui

import (
	"carya/internal/chunk"
	"carya/internal/store"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CommitStatus represents different stages in the commit process
type CommitStatus int

const (
	StatusSelecting CommitStatus = iota
	StatusEditing
	StatusConfirming
	StatusCommitting
	StatusWarning // New state for showing warnings
	StatusDone
	StatusError
)

// CommitComposerModel represents the Bubble Tea model for selecting and committing diffs
type CommitComposerModel struct {
	help             help.Model
	keys             KeyMap
	chunks           []chunk.Chunk
	selectedChunks   map[int]bool
	cursor           int
	listViewport     viewport.Model
	diffViewport     viewport.Model
	store            ChunkStore
	width            int
	height           int
	ready            bool
	err              error
	listWidth        int
	diffWidth        int
	commitMsg        textinput.Model
	status           CommitStatus
	result           string
	spinner          spinner.Model
	pendingWarnings  []string // Store warnings for the warning view
	pendingPatch     string   // Store patch for applying after confirmation
	pendingCommitMsg string   // Store commit message for applying after confirmation
}

// NewCommitComposerModel creates a new commit composer model
func NewCommitComposerModel(store ChunkStore) (*CommitComposerModel, error) {
	log.Println("Initializing commit composer")
	h := help.New()
	h.Styles.ShortDesc = HelpDescStyle
	h.Styles.ShortKey = HelpKeyStyle
	h.Styles.FullDesc = HelpDescStyle
	h.Styles.FullKey = HelpKeyStyle

	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorAccent)

	// Load recent chunks
	log.Println("Loading recent chunks for commit composition")
	chunks, err := store.GetRecentChunks(100)
	if err != nil {
		log.Printf("Error loading chunks: %v", err)
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}
	log.Printf("Loaded %d chunks for commit composition", len(chunks))

	// Setup commit message input
	ti := textinput.New()
	ti.Placeholder = "Type commit message here..."
	ti.CharLimit = 100
	ti.Width = 60
	ti.Prompt = ""

	m := &CommitComposerModel{
		help:           h,
		keys:           DefaultKeys(),
		chunks:         chunks,
		selectedChunks: make(map[int]bool),
		cursor:         0,
		store:          store,
		width:          80,
		height:         24,
		commitMsg:      ti,
		status:         StatusSelecting,
		spinner:        s,
	}

	return m, nil
}

// Init initializes the model
func (m *CommitComposerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update handles messages and updates the model
func (m *CommitComposerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle success/error messages from commit process
	switch msg := msg.(type) {
	case errMsg:
		m.err = msg.err
		m.status = StatusError
		log.Printf("Commit error: %v", m.err)
		return m, nil
	case successMsg:
		m.status = StatusDone
		m.result = msg.output
		log.Printf("Commit successful: %s", m.result)
		return m, nil
	case warningMsg:
		// Store warning info for rendering
		m.status = StatusWarning
		m.pendingWarnings = msg.warnings
		m.pendingPatch = msg.patch
		m.pendingCommitMsg = msg.commitMsg
		log.Println("Showing corruption warning to user")
		return m, nil
	}

	// Handle different states
	switch m.status {
	case StatusSelecting:
		return m.updateSelecting(msg)
	case StatusEditing:
		return m.updateEditing(msg)
	case StatusConfirming:
		return m.updateConfirming(msg)
	case StatusWarning:
		return m.updateWarning(msg)
	case StatusCommitting, StatusDone, StatusError:
		// For these states, just handle quit
		if msg, ok := msg.(tea.KeyMsg); ok {
			if key.Matches(msg, m.keys.Quit) {
				return m, tea.Quit
			}
			if msg.String() == "enter" && (m.status == StatusDone || m.status == StatusError) {
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

// updateSelecting handles the chunk selection state
func (m *CommitComposerModel) updateSelecting(msg tea.Msg) (tea.Model, tea.Cmd) {

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Split width: 40% for list, 60% for diff
		m.listWidth = int(float64(msg.Width) * 0.4)
		m.diffWidth = msg.Width - m.listWidth

		headerHeight := 2
		footerHeight := 3
		contentHeight := msg.Height - headerHeight - footerHeight

		if !m.ready {
			m.listViewport = viewport.New(m.listWidth-2, contentHeight)
			m.diffViewport = viewport.New(m.diffWidth-2, contentHeight)
			m.ready = true
		} else {
			m.listViewport.Width = m.listWidth - 2
			m.listViewport.Height = contentHeight
			m.diffViewport.Width = m.diffWidth - 2
			m.diffViewport.Height = contentHeight
		}

		// Update diff content if chunks exist
		if len(m.chunks) > 0 && m.cursor < len(m.chunks) {
			m.updateDiffContent()
		}

		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.updateDiffContent()
			}

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.chunks)-1 {
				m.cursor++
				m.updateDiffContent()
			}

		case key.Matches(msg, m.keys.Select):
			// Toggle selection for the current chunk
			if len(m.chunks) > 0 {
				m.selectedChunks[m.cursor] = !m.selectedChunks[m.cursor]
			}

		case msg.String() == "enter":
			// Only proceed if at least one chunk is selected
			if len(m.selectedChunks) > 0 {
				m.status = StatusEditing
				m.commitMsg.Focus()
				return m, textinput.Blink
			}

		// Allow scrolling the diff with Ctrl+d and Ctrl+u
		case msg.String() == "ctrl+d":
			m.diffViewport.PageDown()
		case msg.String() == "ctrl+u":
			m.diffViewport.PageUp()
		}
	}

	return m, nil
}

// updateEditing handles the commit message editing state
func (m *CommitComposerModel) updateEditing(msg tea.Msg) (tea.Model, tea.Cmd) {

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case msg.String() == "esc":
			// Go back to selection
			m.status = StatusSelecting
			m.commitMsg.Blur()
			return m, nil

		case msg.String() == "enter":
			// Only proceed if message isn't empty
			if strings.TrimSpace(m.commitMsg.Value()) != "" {
				m.status = StatusConfirming
				return m, nil
			}
		}
	}

	// Handle text input
	var tiCmd tea.Cmd
	m.commitMsg, tiCmd = m.commitMsg.Update(msg)
	return m, tiCmd
}

// updateConfirming handles the confirmation state
func (m *CommitComposerModel) updateConfirming(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			// Proceed with commit
			m.status = StatusCommitting
			return m, m.createCommit
		case "n", "N", "esc":
			// Go back to editing
			m.status = StatusEditing
			return m, textinput.Blink
		case "q", "Q":
			return m, tea.Quit
		}
	}

	return m, nil
}

// createCommit performs the git operations to create a commit from selected diffs
func (m *CommitComposerModel) createCommit() tea.Msg {
	log.Println("Creating commit from selected diffs")

	// First, create a patch from the selected diffs
	log.Println("Creating patch from selected diffs")
	patch, cleanupWarnings := m.createPatchFromSelectedDiffs()

	log.Printf(patch)

	// Check if we have a valid patch
	if len(patch) == 0 {
		log.Println("No valid patches to apply")
		return errMsg{fmt.Errorf("no valid patches to apply")}
	}

	log.Printf("Created patch with %d bytes", len(patch))

	// Log any cleanup warnings
	if len(cleanupWarnings) > 0 {
		log.Println("Warning: Potential corrupt content was detected and cleaned:")
		for _, warning := range cleanupWarnings {
			log.Println(warning)
		}

		// If we're in confirm mode and there were warnings, return with the warning
		if len(cleanupWarnings) > 0 {
			log.Println("Returning corruption warning to user")
			return warningMsg{
				warnings:  cleanupWarnings,
				patch:     patch,
				commitMsg: m.commitMsg.Value(),
			}
		}
	}

	// Apply the patch
	log.Println("Applying patch to git index")
	applyCmd := exec.Command("git", "apply", "--index", "-")
	applyCmd.Stdin = strings.NewReader(patch)

	if output, err := applyCmd.CombinedOutput(); err != nil {
		log.Printf("Error applying patch: %v\n%s", err, output)
		return errMsg{fmt.Errorf("failed to apply patch: %w\n%s", err, output)}
	}
	log.Println("Patch applied successfully")

	// Create the commit
	log.Printf("Creating git commit with message: %s", m.commitMsg.Value())
	commitCmd := exec.Command("git", "commit", "-m", m.commitMsg.Value())
	if output, err := commitCmd.CombinedOutput(); err != nil {
		log.Printf("Error creating commit: %v\n%s", err, output)
		return errMsg{fmt.Errorf("failed to create commit: %w\n%s", err, output)}
	} else {
		// Success - return the git output
		log.Println("Commit created successfully")
		m.result = string(output)
		return successMsg{m.result}
	}
}

// createPatchFromSelectedDiffs combines all selected diffs into a single patch
func (m *CommitComposerModel) createPatchFromSelectedDiffs() (string, []string) {
	log.Println("Combining selected diffs into a unified patch")
	var patches []string
	var warnings []string

	for i, chunk := range m.chunks {
		if selected, ok := m.selectedChunks[i]; ok && selected {
			log.Printf("Adding diff for file: %s to patch", chunk.FilePath)
			// Clean up the diff to make it applicable by git
			cleanDiff, diffWarnings := m.cleanupDiffForGit(chunk)
			if len(diffWarnings) > 0 {
				for _, w := range diffWarnings {
					warnings = append(warnings, fmt.Sprintf("%s: %s", chunk.FilePath, w))
				}
			}
			if cleanDiff != "" {
				patches = append(patches, cleanDiff)
			}
		}
	}

	if len(patches) == 0 {
		return "", warnings
	}
	return strings.Join(patches, ""), warnings
}

// cleanupDiffForGit prepares a diff for use with git apply
func (m *CommitComposerModel) cleanupDiffForGit(c chunk.Chunk) (string, []string) {
	diff := c.Diff
	var warnings []string

	// Skip binary files
	if strings.HasPrefix(diff, "Binary file ") {
		log.Printf("Skipping binary file: %s", c.FilePath)
		return "", nil
	}

	// Ensure the diff ends with a newline for proper git apply
	if !strings.HasSuffix(diff, "\n") {
		diff = diff + "\n"
	}

	// Check for null bytes which would corrupt the patch
	if strings.Contains(diff, "\x00") {
		warning := "Diff contains null bytes (file may be binary)"
		log.Printf("Warning: %s for file %s", warning, c.FilePath)
		warnings = append(warnings, warning)
		return "", warnings
	}

	// Normalize line endings
	diff = strings.ReplaceAll(diff, "\r\n", "\n")

	return diff, warnings
}

// errMsg represents an error message
type errMsg struct {
	err error
}

func (e errMsg) Error() string { return e.err.Error() }

// warningMsg represents a warning message that requires user confirmation
type warningMsg struct {
	warnings  []string
	patch     string
	commitMsg string
}

// successMsg represents a success message
type successMsg struct {
	output string
}

// View renders the model
func (m *CommitComposerModel) View() string {
	if m.err != nil {
		title := ErrorStyle.Render("✗ ERROR")
		errorMsg := ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err))

		errorBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorError).
			Padding(1, 2).
			Width(60).
			Render(errorMsg)

		instructions := HelpDescStyle.Margin(1, 0, 0, 0).Render("q quit")
		return lipgloss.JoinVertical(lipgloss.Center, title, "", errorBox, instructions)
	}

	if !m.ready {
		title := TitleStyle.Render("📋 LOADING CHUNKS")
		spinnerView := m.spinner.View()
		loadingText := TextStyle.Render(" Loading chunks...")

		loadingContent := lipgloss.JoinVertical(lipgloss.Center,
			title,
			"",
			lipgloss.JoinHorizontal(lipgloss.Center, spinnerView, loadingText),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, loadingContent)
	}

	switch m.status {
	case StatusSelecting:
		return m.renderSelectionView()
	case StatusEditing:
		return m.renderEditingView()
	case StatusConfirming:
		return m.renderConfirmationView()
	case StatusWarning:
		return m.renderWarningView()
	case StatusCommitting:
		return m.renderCommittingView()
	case StatusDone:
		return m.renderDoneView()
	case StatusError:
		return m.renderErrorView()
	default:
		return "Unknown state"
	}
}

// renderSelectionView shows the chunk selection interface
func (m *CommitComposerModel) renderSelectionView() string {
	if len(m.chunks) == 0 {
		title := TitleStyle.Render("📋 COMMIT COMPOSER")
		emptyMsg := SubtleTextStyle.Render("No chunks found")
		helpMsg := TextStyle.Render("Start making changes to see them here!")

		emptyBox := DimBoxStyle.Width(50).Align(lipgloss.Center).Render(
			lipgloss.JoinVertical(lipgloss.Center, emptyMsg, "", helpMsg),
		)

		instructions := HelpDescStyle.Margin(1, 0, 0, 0).Render("q quit")

		content := lipgloss.JoinVertical(lipgloss.Center, title, "", emptyBox, instructions)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	// Render both panels
	listPanel := m.renderChunkListPanel()
	diffPanel := m.renderDiffPanel()

	// Join horizontally
	content := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, diffPanel)

	// Add footer with better formatting
	selectedCount := 0
	for _, selected := range m.selectedChunks {
		if selected {
			selectedCount++
		}
	}

	navHelp := HelpKeyStyle.Render("↑/↓") + HelpDescStyle.Render(" navigate")
	selectHelp := HelpKeyStyle.Render("space") + HelpDescStyle.Render(" select")
	continueHelp := HelpKeyStyle.Render("enter") + HelpDescStyle.Render(" continue")
	quitHelp := HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")

	selectedInfo := ""
	if selectedCount > 0 {
		selectedInfo = SuccessStyle.Render(fmt.Sprintf(" • %d selected", selectedCount))
	}

	footer := lipgloss.NewStyle().
		Padding(0, 1).
		Render(navHelp + " • " + selectHelp + " • " + continueHelp + " • " + quitHelp + selectedInfo)

	return lipgloss.JoinVertical(lipgloss.Left, content, footer)
}

// renderChunkListPanel renders the left panel with selectable chunk list
func (m *CommitComposerModel) renderChunkListPanel() string {
	title := HeaderStyle.Padding(1, 2).Render("📋 SELECT DIFFS")

	var items []string
	for i, c := range m.chunks {
		// Determine if this chunk is selected
		checkBox := " [ ] "
		if selected, ok := m.selectedChunks[i]; ok && selected {
			checkBox = " [✓] "
		}

		cursor := "  "
		if m.cursor == i {
			cursor = "❯ "
		}

		// Format filename
		filename := filepath.Base(c.FilePath)
		if len(filename) > 20 {
			filename = filename[:17] + "..."
		}

		// Format time
		timeStr := SubtleTextStyle.Render(c.StartTime.Format("15:04"))

		line := cursor + checkBox + filename + " " + timeStr

		if m.cursor == i {
			line = SelectedItemStyle.Render(line)
		} else {
			line = ItemStyle.Render(line)
		}
		items = append(items, line)
	}

	m.listViewport.SetContent(strings.Join(items, "\n"))

	// Ensure selected item is visible
	if m.cursor < m.listViewport.YOffset {
		m.listViewport.YOffset = m.cursor
	} else if m.cursor >= m.listViewport.YOffset+m.listViewport.Height {
		m.listViewport.YOffset = m.cursor - m.listViewport.Height + 1
	}

	listStyle := lipgloss.NewStyle().
		Width(m.listWidth).
		Height(m.height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1)

	return listStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, m.listViewport.View()))
}

// renderDiffPanel renders the right panel with diff content
func (m *CommitComposerModel) renderDiffPanel() string {
	if m.cursor >= len(m.chunks) {
		return ""
	}

	c := m.chunks[m.cursor]

	// Create header with chunk info
	fileLabel := SubtleTextStyle.Render("File:")
	filePath := TextStyle.Bold(true).Render(c.FilePath)
	timeLabel := SubtleTextStyle.Render("Time:")
	timeRange := TextStyle.Render(fmt.Sprintf("%s → %s",
		c.StartTime.Format("15:04:05"),
		c.EndTime.Format("15:04:05")))

	header := lipgloss.NewStyle().
		Padding(1, 2).
		Render(fileLabel + " " + filePath + "  " + timeLabel + " " + timeRange)

	diffStyle := lipgloss.NewStyle().
		Width(m.diffWidth).
		Height(m.height).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorTitle).
		Padding(0, 1)

	return diffStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, m.diffViewport.View()))
}

// updateDiffContent updates the diff viewport with the current chunk's diff
func (m *CommitComposerModel) updateDiffContent() {
	if m.cursor >= len(m.chunks) || !m.ready {
		return
	}

	c := m.chunks[m.cursor]
	diffContent := m.formatDiff(c.Diff)
	m.diffViewport.SetContent(diffContent)
	m.diffViewport.GotoTop()
}

// formatDiff applies syntax highlighting to diff content (same as in DiffViewer)
func (m *CommitComposerModel) formatDiff(diff string) string {
	// Check if this is a binary file message
	if strings.HasPrefix(diff, "Binary file ") {
		binaryStyle := lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)
		infoStyle := lipgloss.NewStyle().
			Foreground(ColorTertiary)

		lines := strings.Split(diff, "\n")
		var formatted []string
		for i, line := range lines {
			if i == 0 {
				formatted = append(formatted, binaryStyle.Render("⚠ "+line))
			} else if strings.TrimSpace(line) != "" {
				formatted = append(formatted, infoStyle.Render("  "+line))
			}
		}
		return strings.Join(formatted, "\n")
	}

	lines := strings.Split(diff, "\n")
	var formatted []string

	// Style definitions for diff lines - using our color palette
	addedStyle := lipgloss.NewStyle().Foreground(ColorSuccess).Bold(false)
	removedStyle := lipgloss.NewStyle().Foreground(ColorError).Bold(false)
	contextStyle := lipgloss.NewStyle().Foreground(ColorTertiary)
	headerStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	rangeStyle := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			// File headers in diff
			formatted = append(formatted, headerStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			formatted = append(formatted, addedStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			formatted = append(formatted, removedStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			formatted = append(formatted, rangeStyle.Render(line))
		case strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index"):
			formatted = append(formatted, SubtleTextStyle.Render(line))
		case strings.HasPrefix(line, "File:") || strings.HasPrefix(line, "Time:") || strings.HasPrefix(line, "Hash:"):
			formatted = append(formatted, contextStyle.Render(line))
		default:
			formatted = append(formatted, TextStyle.Render(line))
		}
	}

	return strings.Join(formatted, "\n")
}

// renderEditingView shows the commit message editing interface
func (m *CommitComposerModel) renderEditingView() string {
	title := TitleStyle.Render("✏️  COMMIT MESSAGE")

	// Count selected chunks
	selectedCount := 0
	for _, selected := range m.selectedChunks {
		if selected {
			selectedCount++
		}
	}

	// List selected files
	var selectedFiles []string
	for i, chunk := range m.chunks {
		if selected, ok := m.selectedChunks[i]; ok && selected {
			selectedFiles = append(selectedFiles, "  "+filepath.Base(chunk.FilePath))
			if len(selectedFiles) > 5 {
				selectedFiles = append(selectedFiles, fmt.Sprintf("  ... and %d more files", selectedCount-5))
				break
			}
		}
	}

	filesHeader := SubheaderStyle.Render(fmt.Sprintf("SELECTED FILES (%d):", selectedCount))
	filesContent := SubtleTextStyle.Render(strings.Join(selectedFiles, "\n"))

	// Input field
	inputHeader := SubheaderStyle.Render("COMMIT MESSAGE:")

	// Style the input
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(1, 2).
		Width(60)

	inputBox := inputStyle.Render(m.commitMsg.View())

	// Instructions
	escHelp := HelpKeyStyle.Render("esc") + HelpDescStyle.Render(" back")
	enterHelp := HelpKeyStyle.Render("enter") + HelpDescStyle.Render(" continue")
	quitHelp := HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")

	footer := lipgloss.NewStyle().
		Padding(1, 1).
		Render(escHelp + " • " + enterHelp + " • " + quitHelp)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		filesHeader,
		filesContent,
		"",
		inputHeader,
		inputBox,
		"",
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// updateWarning handles the warning confirmation state
func (m *CommitComposerModel) updateWarning(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "y", "Y":
			// Proceed with commit despite warnings
			log.Println("User confirmed to proceed despite warnings")
			m.status = StatusCommitting
			return m, m.applyPendingPatch
		case "n", "N", "esc":
			// Cancel the operation
			log.Println("User canceled the operation due to warnings")
			m.status = StatusSelecting
			return m, nil
		case "q", "Q":
			return m, tea.Quit
		}
	}

	return m, nil
}

// applyPendingPatch applies the previously generated patch after warning confirmation
func (m *CommitComposerModel) applyPendingPatch() tea.Msg {
	log.Println("Applying patch after warning confirmation")

	// Apply the patch
	applyCmd := exec.Command("git", "apply", "--cached", "-")
	applyCmd.Stdin = strings.NewReader(m.pendingPatch)

	if output, err := applyCmd.CombinedOutput(); err != nil {
		log.Printf("Error applying patch: %v\n%s", err, output)
		return errMsg{fmt.Errorf("failed to apply patch: %w\n%s", err, output)}
	}
	log.Println("Patch applied successfully")

	// Create the commit
	log.Printf("Creating git commit with message: %s", m.pendingCommitMsg)
	commitCmd := exec.Command("git", "commit", "-m", m.pendingCommitMsg)
	if output, err := commitCmd.CombinedOutput(); err != nil {
		log.Printf("Error creating commit: %v\n%s", err, output)
		return errMsg{fmt.Errorf("failed to create commit: %w\n%s", err, output)}
	} else {
		// Success - return the git output
		log.Println("Commit created successfully")
		m.result = string(output)
		return successMsg{m.result}
	}
}

// renderConfirmationView shows the confirmation dialog
func (m *CommitComposerModel) renderConfirmationView() string {
	title := TitleStyle.Render("❓ CONFIRM COMMIT")

	// Count selected chunks
	selectedCount := 0
	for _, selected := range m.selectedChunks {
		if selected {
			selectedCount++
		}
	}

	message := fmt.Sprintf("Commit %d changes with message:", selectedCount)

	// Style the confirmation box
	confirmStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorWarning).
		Padding(1, 2).
		Width(60)

	// Format the commit message
	commitMsg := TextStyle.Bold(true).Render("\"" + m.commitMsg.Value() + "\"")

	confirmBox := confirmStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Center,
			SubtleTextStyle.Render(message),
			"",
			commitMsg,
			"",
			TextStyle.Render("Are you sure? (y/n)"),
		),
	)

	// Instructions
	yesHelp := HelpKeyStyle.Render("y") + HelpDescStyle.Render(" yes")
	noHelp := HelpKeyStyle.Render("n") + HelpDescStyle.Render(" no")
	quitHelp := HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")

	footer := lipgloss.NewStyle().
		Padding(1, 1).
		Render(yesHelp + " • " + noHelp + " • " + quitHelp)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		"",
		confirmBox,
		"",
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderWarningView shows the warning confirmation dialog
func (m *CommitComposerModel) renderWarningView() string {
	title := WarningStyle.Render("⚠ CORRUPT CONTENT DETECTED")

	// Format the warnings for display
	var warningLines []string
	for i, warning := range m.pendingWarnings {
		if i < 5 {
			warningLines = append(warningLines, "  • "+warning)
		} else {
			warningLines = append(warningLines, fmt.Sprintf("  • ... and %d more issues", len(m.pendingWarnings)-5))
			break
		}
	}

	warningText := SubtleTextStyle.Render(strings.Join(warningLines, "\n"))

	message := TextStyle.Render("Corrupt or invalid content was detected and cleaned from the diff.")
	question := TextStyle.Bold(true).Render("Proceed with the cleaned version?")

	// Style the warning box
	warningStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorWarning).
		Padding(1, 2).
		Width(70)

	warningBox := warningStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			message,
			"",
			warningText,
			"",
			question,
			"",
			TextStyle.Render("Press (y) to proceed or (n) to cancel"),
		),
	)

	// Instructions
	yesHelp := HelpKeyStyle.Render("y") + HelpDescStyle.Render(" proceed")
	noHelp := HelpKeyStyle.Render("n") + HelpDescStyle.Render(" cancel")
	quitHelp := HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")

	footer := lipgloss.NewStyle().
		Padding(1, 1).
		Render(yesHelp + " • " + noHelp + " • " + quitHelp)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		"",
		warningBox,
		"",
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderCommittingView shows the commit in progress view
func (m *CommitComposerModel) renderCommittingView() string {
	title := TitleStyle.Render("⏳ CREATING COMMIT")

	spinnerView := m.spinner.View()
	loadingText := TextStyle.Render(" Creating commit, please wait...")

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center,
			title,
			"",
			lipgloss.JoinHorizontal(lipgloss.Center, spinnerView, loadingText),
		),
	)
}

// renderDoneView shows the success view
func (m *CommitComposerModel) renderDoneView() string {
	title := SuccessStyle.Render("✓ COMMIT CREATED")

	resultStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSuccess).
		Padding(1, 2).
		Width(60)

	resultBox := resultStyle.Render(m.result)

	footer := HelpDescStyle.Margin(1, 0, 0, 0).Render("Press Enter or q to quit")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", resultBox, "", footer)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderErrorView shows the error view
func (m *CommitComposerModel) renderErrorView() string {
	title := ErrorStyle.Render("✗ ERROR")
	errorMsg := TextStyle.Render(m.err.Error())

	errorBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorError).
		Padding(1, 2).
		Width(60).
		Render(errorMsg)

	footer := HelpDescStyle.Margin(1, 0, 0, 0).Render("Press Enter or q to quit")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", errorBox, "", footer)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// RunCommitComposer runs the commit composer TUI
func RunCommitComposer(dataSourceName string) error {
	log.Println("Starting commit composer with database:", dataSourceName)
	store, err := store.NewSQLiteStore(dataSourceName)
	if err != nil {
		log.Printf("Error opening store: %v", err)
		return fmt.Errorf("failed to open store: %w", err)
	}
	log.Println("Successfully opened chunk store")
	defer store.Close()

	model, err := NewCommitComposerModel(store)
	if err != nil {
		log.Printf("Error creating commit composer model: %v", err)
		return err
	}
	log.Println("Successfully created commit composer model")

	log.Println("Starting commit composer UI")
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Printf("Error running commit composer UI: %v", err)
		return fmt.Errorf("error running commit composer: %w", err)
	}
	log.Println("Commit composer UI exited successfully")

	return nil
}
