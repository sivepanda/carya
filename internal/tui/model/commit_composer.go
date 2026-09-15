package model

import (
	"carya/internal/chunk"
	"carya/internal/compose"
	"carya/internal/store"
	"carya/internal/tui"
	"carya/internal/tui/shared"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// CommitStatus represents different stages in the commit process
type CommitStatus int

const (
	StatusSelecting CommitStatus = iota
	StatusEditing
	StatusConfirming
	StatusCommitting
	StatusWarning // New state for showing warnings
	StatusApplyConflict
	StatusDone
	StatusError
)

// ChunkStore interface for retrieving chunks
type ChunkStore interface {
	GetRecentChunks(limit int) ([]chunk.Chunk, error)
	FindChunks(filePath string) ([]chunk.Chunk, error)
	UpdateChunkFeatureLabel(ids []chunk.ChunkID, label string) error
	ClearChunkFeatureLabel(ids []chunk.ChunkID) error
}

type composerRow struct {
	isFolder   bool
	label      string
	chunkIndex int
}

// CommitComposer represents the Bubble Tea model for selecting and committing diffs
type CommitComposer struct {
	help             help.Model
	keys             tui.KeyMap
	composer         *compose.Composer
	cursor           int
	listViewport     viewport.Model
	diffViewport     viewport.Model
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
	rows             []composerRow
	labelInput       textinput.Model
	editingLabel     bool
	statusLine       string
	applyConflict    applyConflictState
}

type applyConflictState struct {
	abortSelected bool
	totalSelected int
	applicable    map[int]bool
	skipped       []string
	commitMsg     string
}

// NewCommitComposer creates a new commit composer model
func NewCommitComposer(store ChunkStore, repoPath string) (*CommitComposer, error) {
	log.Println("Initializing commit composer")
	h := help.New()
	h.Styles.ShortDesc = tui.HelpDescStyle
	h.Styles.ShortKey = tui.HelpKeyStyle
	h.Styles.FullDesc = tui.HelpDescStyle
	h.Styles.FullKey = tui.HelpKeyStyle

	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(tui.ColorAccent)

	composerModel, err := compose.New(store, repoPath)
	if err != nil {
		log.Printf("Error initializing compose model: %v", err)
		return nil, err
	}

	// Setup commit message input
	ti := textinput.New()
	ti.Placeholder = "Type commit message here..."
	ti.CharLimit = 100
	ti.SetWidth(60)
	ti.Prompt = ""

	labelInput := textinput.New()
	labelInput.Placeholder = "Feature label (example: composer/navigation)"
	labelInput.CharLimit = 80
	labelInput.SetWidth(60)
	labelInput.Prompt = ""

	m := &CommitComposer{
		help:       h,
		keys:       tui.DefaultKeys(),
		composer:   composerModel,
		cursor:     0,
		width:      80,
		height:     24,
		commitMsg:  ti,
		labelInput: labelInput,
		status:     StatusSelecting,
		spinner:    s,
	}
	m.rebuildRows()

	return m, nil
}

// Init initializes the model
func (m *CommitComposer) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update handles messages and updates the model
func (m *CommitComposer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case applyConflictMsg:
		m.status = StatusApplyConflict
		m.applyConflict = applyConflictState{
			abortSelected: true,
			totalSelected: msg.totalSelected,
			applicable:    msg.applicable,
			skipped:       msg.skipped,
			commitMsg:     msg.commitMsg,
		}
		log.Println("Showing apply conflict choices to user")
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
	case StatusApplyConflict:
		return m.updateApplyConflict(msg)
	case StatusDone, StatusError:
		if msg, ok := msg.(tea.KeyMsg); ok {
			if key.Matches(msg, m.keys.Quit) || msg.String() == "enter" {
				return m, tea.Quit
			}
		}
	case StatusCommitting:
		if msg, ok := msg.(tea.KeyMsg); ok {
			if key.Matches(msg, m.keys.Quit) {
				return m, tea.Quit
			}
		}
		// Keep spinner animating
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// updateSelecting handles the chunk selection state
func (m *CommitComposer) updateSelecting(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate split view layout
		layout := shared.CalculateSplitViewLayout(msg.Width, msg.Height, 2, 3)
		m.listWidth = layout.ListWidth
		m.diffWidth = layout.DiffWidth

		if !m.ready {
			m.listViewport, m.diffViewport = shared.InitializeViewports(layout)
			m.ready = true
		} else {
			shared.UpdateViewportSizes(&m.listViewport, &m.diffViewport, layout)
		}

		if len(m.rows) > 0 {
			m.updateDiffContent()
		}

		return m, nil

	case tea.KeyMsg:
		if m.editingLabel {
			switch msg.String() {
			case "esc":
				m.editingLabel = false
				m.labelInput.Blur()
				m.statusLine = "label edit cancelled"
				return m, nil
			case "enter":
				label := strings.TrimSpace(m.labelInput.Value())
				if label == "" {
					m.statusLine = "feature label cannot be empty"
					return m, nil
				}
				updated := m.assignLabelToSelected(label)
				m.editingLabel = false
				m.labelInput.Blur()
				if updated == 0 {
					m.statusLine = "select at least one chunk first"
				} else {
					m.statusLine = fmt.Sprintf("assigned %d chunk(s) to %q", updated, label)
				}
				m.updateDiffContent()
				return m, nil
			}

			var cmd tea.Cmd
			m.labelInput, cmd = m.labelInput.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.updateDiffContent()
			}

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.updateDiffContent()
			}

		case key.Matches(msg, m.keys.Select):
			m.toggleCurrentSelection()
			m.updateDiffContent()

		case msg.String() == "ctrl+s":
			saved, err := m.persistPendingLabels()
			if err != nil {
				m.statusLine = fmt.Sprintf("save failed: %v", err)
			} else if saved == 0 {
				m.statusLine = "nothing to save"
			} else {
				m.statusLine = fmt.Sprintf("saved %d label update(s)", saved)
			}

		case msg.String() == "f":
			if m.selectedChunkCount() == 0 {
				m.statusLine = "select at least one chunk first"
				break
			}
			m.editingLabel = true
			m.labelInput.SetValue("")
			m.labelInput.Focus()
			m.statusLine = "type a feature label, then press enter"

		case msg.String() == "p":
			updated := m.clearLabelForSelected()
			if updated == 0 {
				m.statusLine = "select at least one chunk first"
			} else {
				m.statusLine = fmt.Sprintf("popped %d chunk(s) out of feature", updated)
				m.updateDiffContent()
			}

		case msg.String() == "enter":
			// Only proceed if at least one chunk is actually selected
			if m.selectedChunkCount() > 0 {
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

func (m *CommitComposer) rebuildRows() {
	type grouped struct {
		label   string
		indices []int
	}

	chunks := m.composer.Chunks()
	groups := map[string][]int{}
	for i, c := range chunks {
		label := strings.TrimSpace(c.FeatureLabel)
		groups[label] = append(groups[label], i)
	}

	var labels []string
	for label := range groups {
		if label != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)

	ordered := make([]grouped, 0, len(labels)+1)
	ordered = append(ordered, grouped{label: "", indices: groups[""]})
	for _, label := range labels {
		ordered = append(ordered, grouped{label: label, indices: groups[label]})
	}

	rows := make([]composerRow, 0, len(chunks)+len(ordered))
	for _, group := range ordered {
		rows = append(rows, composerRow{isFolder: true, label: group.label, chunkIndex: -1})
		for _, idx := range group.indices {
			rows = append(rows, composerRow{label: group.label, chunkIndex: idx})
		}
	}

	m.rows = rows
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
}

func (m *CommitComposer) selectedChunkCount() int {
	return m.composer.SelectedChunkCount()
}

func (m *CommitComposer) toggleCurrentSelection() {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}

	row := m.rows[m.cursor]
	if row.isFolder {
		indices := m.chunkIndicesForLabel(row.label)
		if len(indices) == 0 {
			return
		}

		allSelected := true
		for _, idx := range indices {
			if !m.composer.IsSelected(idx) {
				allSelected = false
				break
			}
		}

		for _, idx := range indices {
			m.composer.SetSelected(idx, !allSelected)
		}
		return
	}

	m.composer.ToggleSelection(row.chunkIndex)
}

func (m *CommitComposer) chunkIndicesForLabel(label string) []int {
	return m.composer.ChunkIndicesForLabel(label)
}

func (m *CommitComposer) assignLabelToSelected(label string) int {
	updated := m.composer.AssignLabelToSelected(label)
	if updated > 0 {
		m.rebuildRows()
	}
	return updated
}

func (m *CommitComposer) clearLabelForSelected() int {
	updated := m.composer.ClearLabelForSelected()
	if updated > 0 {
		m.rebuildRows()
	}
	return updated
}

func (m *CommitComposer) persistPendingLabels() (int, error) {
	return m.composer.PersistPendingLabels()
}

// updateEditing handles the commit message editing state
func (m *CommitComposer) updateEditing(msg tea.Msg) (tea.Model, tea.Cmd) {

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
func (m *CommitComposer) updateConfirming(msg tea.Msg) (tea.Model, tea.Cmd) {
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
func (m *CommitComposer) createCommit() tea.Msg {
	log.Println("Creating commit from selected diffs")
	result, err := m.composer.CreateCommit(m.commitMsg.Value())
	if err != nil {
		log.Printf("Error creating composed commit: %v", err)
		return errMsg{err}
	}

	if result.Warning != nil {
		return warningMsg{
			warnings:  result.Warning.Warnings,
			patch:     result.Warning.Patch,
			commitMsg: result.Warning.CommitMsg,
		}
	}

	if result.ApplyConflict != nil {
		return applyConflictMsg{
			totalSelected: result.ApplyConflict.TotalSelected,
			applicable:    result.ApplyConflict.Applicable,
			skipped:       result.ApplyConflict.Skipped,
			commitMsg:     result.ApplyConflict.CommitMsg,
		}
	}

	m.result = result.Output
	return successMsg{m.result}
}

func (m *CommitComposer) applyApplicablePatch() tea.Msg {
	log.Println("Applying patch for applicable chunks only")
	output, err := m.composer.ApplyApplicableCommit(m.applyConflict.applicable, m.applyConflict.commitMsg)
	if err != nil {
		return errMsg{err}
	}

	m.result = output
	return successMsg{m.result}
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

type applyConflictMsg struct {
	totalSelected int
	applicable    map[int]bool
	skipped       []string
	commitMsg     string
}

// successMsg represents a success message
type successMsg struct {
	output string
}

// View renders the model
func (m *CommitComposer) View() tea.View {
	var s string

	if m.err != nil {
		title := tui.ErrorStyle.Render("✗ ERROR")
		errorMsg := tui.ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err))

		errorBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tui.ColorError).
			Padding(1, 2).
			Width(60).
			Render(errorMsg)

		instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("q quit")
		s = lipgloss.JoinVertical(lipgloss.Center, title, "", errorBox, instructions)
	} else if !m.ready {
		title := tui.TitleStyle.Render("📋 LOADING CHUNKS")
		spinnerView := m.spinner.View()
		loadingText := tui.TextStyle.Render(" Loading chunks...")

		loadingContent := lipgloss.JoinVertical(lipgloss.Center,
			title,
			"",
			lipgloss.JoinHorizontal(lipgloss.Center, spinnerView, loadingText),
		)
		s = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, loadingContent)
	} else {
		switch m.status {
		case StatusSelecting:
			s = m.renderSelectionView()
		case StatusEditing:
			s = m.renderEditingView()
		case StatusConfirming:
			s = m.renderConfirmationView()
		case StatusWarning:
			s = m.renderWarningView()
		case StatusApplyConflict:
			s = m.renderApplyConflictView()
		case StatusCommitting:
			s = m.renderCommittingView()
		case StatusDone:
			s = m.renderDoneView()
		case StatusError:
			s = m.renderErrorView()
		default:
			s = "Unknown state"
		}
	}

	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

// renderSelectionView shows the chunk selection interface
func (m *CommitComposer) renderSelectionView() string {
	if len(m.composer.Chunks()) == 0 {
		title := tui.TitleStyle.Render("📋 COMMIT COMPOSER")
		emptyMsg := tui.SubtleTextStyle.Render("No chunks found")
		helpMsg := tui.TextStyle.Render("Start making changes to see them here!")

		emptyBox := tui.DimBoxStyle.Width(50).Align(lipgloss.Center).Render(
			lipgloss.JoinVertical(lipgloss.Center, emptyMsg, "", helpMsg),
		)

		instructions := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("q quit")

		content := lipgloss.JoinVertical(lipgloss.Center, title, "", emptyBox, instructions)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	selectedCount := m.selectedChunkCount()

	navHelp := tui.HelpKeyStyle.Render("↑/↓") + tui.HelpDescStyle.Render(" navigate")
	selectHelp := tui.HelpKeyStyle.Render("space") + tui.HelpDescStyle.Render(" select")
	labelHelp := tui.HelpKeyStyle.Render("f") + tui.HelpDescStyle.Render(" label")
	popHelp := tui.HelpKeyStyle.Render("p") + tui.HelpDescStyle.Render(" pop")
	saveHelp := tui.HelpKeyStyle.Render("ctrl+s") + tui.HelpDescStyle.Render(" save labels")
	continueHelp := tui.HelpKeyStyle.Render("enter") + tui.HelpDescStyle.Render(" continue")
	quitHelp := tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit")

	selectedInfo := ""
	if selectedCount > 0 {
		selectedInfo = tui.SuccessStyle.Render(fmt.Sprintf(" • %d selected", selectedCount))
	}
	dirtyInfo := ""
	if m.composer.DirtyLabels() {
		dirtyInfo = tui.WarningStyle.Render(" • unsaved labels")
	}
	statusInfo := ""
	if strings.TrimSpace(m.statusLine) != "" {
		statusInfo = "\n" + tui.SubtleTextStyle.Render(m.statusLine)
	}

	footer := lipgloss.NewStyle().
		Padding(0, 1).
		Render(navHelp + " • " + selectHelp + " • " + labelHelp + " • " + popHelp + " • " + saveHelp + " • " + continueHelp + " • " + quitHelp + selectedInfo + dirtyInfo + statusInfo)

	if m.editingLabel {
		promptHeader := tui.SubheaderStyle.Render("FEATURE LABEL")
		promptBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tui.ColorBorder).
			Padding(1, 2).
			Width(60).
			Render(m.labelInput.View())
		promptHelp := tui.HelpDescStyle.Render("enter apply • esc cancel")
		footer = lipgloss.JoinVertical(lipgloss.Left, footer, "", promptHeader, promptBox, promptHelp)
	}

	paneHeight := m.height - lipgloss.Height(footer)
	if paneHeight <= 0 {
		return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(footer)
	}

	// Render both panels
	listPanel := m.renderChunkListPanel(paneHeight)
	diffPanel := m.renderDiffPanel(paneHeight)

	// Join horizontally
	content := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, diffPanel)

	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(
		lipgloss.JoinVertical(lipgloss.Left, content, footer),
	)
}

// renderChunkListPanel renders the left panel with selectable chunk list
func (m *CommitComposer) renderChunkListPanel(height int) string {
	listViewportWidth, listViewportHeight := shared.TitledPanelViewportSize(m.listWidth, height, 0)
	m.listViewport.SetWidth(listViewportWidth)
	m.listViewport.SetHeight(listViewportHeight)
	chunks := m.composer.Chunks()

	var items []string
	for i, row := range m.rows {
		cursor := "  "
		if m.cursor == i {
			cursor = "❯ "
		}

		var line string
		if row.isFolder {
			label := row.label
			if label == "" {
				label = "Unassigned"
			}
			indices := m.chunkIndicesForLabel(row.label)
			selected := 0
			for _, idx := range indices {
				if m.composer.IsSelected(idx) {
					selected++
				}
			}

			checkBox := "[ ]"
			if selected == len(indices) && len(indices) > 0 {
				checkBox = "[✓]"
			} else if selected > 0 {
				checkBox = "[~]"
			}
			line = fmt.Sprintf("%s %s %s (%d)", cursor, checkBox, label, len(indices))
			line = tui.SubheaderStyle.Render(line)
		} else {
			chunkIdx := row.chunkIndex
			if chunkIdx < 0 || chunkIdx >= len(chunks) {
				continue
			}
			c := chunks[chunkIdx]
			checkBox := "[ ]"
			if m.composer.IsSelected(chunkIdx) {
				checkBox = "[✓]"
			}

			filename := filepath.Base(c.FilePath)
			if len(filename) > 20 {
				filename = filename[:17] + "..."
			}
			timeStr := tui.SubtleTextStyle.Render(c.StartTime.Format("15:04"))
			line = fmt.Sprintf("%s  %s %s %s", cursor, checkBox, filename, timeStr)
			if m.cursor == i {
				line = tui.SelectedItemStyle.Render(line)
			} else {
				line = tui.ItemStyle.Render(line)
			}
		}

		if row.isFolder && m.cursor == i {
			line = tui.SelectedItemStyle.Render(line)
		}

		items = append(items, line)
	}

	m.listViewport.SetContent(strings.Join(items, "\n"))

	// Ensure selected item is visible
	shared.EnsureItemVisible(&m.listViewport, m.cursor)

	return shared.RenderTitledPanel("CHUNKS", m.listViewport.View(), m.listWidth, height, tui.ColorBorder)
}

// renderDiffPanel renders the right panel with diff content
func (m *CommitComposer) renderDiffPanel(height int) string {
	chunks := m.composer.Chunks()
	if m.cursor >= len(m.rows) || m.cursor < 0 {
		empty := tui.SubtleTextStyle.Render("No chunk selected")
		diffViewportWidth, diffViewportHeight := shared.TitledPanelViewportSize(m.diffWidth, height, 0)
		m.diffViewport.SetWidth(diffViewportWidth)
		m.diffViewport.SetHeight(diffViewportHeight)
		m.diffViewport.SetContent(empty)
		return shared.RenderTitledPanel("DIFF", m.diffViewport.View(), m.diffWidth, height, tui.ColorTitle)
	}

	row := m.rows[m.cursor]
	if row.isFolder {
		label := row.label
		if label == "" {
			label = "Unassigned"
		}
		header := tui.TextStyle.Bold(true).Render("Feature folder: " + label)
		diffViewportWidth, diffViewportHeight := shared.TitledPanelViewportSize(m.diffWidth, height, lipgloss.Height(header))
		m.diffViewport.SetWidth(diffViewportWidth)
		m.diffViewport.SetHeight(diffViewportHeight)
		return shared.RenderTitledPanel("DIFF", lipgloss.JoinVertical(lipgloss.Left, header, m.diffViewport.View()), m.diffWidth, height, tui.ColorTitle)
	}

	c := chunks[row.chunkIndex]
	header := shared.RenderChunkHeader(c, tui.SubtleTextStyle, tui.TextStyle.Bold(true))
	diffViewportWidth, diffViewportHeight := shared.TitledPanelViewportSize(m.diffWidth, height, lipgloss.Height(header))
	m.diffViewport.SetWidth(diffViewportWidth)
	m.diffViewport.SetHeight(diffViewportHeight)
	return shared.RenderTitledPanel("DIFF", lipgloss.JoinVertical(lipgloss.Left, header, m.diffViewport.View()), m.diffWidth, height, tui.ColorTitle)
}

// updateDiffContent updates the diff viewport with the current chunk's diff
func (m *CommitComposer) updateDiffContent() {
	chunks := m.composer.Chunks()
	if m.cursor >= len(m.rows) || !m.ready || m.cursor < 0 {
		return
	}

	row := m.rows[m.cursor]
	if row.isFolder {
		indices := m.chunkIndicesForLabel(row.label)
		label := row.label
		if label == "" {
			label = "Unassigned"
		}
		selected := 0
		files := make([]string, 0, len(indices))
		for _, idx := range indices {
			if idx < 0 || idx >= len(chunks) {
				continue
			}
			if m.composer.IsSelected(idx) {
				selected++
			}
			files = append(files, "- "+chunks[idx].FilePath)
		}
		if len(files) > 8 {
			files = append(files[:8], fmt.Sprintf("- ... and %d more", len(files)-8))
		}
		summary := fmt.Sprintf("%s\n\nChunks: %d\nSelected: %d\n\nFiles:\n%s", label, len(indices), selected, strings.Join(files, "\n"))
		m.diffViewport.SetContent(summary)
		m.diffViewport.GotoTop()
		return
	}

	c := chunks[row.chunkIndex]
	m.diffViewport.SetContent(chunk.FormatDiff(c.Diff))
	m.diffViewport.GotoTop()
}

// renderEditingView shows the commit message editing interface
func (m *CommitComposer) renderEditingView() string {
	title := tui.TitleStyle.Render("✏️  COMMIT MESSAGE")

	selectedCount := m.selectedChunkCount()

	// List selected files
	chunks := m.composer.Chunks()
	var selectedFiles []string
	for i, chunk := range chunks {
		if m.composer.IsSelected(i) {
			selectedFiles = append(selectedFiles, "  "+filepath.Base(chunk.FilePath))
			if len(selectedFiles) > 5 {
				selectedFiles = append(selectedFiles, fmt.Sprintf("  ... and %d more files", selectedCount-5))
				break
			}
		}
	}

	filesHeader := tui.SubheaderStyle.Render(fmt.Sprintf("SELECTED FILES (%d):", selectedCount))
	filesContent := tui.SubtleTextStyle.Render(strings.Join(selectedFiles, "\n"))

	// Input field
	inputHeader := tui.SubheaderStyle.Render("COMMIT MESSAGE:")

	// Style the input
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorBorder).
		Padding(1, 2).
		Width(60)

	inputBox := inputStyle.Render(m.commitMsg.View())

	// Instructions
	escHelp := tui.HelpKeyStyle.Render("esc") + tui.HelpDescStyle.Render(" back")
	enterHelp := tui.HelpKeyStyle.Render("enter") + tui.HelpDescStyle.Render(" continue")
	quitHelp := tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit")

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
func (m *CommitComposer) updateWarning(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *CommitComposer) updateApplyConflict(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "up", "k", "down", "j":
			m.applyConflict.abortSelected = !m.applyConflict.abortSelected
			return m, nil
		case "enter":
			if m.applyConflict.abortSelected {
				m.status = StatusSelecting
				m.statusLine = "commit aborted due to stale chunk conflicts"
				return m, nil
			}

			m.status = StatusCommitting
			return m, m.applyApplicablePatch
		case "esc":
			m.status = StatusSelecting
			m.statusLine = "commit aborted"
			return m, nil
		case "q", "Q":
			return m, tea.Quit
		}
	}

	return m, nil
}

