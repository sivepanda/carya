package model

import (
	"carya/internal/chunk"
	"carya/internal/repository"
	"carya/internal/store"
<<<<<<< Updated upstream:internal/tui/model/diff_viewer.go
	"carya/internal/tui"
=======
	"carya/internal/tui/shared"
>>>>>>> Stashed changes:internal/tui/diff_viewer.go
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DiffViewer represents the Bubble Tea model for viewing diffs
// Uses a telescope-style split view: list on left, diff on right
type DiffViewer struct {
	help         help.Model
	keys         tui.KeyMap
	chunks       []chunk.Chunk
	cursor       int
	listViewport viewport.Model
	diffViewport viewport.Model
	store        ChunkStore
	width        int
	height       int
	ready        bool
	err          error
	listWidth    int
	diffWidth    int
}

// NewDiffViewer creates a new diff viewer model
func NewDiffViewer(store ChunkStore) (*DiffViewer, error) {
	h := help.New()
	h.Styles.ShortDesc = tui.HelpDescStyle
	h.Styles.ShortKey = tui.HelpKeyStyle
	h.Styles.FullDesc = tui.HelpDescStyle
	h.Styles.FullKey = tui.HelpKeyStyle

	// Load recent chunks
	chunks, err := store.GetRecentChunks(100)
	if err != nil {
		log.Printf("Failed to load chunks: %v", err)
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}

	// Log information about loaded chunks
	log.Printf("Loaded %d chunks", len(chunks))
	for i, c := range chunks {
		log.Printf("Chunk %d: ID=%s, FilePath=%s, DiffLength=%d",
			i, c.ID, c.FilePath, len(c.Diff))
		if len(c.Diff) == 0 {
			log.Printf("WARNING: Chunk %d has empty diff content", i)
		}
	}

	m := &DiffViewer{
		help:   h,
		keys:   tui.DefaultKeys(),
		chunks: chunks,
		cursor: 0,
		store:  store,
		width:  80,
		height: 24,
	}

	return m, nil
}

// Init initializes the model
func (m *DiffViewer) Init() tea.Cmd {
	return nil
}

// LoadedChunksMsg indicates chunks have been loaded
type LoadedChunksMsg struct {
	Chunks []chunk.Chunk
	Error  error
}

// Update handles messages and updates the model
func (m *DiffViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case LoadedChunksMsg:
		if msg.Error != nil {
			m.err = msg.Error
			return m, nil
		}
		m.chunks = msg.Chunks
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate split view layout
		layout := shared.CalculateSplitViewLayout(msg.Width, msg.Height, 2, 2)
		m.listWidth = layout.ListWidth
		m.diffWidth = layout.DiffWidth

		if !m.ready {
			m.listViewport, m.diffViewport = shared.InitializeViewports(layout)
			m.ready = true
		} else {
			shared.UpdateViewportSizes(&m.listViewport, &m.diffViewport, layout)
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

		// Allow scrolling the diff with Ctrl+d and Ctrl+u
		case msg.String() == "ctrl+d":
			m.diffViewport.ViewDown()
		case msg.String() == "ctrl+u":
			m.diffViewport.ViewUp()
		}
	}

	return m, cmd
}

// View renders the model
func (m *DiffViewer) View() string {
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
		spinner := tui.SubtleTextStyle.Render("◐")
		loadingText := tui.TextStyle.Render("  Loading...")
		return lipgloss.JoinVertical(lipgloss.Center, spinner+" "+loadingText)
	}

	return m.renderSplitView()
}

