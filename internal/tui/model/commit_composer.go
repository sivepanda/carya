package model

import (
	"carya/internal/chunk"
	"carya/internal/patch"
	"carya/internal/store"
	"carya/internal/tui"
	"carya/internal/tui/shared"
	"fmt"
	"log"
	"path/filepath"
	"sort"
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
	rows             []composerRow
	labelInput       textinput.Model
	editingLabel     bool
	pendingLabels    map[chunk.ChunkID]string
	dirtyLabels      bool
	statusLine       string
}

// NewCommitComposer creates a new commit composer model
func NewCommitComposer(store ChunkStore) (*CommitComposer, error) {
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

	labelInput := textinput.New()
	labelInput.Placeholder = "Feature label (example: composer/navigation)"
	labelInput.CharLimit = 80
	labelInput.Width = 60
	labelInput.Prompt = ""

	m := &CommitComposer{
		help:           h,
		keys:           tui.DefaultKeys(),
		chunks:         chunks,
		selectedChunks: make(map[int]bool),
		cursor:         0,
		store:          store,
		width:          80,
		height:         24,
		commitMsg:      ti,
		labelInput:     labelInput,
		status:         StatusSelecting,
		spinner:        s,
		pendingLabels:  make(map[chunk.ChunkID]string),
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

	groups := map[string][]int{}
	for i, c := range m.chunks {
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

	rows := make([]composerRow, 0, len(m.chunks)+len(ordered))
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
	count := 0
	for _, selected := range m.selectedChunks {
		if selected {
			count++
		}
	}
	return count
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
			if !m.selectedChunks[idx] {
				allSelected = false
				break
			}
		}

		for _, idx := range indices {
			m.selectedChunks[idx] = !allSelected
		}
		return
	}

	m.selectedChunks[row.chunkIndex] = !m.selectedChunks[row.chunkIndex]
}

