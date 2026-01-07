package tui

import (
	"log"
	
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// UpdateWithErrorHandling handles the model update with proper error handling
func (m *CommitComposerModel) UpdateWithErrorHandling(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle success/error messages from commit process
	switch msg := msg.(type) {
	case errMsg:
		m.err = msg.err
		m.status = StatusError
		log.Printf("Commit error: %v", m.err)
		return m, nil
		
	case successMsg:
		m.result = msg.output
		m.status = StatusDone
		log.Printf("Commit successful: %s", m.result)
		return m, nil
		
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// Handle different states based on current status
	switch m.status {
	case StatusSelecting:
		return m.updateSelecting(msg)
	case StatusEditing:
		return m.updateEditing(msg)
	case StatusConfirming:
		return m.updateConfirming(msg)
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