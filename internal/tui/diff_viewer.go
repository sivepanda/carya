package tui

import (
	"carya/internal/chunk"
	"carya/internal/store"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DiffViewer states
const (
	StateChunkList = iota
	StateDiffView
)

// DiffViewerModel represents the Bubble Tea model for viewing diffs
type DiffViewerModel struct {
	help           help.Model
	keys           KeyMap
	state          int
	chunks         []chunk.Chunk
	cursor         int
	viewport       viewport.Model
	store          ChunkStore
	width          int
	height         int
	ready          bool
	err            error
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
		state:  StateChunkList,
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

		if m.state == StateDiffView {
			headerHeight := 8
			footerHeight := 4
			verticalMarginHeight := headerHeight + footerHeight

			if !m.ready {
				m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
				m.viewport.YPosition = headerHeight
				m.ready = true
			} else {
				m.viewport.Width = msg.Width
				m.viewport.Height = msg.Height - verticalMarginHeight
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			if m.state == StateDiffView {
				// Go back to chunk list
				m.state = StateChunkList
				m.ready = false
				return m, nil
			}
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.state == StateChunkList && m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, m.keys.Down):
			if m.state == StateChunkList && m.cursor < len(m.chunks)-1 {
				m.cursor++
			}

		case key.Matches(msg, m.keys.Enter):
			if m.state == StateChunkList && len(m.chunks) > 0 {
				m.state = StateDiffView
				m.ready = false
				return m, nil
			} else if m.state == StateDiffView {
				m.state = StateChunkList
				m.ready = false
				return m, nil
			}
		}
	}

	// Update viewport if in diff view
	if m.state == StateDiffView && m.ready {
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the model
func (m *DiffViewerModel) View() string {
	if m.err != nil {
		title := TitleStyle.Render("═══ ERROR ═══")
		errorText := TextStyle.Render(fmt.Sprintf("Error: %v\n\nPress 'q' to quit.", m.err))
		return lipgloss.JoinVertical(lipgloss.Center, title, errorText)
	}

	switch m.state {
	case StateChunkList:
		return m.renderChunkList()
	case StateDiffView:
		return m.renderDiffView()
	default:
		return ""
	}
}

// renderChunkList renders the list of chunks
func (m *DiffViewerModel) renderChunkList() string {
	title := TitleStyle.Render("═══ CHUNK HISTORY ═══")

	if len(m.chunks) == 0 {
		emptyText := TextStyle.Render("No chunks found. Start making changes to see them here!")
		instructions := HelpDescStyle.Render("\nPress 'q' to quit")
		return lipgloss.JoinVertical(lipgloss.Left, title, emptyText, instructions)
	}

	var items []string
	for i, c := range m.chunks {
		cursor := "  "
		if m.cursor == i {
			cursor = "> "
		}

		// Format the chunk info
		timeRange := fmt.Sprintf("%s → %s",
			c.StartTime.Format("15:04:05"),
			c.EndTime.Format("15:04:05"))

		manualTag := ""
		if c.Manual {
			manualTag = " [manual]"
		}

		line := fmt.Sprintf("%s%s  %s%s",
			cursor,
			c.FilePath,
			timeRange,
			manualTag)

		if m.cursor == i {
			line = SelectedItemStyle.Render(line)
		} else {
			line = ItemStyle.Render(line)
		}
		items = append(items, line)
	}

	// Limit visible items to fit screen
	visibleItems := items
	if len(items) > 15 {
		start := m.cursor - 7
		end := m.cursor + 8
		if start < 0 {
			start = 0
			end = 15
		} else if end > len(items) {
			end = len(items)
			start = end - 15
		}
		visibleItems = items[start:end]
	}

	itemsText := strings.Join(visibleItems, "\n")
	instructions := HelpDescStyle.Render(fmt.Sprintf(
		"\nShowing %d chunks | ↑/↓ to navigate, Enter to view, 'q' to quit",
		len(m.chunks)))

	return lipgloss.JoinVertical(lipgloss.Left, title, itemsText, instructions)
}

// renderDiffView renders the detailed diff view
func (m *DiffViewerModel) renderDiffView() string {
	if m.cursor >= len(m.chunks) {
		return "Invalid chunk selection"
	}

	c := m.chunks[m.cursor]

	// Create header
	title := TitleStyle.Render("═══ CHUNK DETAILS ═══")

	headerStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary).
		Padding(0, 2)

	header := headerStyle.Render(fmt.Sprintf(
		"File: %s\nTime: %s → %s\nID: %s\nHash: %s\nManual: %v",
		c.FilePath,
		c.StartTime.Format("2006-01-02 15:04:05"),
		c.EndTime.Format("2006-01-02 15:04:05"),
		c.ID,
		c.Hash,
		c.Manual,
	))

	// Format diff with syntax highlighting
	diffContent := m.formatDiff(c.Diff)

	if !m.ready {
		return lipgloss.JoinVertical(lipgloss.Left, title, header, diffContent)
	}

	m.viewport.SetContent(diffContent)

	instructions := HelpDescStyle.Render("\n↑/↓ to scroll, Enter/q to go back")

	return lipgloss.JoinVertical(lipgloss.Left, title, header, m.viewport.View(), instructions)
}

// formatDiff applies syntax highlighting to diff content
func (m *DiffViewerModel) formatDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var formatted []string

	// Style definitions for diff lines
	addedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	removedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	contextStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	headerStyle := lipgloss.NewStyle().Foreground(ColorTitle).Bold(true)

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+"):
			formatted = append(formatted, addedStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			formatted = append(formatted, removedStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			formatted = append(formatted, headerStyle.Render(line))
		case strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index"):
			formatted = append(formatted, headerStyle.Render(line))
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
