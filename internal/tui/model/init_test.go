package model

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyEnter() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

func keyDown() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyDown}
}

func keyUp() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyUp}
}

func keySelectX() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
}

func keyHelp() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
}

func TestNewInitDefaults(t *testing.T) {
	m := NewInit()

	if m.state != StateWelcome {
		t.Fatalf("expected welcome state, got %d", m.state)
	}
	if m.width != 80 {
		t.Fatalf("expected default width 80, got %d", m.width)
	}
	if m.selectedFeatures == nil {
		t.Fatal("expected selectedFeatures to be initialized")
	}
	if !m.confirmSelection {
		t.Fatal("expected confirmSelection default true")
	}
}

func TestInitFlowFeatureSelectionAndBack(t *testing.T) {
	m := NewInit()

	if _, cmd := m.Update(keyEnter()); cmd != nil {
		t.Fatal("expected nil cmd entering feature select")
	}
	if m.state != StateFeatureSelect {
		t.Fatalf("expected feature-select state, got %d", m.state)
	}

	if _, cmd := m.Update(keySelectX()); cmd != nil {
		t.Fatal("expected nil cmd when toggling feature")
	}
	if !m.IsFeatureEnabled(availableFeatures[0].Key) {
		t.Fatalf("expected feature %q enabled", availableFeatures[0].Key)
	}

	if _, cmd := m.Update(keyEnter()); cmd != nil {
		t.Fatal("expected nil cmd entering confirm state")
	}
	if m.state != StateConfirm {
		t.Fatalf("expected confirm state, got %d", m.state)
	}

	if _, cmd := m.Update(keyDown()); cmd != nil {
		t.Fatal("expected nil cmd toggling confirm selection")
	}
	if m.confirmSelection {
		t.Fatal("expected confirmSelection to be false after keyDown")
	}

	if _, cmd := m.Update(keyEnter()); cmd != nil {
		t.Fatal("expected nil cmd when returning to feature select")
	}
	if m.state != StateFeatureSelect {
		t.Fatalf("expected feature-select state after declining, got %d", m.state)
	}
}

func TestInitFlowConfirmReturnsExecuteCommand(t *testing.T) {
	m := NewInit()

	m.Update(keyEnter()) // welcome -> feature select
	m.Update(keyEnter()) // feature select -> confirm
	if m.state != StateConfirm {
		t.Fatalf("expected confirm state, got %d", m.state)
	}

	_, cmd := m.Update(keyEnter()) // confirm true -> execute + batch cmd
	if m.state != StateExecute {
		t.Fatalf("expected execute state, got %d", m.state)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command when starting execution")
	}
}

func TestInitUpdateWithFormSubmittedMsg(t *testing.T) {
	m := NewInit()
	featureErr := errors.New("boom")

	if _, cmd := m.Update(FormSubmittedMsg{
		Error:              nil,
		FeatureErrors:      map[string]error{"teamsync": featureErr},
		LaunchHousekeeping: true,
	}); cmd != nil {
		t.Fatal("expected nil cmd for FormSubmittedMsg")
	}

	if m.state != StateComplete {
		t.Fatalf("expected complete state, got %d", m.state)
	}
	if !m.ShouldLaunchHousekeeping() {
		t.Fatal("expected housekeeping launch to be true")
	}
	if !m.HasFeatureError("teamsync") {
		t.Fatal("expected teamsync feature error to be present")
	}
}

func TestInitWindowResizeAndHelpToggle(t *testing.T) {
	m := NewInit()

	m.Update(tea.WindowSizeMsg{Width: 123, Height: 45})
	if m.width != 123 || m.height != 45 {
		t.Fatalf("expected window size 123x45, got %dx%d", m.width, m.height)
	}

	if m.showAll {
		t.Fatal("expected help to start hidden")
	}
	m.Update(keyHelp())
	if !m.showAll {
		t.Fatal("expected help to toggle on")
	}
	m.Update(keyHelp())
	if m.showAll {
		t.Fatal("expected help to toggle off")
	}
}

func TestInitConfirmSelectionTogglesWithUpDown(t *testing.T) {
	m := NewInit()
	m.state = StateConfirm
	m.confirmSelection = true

	m.Update(keyDown())
	if m.confirmSelection {
		t.Fatal("expected keyDown to flip confirmSelection false")
	}

	m.Update(keyUp())
	if !m.confirmSelection {
		t.Fatal("expected keyUp to flip confirmSelection true")
	}
}