// applyPendingPatch applies the previously generated patch after warning confirmation
func (m *CommitComposer) applyPendingPatch() tea.Msg {
	log.Println("Applying patch after warning confirmation")
	output, err := m.composer.ApplyPatchAndCommit(m.pendingPatch, m.pendingCommitMsg)
	if err != nil {
		log.Printf("Error applying pending patch: %v", err)
		return errMsg{err}
	}

	// Success - return the git output
	log.Println("Commit created successfully")
	m.result = output
	return successMsg{m.result}
}

// renderConfirmationView shows the confirmation dialog
func (m *CommitComposer) renderConfirmationView() string {
	title := tui.TitleStyle.Render("❓ CONFIRM COMMIT")

	selectedCount := m.selectedChunkCount()

	message := fmt.Sprintf("Commit %d changes with message:", selectedCount)

	// Style the confirmation box
	confirmStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorWarning).
		Padding(1, 2).
		Width(60)

	// Format the commit message
	commitMsg := tui.TextStyle.Bold(true).Render("\"" + m.commitMsg.Value() + "\"")

	confirmBox := confirmStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Center,
			tui.SubtleTextStyle.Render(message),
			"",
			commitMsg,
			"",
			tui.TextStyle.Render("Are you sure? (y/n)"),
		),
	)

	// Instructions
	yesHelp := tui.HelpKeyStyle.Render("y") + tui.HelpDescStyle.Render(" yes")
	noHelp := tui.HelpKeyStyle.Render("n") + tui.HelpDescStyle.Render(" no")
	quitHelp := tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit")

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
func (m *CommitComposer) renderWarningView() string {
	title := tui.WarningStyle.Render("⚠ CORRUPT CONTENT DETECTED")

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

	warningText := tui.SubtleTextStyle.Render(strings.Join(warningLines, "\n"))

	message := tui.TextStyle.Render("Corrupt or invalid content was detected and cleaned from the diff.")
	question := tui.TextStyle.Bold(true).Render("Proceed with the cleaned version?")

	// Style the warning box
	warningStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorWarning).
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
			tui.TextStyle.Render("Press (y) to proceed or (n) to cancel"),
		),
	)

	// Instructions
	yesHelp := tui.HelpKeyStyle.Render("y") + tui.HelpDescStyle.Render(" proceed")
	noHelp := tui.HelpKeyStyle.Render("n") + tui.HelpDescStyle.Render(" cancel")
	quitHelp := tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit")

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

