package model

import (
	"carya/internal/chunk"
	"carya/internal/store"
	"carya/internal/tui"
	"carya/internal/tui/shared"
	"fmt"
	"log"
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

// DiffViewer represents the Bubble Tea model for viewing diffs
// Uses a telescope-style split view: list on left, diff on right
type DiffViewer struct {
	help         help.Model
	keys         tui.KeyMap
	chunks       []chunk.Chunk
	cursor       int
	filtered     []int
	listViewport viewport.Model
	diffViewport viewport.Model
	search       textinput.Model
	searching    bool
	spinner      spinner.Model
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

	log.Printf("Loaded %d chunks", len(chunks))

	m := &DiffViewer{
		help:    h,
		keys:    tui.DefaultKeys(),
		chunks:  chunks,
		cursor:  0,
		store:   store,
		spinner: shared.NewDefaultSpinner(tui.ColorAccent),
		width:   80,
		height:  24,
	}
	search := textinput.New()
	search.Prompt = "search: "
	search.Placeholder = "type file path..."
	search.CharLimit = 200
	search.Width = 36
	m.search = search
	m.rebuildFilter()

	return m, nil
}

// Init initializes the model
func (m *DiffViewer) Init() tea.Cmd {
	return m.spinner.Tick
}

// LoadedChunksMsg indicates chunks have been loaded
type LoadedChunksMsg struct {
	Chunks []chunk.Chunk
	Error  error
}

// Update handles messages and updates the model
func (m *DiffViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case LoadedChunksMsg:
		if msg.Error != nil {
			m.err = msg.Error
			return m, nil
		}
		m.chunks = msg.Chunks
		m.rebuildFilter()
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
		if m.searching {
			switch msg.String() {
			case "esc", "enter":
				m.searching = false
				m.search.Blur()
				m.updateDiffContent()
				return m, nil
			}

			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.rebuildFilter()
			m.updateDiffContent()
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case msg.String() == "/":
			m.searching = true
			m.search.Focus()
			return m, textinput.Blink

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 && len(m.filtered) > 0 {
				m.cursor--
				m.updateDiffContent()
			}

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.filtered)-1 && len(m.filtered) > 0 {
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

	// Update spinner while loading
	if !m.ready {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
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
		loadingText := tui.TextStyle.Render(" Loading...")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), loadingText))
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
	searchHelp := tui.HelpKeyStyle.Render("/") + tui.HelpDescStyle.Render(" search")
	scrollHelp := tui.HelpKeyStyle.Render("ctrl+d/u") + tui.HelpDescStyle.Render(" scroll")
	quitHelp := tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit")
	counter := tui.SubtleTextStyle.Render(fmt.Sprintf("%d/%d", maxInt(m.cursor+1, 0), len(m.filtered)))
	if len(m.filtered) == 0 {
		counter = tui.SubtleTextStyle.Render("0/0")
	}

	searchBar := shared.RenderSearchBar(m.searching, m.search.View(), m.search.Value(), len(m.filtered), len(m.chunks), m.width)

	footer := lipgloss.NewStyle().
		Padding(0, 1).
		Render(navHelp + " • " + searchHelp + " • " + scrollHelp + " • " + quitHelp + " • " + counter)

	return lipgloss.JoinVertical(lipgloss.Left, searchBar, content, footer)
}

// renderChunkListPanel renders the left panel with chunk list
func (m *DiffViewer) renderChunkListPanel() string {
	var items []string
	for visibleIndex, idx := range m.filtered {
		c := m.chunks[idx]
		cursor := "  "
		if m.cursor == visibleIndex {
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

		if m.cursor == visibleIndex {
			line = tui.SelectedItemStyle.Render(line)
		} else {
			line = tui.ItemStyle.Render(line)
		}
		items = append(items, line)
	}

	m.listViewport.SetContent(strings.Join(items, "\n"))

	// Ensure selected item is visible
	shared.EnsureItemVisible(&m.listViewport, m.cursor)

	return shared.RenderTitledPanel("CHUNKS", m.listViewport.View(), m.listWidth, m.height, tui.ColorBorder)
}

// renderDiffPanel renders the right panel with diff content
func (m *DiffViewer) renderDiffPanel() string {
	if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
		return ""
	}

	c := m.chunks[m.filtered[m.cursor]]
	header := shared.RenderChunkHeader(c, tui.SubtleTextStyle, tui.TextStyle.Bold(true))
	content := lipgloss.JoinVertical(lipgloss.Left, header, m.diffViewport.View())
	return shared.RenderTitledPanel("DIFF", content, m.diffWidth, m.height, tui.ColorTitle)
}

// updateDiffContent updates the diff viewport with the current chunk's diff
func (m *DiffViewer) updateDiffContent() {
	if !m.ready {
		return
	}
	if len(m.filtered) == 0 {
		m.diffViewport.SetContent(tui.SubtleTextStyle.Render("No files match filter"))
		return
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.filtered) {
		return
	}

	c := m.chunks[m.filtered[m.cursor]]

	if len(c.Diff) == 0 {
		m.diffViewport.SetContent(tui.SubtleTextStyle.Render("No diff content available"))
		return
	}

	m.diffViewport.SetContent(chunk.FormatDiff(c.Diff))
	m.diffViewport.GotoTop()
}

func (m *DiffViewer) rebuildFilter() {
	paths := make([]string, len(m.chunks))
	for i, c := range m.chunks {
		paths[i] = c.FilePath
	}
	m.filtered = shared.BuildFilteredIndices(paths, m.search.Value())
	if len(m.filtered) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RunDiffViewer runs the diff viewer TUI
func RunDiffViewer(dataSourceName string) error {
	// Configure logger prefix for diff viewer context
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
