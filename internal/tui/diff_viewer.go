package tui

import (
	"carya/internal/chunk"
	"carya/internal/store"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DiffViewerModel represents the Bubble Tea model for viewing diffs
// Uses a telescope-style split view: list on left, diff on right
type DiffViewerModel struct {
	help           help.Model
	keys           KeyMap
	chunks         []chunk.Chunk
	cursor         int
	listViewport   viewport.Model
	diffViewport   viewport.Model
	store          ChunkStore
	width          int
	height         int
	ready          bool
	err            error
	listWidth      int
	diffWidth      int
}

// ChunkStore interface for retrieving chunks
type ChunkStore interface {
	GetRecentChunks(limit int) ([]chunk.Chunk, error)
	FindChunks(filePath string) ([]chunk.Chunk, error)
}

// NewDiffViewerModel creates a new diff viewer model
func NewDiffViewerModel(store ChunkStore) (*DiffViewerModel, error) {
	h := help.New()
	h.Styles.ShortDesc = HelpDescStyle
	h.Styles.ShortKey = HelpKeyStyle
	h.Styles.FullDesc = HelpDescStyle
	h.Styles.FullKey = HelpKeyStyle

	// Load recent chunks
	chunks, err := store.GetRecentChunks(100)
	if err != nil {
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}

	m := &DiffViewerModel{
		help:   h,
		keys:   DefaultKeys(),
		chunks: chunks,
		cursor: 0,
		store:  store,
		width:  80,
		height: 24,
	}

	return m, nil
}

// Init initializes the model
func (m *DiffViewerModel) Init() tea.Cmd {
	return nil
}

// LoadedChunksMsg indicates chunks have been loaded
type LoadedChunksMsg struct {
	Chunks []chunk.Chunk
	Error  error
}

// Update handles messages and updates the model
func (m *DiffViewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

		// Split width: 40% for list, 60% for diff
		m.listWidth = int(float64(msg.Width) * 0.4)
		m.diffWidth = msg.Width - m.listWidth

		headerHeight := 2
		footerHeight := 2
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
func (m *DiffViewerModel) View() string {
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
		spinner := SubtleTextStyle.Render("◐")
		loadingText := TextStyle.Render("  Loading...")
		return lipgloss.JoinVertical(lipgloss.Center, spinner+" "+loadingText)
	}

	return m.renderSplitView()
}

// renderSplitView renders the telescope-style split view
func (m *DiffViewerModel) renderSplitView() string {
	if len(m.chunks) == 0 {
		title := TitleStyle.Render("📋 CHUNK VIEWER")
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
	navHelp := HelpKeyStyle.Render("↑/↓") + HelpDescStyle.Render(" navigate")
	scrollHelp := HelpKeyStyle.Render("ctrl+d/u") + HelpDescStyle.Render(" scroll")
	quitHelp := HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")
	counter := SubtleTextStyle.Render(fmt.Sprintf("%d/%d", m.cursor+1, len(m.chunks)))

	footer := lipgloss.NewStyle().
		Padding(0, 1).
		Render(navHelp + " • " + scrollHelp + " • " + quitHelp + " • " + counter)

	return lipgloss.JoinVertical(lipgloss.Left, content, footer)
}

// renderChunkListPanel renders the left panel with chunk list
func (m *DiffViewerModel) renderChunkListPanel() string {
	title := HeaderStyle.Padding(1, 2).Render("📋 CHUNKS")

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
		timeStr := SubtleTextStyle.Render(c.StartTime.Format("15:04"))

		line := cursor + filename + " " + timeStr

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
func (m *DiffViewerModel) renderDiffPanel() string {
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
func (m *DiffViewerModel) updateDiffContent() {
	if m.cursor >= len(m.chunks) || !m.ready {
		return
	}

	c := m.chunks[m.cursor]
	diffContent := m.formatDiff(c.Diff)
	m.diffViewport.SetContent(diffContent)
	m.diffViewport.GotoTop()
}

// formatDiff applies syntax highlighting to diff content
func (m *DiffViewerModel) formatDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var formatted []string

	// Style definitions for diff lines - using our new color palette
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

// RunDiffViewer runs the diff viewer TUI
func RunDiffViewer(dataSourceName string) error {
	store, err := store.NewSQLiteStore(dataSourceName)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer store.Close()

	model, err := NewDiffViewerModel(store)
	if err != nil {
		return err
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running diff viewer: %w", err)
	}

	return nil
}