func (m *CommitComposer) renderApplyConflictView() string {
	title := tui.WarningStyle.Render("⚠ APPLY CONFLICTS DETECTED")

	applicableCount := len(m.applyConflict.applicable)
	skippedCount := len(m.applyConflict.skipped)

	lines := []string{
		fmt.Sprintf("Selected chunks: %d", m.applyConflict.totalSelected),
		fmt.Sprintf("Applicable now: %d", applicableCount),
		fmt.Sprintf("Conflicting now: %d", skippedCount),
		"",
		"Some selected chunks no longer apply cleanly.",
	}

	if skippedCount > 0 {
		lines = append(lines, "")
		for i, skipped := range m.applyConflict.skipped {
			if i >= 5 {
				lines = append(lines, fmt.Sprintf("  • ... and %d more", skippedCount-5))
				break
			}
			lines = append(lines, "  • "+skipped)
		}
	}

	abortOption := "  Abort whole commit"
	continueOption := "  Commit only applicable chunks"
	if m.applyConflict.abortSelected {
		abortOption = tui.SelectedItemStyle.Render("❯ Abort whole commit")
		continueOption = tui.ItemStyle.Render("  Commit only applicable chunks")
	} else {
		abortOption = tui.ItemStyle.Render("  Abort whole commit")
		continueOption = tui.SelectedItemStyle.Render("❯ Commit only applicable chunks")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorWarning).
		Padding(1, 2).
		Width(78).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				tui.TextStyle.Render(strings.Join(lines, "\n")),
				"",
				tui.TextStyle.Bold(true).Render("How should compose proceed?"),
				"",
				abortOption,
				continueOption,
			),
		)

	footer := lipgloss.NewStyle().
		Padding(1, 1).
		Render(
			tui.HelpKeyStyle.Render("↑/↓") + tui.HelpDescStyle.Render(" choose") +
				" • " + tui.HelpKeyStyle.Render("enter") + tui.HelpDescStyle.Render(" confirm") +
				" • " + tui.HelpKeyStyle.Render("esc") + tui.HelpDescStyle.Render(" cancel") +
				" • " + tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit"),
		)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		"",
		box,
		"",
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderCommittingView shows the commit in progress view
func (m *CommitComposer) renderCommittingView() string {
	title := tui.TitleStyle.Render("⏳ CREATING COMMIT")

	spinnerView := m.spinner.View()
	loadingText := tui.TextStyle.Render(" Creating commit, please wait...")

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
func (m *CommitComposer) renderDoneView() string {
	title := tui.SuccessStyle.Render("✓ COMMIT CREATED")

	resultStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorSuccess).
		Padding(1, 2).
		Width(60)

	resultBox := resultStyle.Render(m.result)

	footer := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("Press Enter or q to quit")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", resultBox, "", footer)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderErrorView shows the error view
func (m *CommitComposer) renderErrorView() string {
	title := tui.ErrorStyle.Render("✗ ERROR")
	errorMsg := tui.TextStyle.Render(m.err.Error())

	errorBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorError).
		Padding(1, 2).
		Width(60).
		Render(errorMsg)

	footer := tui.HelpDescStyle.Margin(1, 0, 0, 0).Render("Press Enter or q to quit")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", errorBox, "", footer)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// RunCommitComposer runs the commit composer TUI
func RunCommitComposer(dataSourceName, repoPath string) error {
	log.Println("Starting commit composer with database:", dataSourceName)
	store, err := store.NewSQLiteStore(dataSourceName)
	if err != nil {
		log.Printf("Error opening store: %v", err)
		return fmt.Errorf("failed to open store: %w", err)
	}
	log.Println("Successfully opened chunk store")
	defer store.Close()

	model, err := NewCommitComposer(store, repoPath)
	if err != nil {
		log.Printf("Error creating commit composer model: %v", err)
		return err
	}
	log.Println("Successfully created commit composer model")

	log.Println("Starting commit composer UI")
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		log.Printf("Error running commit composer UI: %v", err)
		return fmt.Errorf("error running commit composer: %w", err)
	}
	log.Println("Commit composer UI exited successfully")

	return nil
}