// renderSplitView renders the telescope-style split view
func (m *DiffViewer) renderSplitView() string {
	if len(m.chunks) == 0 {
		title := tui.TitleStyle.Render("📋 CHUNK VIEWER")
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

	// Add footer with better formatting
	navHelp := tui.HelpKeyStyle.Render("↑/↓") + tui.HelpDescStyle.Render(" navigate")
	scrollHelp := tui.HelpKeyStyle.Render("ctrl+d/u") + tui.HelpDescStyle.Render(" scroll")
	quitHelp := tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit")
	counter := tui.SubtleTextStyle.Render(fmt.Sprintf("%d/%d", m.cursor+1, len(m.chunks)))

	footer := lipgloss.NewStyle().
		Padding(0, 1).
		Render(navHelp + " • " + scrollHelp + " • " + quitHelp + " • " + counter)

	return lipgloss.JoinVertical(lipgloss.Left, content, footer)
}

// renderChunkListPanel renders the left panel with chunk list
func (m *DiffViewer) renderChunkListPanel() string {
	title := tui.HeaderStyle.Padding(1, 2).Render("📋 CHUNKS")

	var items []string
	for i, c := range m.chunks {
		cursor := "  "
		if m.cursor == i {
			cursor = "❯ "
		}

		// Format filename
		filename := filepath.Base(c.FilePath)
		if len(filename) > 25 {
			filename = filename[:22] + "..."
		}

		// Format time
		timeStr := tui.SubtleTextStyle.Render(c.StartTime.Format("15:04"))

		line := cursor + filename + " " + timeStr

		if m.cursor == i {
			line = tui.SelectedItemStyle.Render(line)
		} else {
			line = tui.ItemStyle.Render(line)
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
func (m *DiffViewer) renderDiffPanel() string {
	if m.cursor >= len(m.chunks) {
		return ""
	}

	c := m.chunks[m.cursor]
<<<<<<< Updated upstream:internal/tui/model/diff_viewer.go

	// Create header with chunk info
	fileLabel := tui.SubtleTextStyle.Render("File:")
	filePath := tui.TextStyle.Bold(true).Render(c.FilePath)
	timeLabel := tui.SubtleTextStyle.Render("Time:")
	timeRange := tui.TextStyle.Render(fmt.Sprintf("%s → %s",
		c.StartTime.Format("15:04:05"),
		c.EndTime.Format("15:04:05")))

	header := lipgloss.NewStyle().
		Padding(1, 2).
		Render(fileLabel + " " + filePath + "  " + timeLabel + " " + timeRange)

	diffStyle := lipgloss.NewStyle().
		Width(m.diffWidth).
		Height(m.height).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(tui.ColorTitle).
		Padding(0, 1)

	return diffStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, m.diffViewport.View()))
=======
	header := shared.RenderChunkHeader(c, SubtleTextStyle, TextStyle.Bold(true))
	return shared.RenderDiffPanel(header, m.diffViewport.View(), m.diffWidth, m.height, ColorTitle)
>>>>>>> Stashed changes:internal/tui/diff_viewer.go
}

// updateDiffContent updates the diff viewport with the current chunk's diff
func (m *DiffViewer) updateDiffContent() {
	if m.cursor >= len(m.chunks) || !m.ready {
		return
	}

	c := m.chunks[m.cursor]

	// Debug logging to check if diff content exists
	diffLength := len(c.Diff)
	if diffLength == 0 {
		log.Printf("WARNING: Empty diff content for chunk %s", c.ID)
		m.diffViewport.SetContent(lipgloss.NewStyle().
			Foreground(tui.ColorError).
			Bold(true).
			Render("WARNING: Diff content is empty"))
		return
	}

	// Log the raw diff content for debugging
	log.Printf("Displaying diff for chunk %s (file: %s, length: %d)",
		c.ID, c.FilePath, diffLength)
	log.Printf("Raw diff content:\n%s", c.Diff)

	// Format the diff content with syntax highlighting
<<<<<<< Updated upstream:internal/tui/model/diff_viewer.go
	diffContent := m.formatDiff(c.Diff)
=======
	diffContent := chunk.FormatDiff(c.Diff)
>>>>>>> Stashed changes:internal/tui/diff_viewer.go

	// Set the content in the viewport
	m.diffViewport.SetContent(diffContent)
	m.diffViewport.GotoTop()
}

<<<<<<< Updated upstream:internal/tui/model/diff_viewer.go
// formatDiff applies syntax highlighting to diff content
func (m *DiffViewer) formatDiff(diff string) string {
	// Check if this is a binary file message
	if strings.HasPrefix(diff, "Binary file ") {
		binaryStyle := lipgloss.NewStyle().
			Foreground(tui.ColorWarning).
			Bold(true)
		infoStyle := lipgloss.NewStyle().
			Foreground(tui.ColorTertiary)

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

	// If the diff is empty, show a message
	if strings.TrimSpace(diff) == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Render("No changes detected")
	}

	lines := strings.Split(diff, "\n")
	var formatted []string

	// Style definitions for diff lines - using our color palette
	addedStyle := lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(false)
	removedStyle := lipgloss.NewStyle().Foreground(tui.ColorError).Bold(false)
	contextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc")) // Make context lines more visible
	headerStyle := lipgloss.NewStyle().Foreground(tui.ColorAccent).Bold(true)
	rangeStyle := lipgloss.NewStyle().Foreground(tui.ColorWarning).Bold(true)

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
			formatted = append(formatted, tui.SubtleTextStyle.Render(line))
		case strings.HasPrefix(line, "File:") || strings.HasPrefix(line, "Time:") || strings.HasPrefix(line, "Hash:"):
			formatted = append(formatted, contextStyle.Render(line))
		default:
			// Make context lines more visible with explicit color
			formatted = append(formatted, contextStyle.Render(line))
		}
	}

	return strings.Join(formatted, "\n")
}

=======
>>>>>>> Stashed changes:internal/tui/diff_viewer.go
// RunDiffViewer runs the diff viewer TUI
func RunDiffViewer(dataSourceName string) error {
	// Setup logging to the repo log file
	repo, err := repository.New()
	if err != nil {
		return fmt.Errorf("failed to initialize repository: %w", err)
	}

	// Open log file for appending
	logFile, err := os.OpenFile(repo.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	// Configure logger
	log.SetOutput(logFile)
	log.SetPrefix("DiffViewer: ")
	log.SetFlags(log.Ldate | log.Ltime)
	log.Println("Starting diff viewer")

	// Initialize store
	store, err := store.NewSQLiteStore(dataSourceName)
	if err != nil {
		log.Printf("Error opening store: %v", err)
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer store.Close()

	// Create model
	model, err := NewDiffViewer(store)
	if err != nil {
		log.Printf("Error creating model: %v", err)
		return err
	}

	// Run the program
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Printf("Error running program: %v", err)
		return fmt.Errorf("error running diff viewer: %w", err)
	}

	log.Println("Diff viewer closed")
	return nil
}