func (m *CommitComposer) chunkIndicesForLabel(label string) []int {
	indices := make([]int, 0)
	for i, c := range m.chunks {
		if strings.TrimSpace(c.FeatureLabel) == strings.TrimSpace(label) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *CommitComposer) selectedChunkIndices() []int {
	indices := make([]int, 0)
	for i, selected := range m.selectedChunks {
		if selected {
			indices = append(indices, i)
		}
	}
	sort.Ints(indices)
	return indices
}

func (m *CommitComposer) assignLabelToSelected(label string) int {
	label = strings.TrimSpace(label)
	if label == "" {
		return 0
	}

	updated := 0
	for _, idx := range m.selectedChunkIndices() {
		if idx < 0 || idx >= len(m.chunks) {
			continue
		}
		m.chunks[idx].FeatureLabel = label
		m.pendingLabels[m.chunks[idx].ID] = label
		updated++
	}

	if updated > 0 {
		m.dirtyLabels = true
		m.rebuildRows()
	}

	return updated
}

func (m *CommitComposer) clearLabelForSelected() int {
	updated := 0
	for _, idx := range m.selectedChunkIndices() {
		if idx < 0 || idx >= len(m.chunks) {
			continue
		}
		if m.chunks[idx].FeatureLabel == "" {
			continue
		}
		m.chunks[idx].FeatureLabel = ""
		m.pendingLabels[m.chunks[idx].ID] = ""
		updated++
	}

	if updated > 0 {
		m.dirtyLabels = true
		m.rebuildRows()
	}

	return updated
}

func (m *CommitComposer) persistPendingLabels() (int, error) {
	if len(m.pendingLabels) == 0 {
		m.dirtyLabels = false
		return 0, nil
	}

	idsByLabel := make(map[string][]chunk.ChunkID)
	for id, label := range m.pendingLabels {
		idsByLabel[label] = append(idsByLabel[label], id)
	}

	for label, ids := range idsByLabel {
		if strings.TrimSpace(label) == "" {
			if err := m.store.ClearChunkFeatureLabel(ids); err != nil {
				return 0, err
			}
			continue
		}

		if err := m.store.UpdateChunkFeatureLabel(ids, label); err != nil {
			return 0, err
		}
	}

	saved := len(m.pendingLabels)
	m.pendingLabels = make(map[chunk.ChunkID]string)
	m.dirtyLabels = false
	return saved, nil
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
	if _, err := m.persistPendingLabels(); err != nil {
		log.Printf("Error saving feature labels: %v", err)
		return errMsg{fmt.Errorf("failed to save feature labels: %w", err)}
	}

	// Create a patch from the selected diffs using the patch package
	log.Println("Creating patch from selected diffs")
	result := patch.CreateFromChunks(m.chunks, m.selectedChunks)

	log.Printf("%s", result.Patch)

	// Check if we have a valid patch
	if len(result.Patch) == 0 {
		log.Println("No valid patches to apply")
		return errMsg{fmt.Errorf("no valid patches to apply")}
	}

	log.Printf("Created patch with %d bytes", len(result.Patch))

	// Log any cleanup warnings
	if len(result.Warnings) > 0 {
		log.Println("Warning: Potential corrupt content was detected and cleaned:")
		for _, warning := range result.Warnings {
			log.Println(warning)
		}

		// If we're in confirm mode and there were warnings, return with the warning
		log.Println("Returning corruption warning to user")
		return warningMsg{
			warnings:  result.Warnings,
			patch:     result.Patch,
			commitMsg: m.commitMsg.Value(),
		}
	}

	// Apply the patch using the patch package
	log.Println("Applying patch to git index")
	if err := patch.Apply(result.Patch); err != nil {
		log.Printf("Error applying patch: %v", err)
		return errMsg{err}
	}
	log.Println("Patch applied successfully")

	// Create the commit using the patch package
	log.Printf("Creating git commit with message: %s", m.commitMsg.Value())
	output, err := patch.Commit(m.commitMsg.Value())
	if err != nil {
		log.Printf("Error creating commit: %v", err)
		return errMsg{err}
	}

	// Success - return the git output
	log.Println("Commit created successfully")
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

// successMsg represents a success message
type successMsg struct {
	output string
}

// View renders the model
func (m *CommitComposer) View() string {
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
		return lipgloss.JoinVertical(lipgloss.Center, title, "", errorBox, instructions)
	}

	if !m.ready {
		title := tui.TitleStyle.Render("📋 LOADING CHUNKS")
		spinnerView := m.spinner.View()
		loadingText := tui.TextStyle.Render(" Loading chunks...")

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
func (m *CommitComposer) renderSelectionView() string {
	if len(m.chunks) == 0 {
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

	// Render both panels
	listPanel := m.renderChunkListPanel()
	diffPanel := m.renderDiffPanel()

	// Join horizontally
	content := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, diffPanel)

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
	if m.dirtyLabels {
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

	return lipgloss.JoinVertical(lipgloss.Left, content, footer)
}

// renderChunkListPanel renders the left panel with selectable chunk list
func (m *CommitComposer) renderChunkListPanel() string {
	title := tui.HeaderStyle.Padding(1, 2).Render("📋 SELECT FEATURES & DIFFS")

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
				if m.selectedChunks[idx] {
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
			if chunkIdx < 0 || chunkIdx >= len(m.chunks) {
				continue
			}
			c := m.chunks[chunkIdx]
			checkBox := "[ ]"
			if m.selectedChunks[chunkIdx] {
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

	listStyle := lipgloss.NewStyle().
		Width(m.listWidth).
		Height(m.height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorBorder).
		Padding(0, 1)

	return listStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, m.listViewport.View()))
}

// renderDiffPanel renders the right panel with diff content
func (m *CommitComposer) renderDiffPanel() string {
	if m.cursor >= len(m.rows) || m.cursor < 0 {
		return ""
	}

	row := m.rows[m.cursor]
	if row.isFolder {
		label := row.label
		if label == "" {
			label = "Unassigned"
		}
		header := tui.TextStyle.Bold(true).Render("Feature folder: " + label)
		return shared.RenderDiffPanel(header, m.diffViewport.View(), m.diffWidth, m.height, tui.ColorTitle)
	}

	c := m.chunks[row.chunkIndex]
	header := shared.RenderChunkHeader(c, tui.SubtleTextStyle, tui.TextStyle.Bold(true))
	return shared.RenderDiffPanel(header, m.diffViewport.View(), m.diffWidth, m.height, tui.ColorTitle)
}

// updateDiffContent updates the diff viewport with the current chunk's diff
func (m *CommitComposer) updateDiffContent() {
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
			if idx < 0 || idx >= len(m.chunks) {
				continue
			}
			if m.selectedChunks[idx] {
				selected++
			}
			files = append(files, "- "+m.chunks[idx].FilePath)
		}
		if len(files) > 8 {
			files = append(files[:8], fmt.Sprintf("- ... and %d more", len(files)-8))
		}
		summary := fmt.Sprintf("%s\n\nChunks: %d\nSelected: %d\n\nFiles:\n%s", label, len(indices), selected, strings.Join(files, "\n"))
		m.diffViewport.SetContent(summary)
		m.diffViewport.GotoTop()
		return
	}

	c := m.chunks[row.chunkIndex]
	m.diffViewport.SetContent(chunk.FormatDiff(c.Diff))
	m.diffViewport.GotoTop()
}

// renderEditingView shows the commit message editing interface
func (m *CommitComposer) renderEditingView() string {
	title := tui.TitleStyle.Render("✏️  COMMIT MESSAGE")

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

// applyPendingPatch applies the previously generated patch after warning confirmation
func (m *CommitComposer) applyPendingPatch() tea.Msg {
	log.Println("Applying patch after warning confirmation")

	// Apply the patch using the patch package
	if err := patch.Apply(m.pendingPatch); err != nil {
		log.Printf("Error applying patch: %v", err)
		return errMsg{err}
	}
	log.Println("Patch applied successfully")

	// Create the commit using the patch package
	log.Printf("Creating git commit with message: %s", m.pendingCommitMsg)
	output, err := patch.Commit(m.pendingCommitMsg)
	if err != nil {
		log.Printf("Error creating commit: %v", err)
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
func RunCommitComposer(dataSourceName string) error {
	log.Println("Starting commit composer with database:", dataSourceName)
	store, err := store.NewSQLiteStore(dataSourceName)
	if err != nil {
		log.Printf("Error opening store: %v", err)
		return fmt.Errorf("failed to open store: %w", err)
	}
	log.Println("Successfully opened chunk store")
	defer store.Close()

	model, err := NewCommitComposer(store)
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
