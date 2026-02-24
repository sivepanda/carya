package model

import (
	"carya/internal/repository"
	"carya/internal/tui"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	primitives "github.com/sivepanda/mycelia/tui"
)

// Housekeeping is a thin controller that drives mycelia's Setup primitive.
// It owns all key dispatch and delegates state/rendering to the primitive.
type Housekeeping struct {
	keys     tui.KeyMap
	help     help.Model
	showAll  bool
	setup    primitives.Setup
	width    int
	height   int
}

// NewHousekeeping creates a new housekeeping model.
// It performs carya-specific repo initialization synchronously (fast),
// then delegates the setup wizard to mycelia's primitive.
func NewHousekeeping() Housekeeping {
	// Carya-specific: ensure .carya directory exists and is gitignored
	if repo, err := repository.New(); err == nil {
		_ = repo.EnsureExists()
		_ = repo.EnsureGitignore()
	}

	h := help.New()
	h.Styles.ShortDesc = tui.HelpDescStyle
	h.Styles.ShortKey = tui.HelpKeyStyle
	h.Styles.FullDesc = tui.HelpDescStyle
	h.Styles.FullKey = tui.HelpKeyStyle

	return Housekeeping{
		keys:   tui.DefaultKeys(),
		help:   h,
		setup:  primitives.NewSetup(),
		width:  80,
		height: 24,
	}
}

// Init initializes the model by starting the setup primitive.
func (m Housekeeping) Init() tea.Cmd {
	return m.setup.Init()
}

// Update handles all messages. Keys are dispatched via imperative methods;
// everything else is forwarded to the primitive's Update/Tick.
func (m Housekeeping) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.setup = m.setup.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward non-key messages to the primitive for async results + spinner
	var cmd tea.Cmd
	m.setup, cmd = m.setup.Tick(msg)
	var cmd2 tea.Cmd
	m.setup, cmd2 = m.setup.Update(msg)
	return m, tea.Batch(cmd, cmd2)
}

func (m Housekeeping) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Quit
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}

	// Help toggle
	if key.Matches(msg, m.keys.Help) {
		m.showAll = !m.showAll
		return m, nil
	}

	// Complete state: enter exits
	if m.setup.Done() {
		if key.Matches(msg, m.keys.Enter) {
			return m, tea.Quit
		}
		return m, nil
	}

	state := m.setup.State()

	// Manual input mode: form gets special key handling
	if state == primitives.SetupManualInput {
		switch msg.String() {
		case "esc":
			m.setup = m.setup.Back()
			return m, nil
		case "tab", "down":
			m.setup = m.setup.FormNextField()
			return m, nil
		case "shift+tab", "up":
			m.setup = m.setup.FormPrevField()
			return m, nil
		case "enter":
			var cmd tea.Cmd
			m.setup, cmd = m.setup.Submit()
			return m, cmd
		default:
			var cmd tea.Cmd
			m.setup, cmd = m.setup.UpdateInput(msg)
			return m, cmd
		}
	}

	// Normal navigation
	switch {
	case key.Matches(msg, m.keys.Up):
		m.setup = m.setup.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.setup = m.setup.CursorDown()
	case key.Matches(msg, m.keys.Select):
		m.setup = m.setup.Toggle()
	case msg.String() == "i":
		m.setup = m.setup.EnterManualInput()
	case key.Matches(msg, m.keys.Enter):
		var cmd tea.Cmd
		m.setup, cmd = m.setup.Submit()
		return m, cmd
	}

	return m, nil
}

// View renders the setup wizard with carya's help bar.
func (m Housekeeping) View() string {
	content := m.setup.View()

	m.help.ShowAll = m.showAll
	helpView := m.help.View(m.keys)
	helpText := tui.HelpStyle.Render(helpView)

	return content + "\n" + helpText
}
