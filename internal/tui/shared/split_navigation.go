package shared

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// SplitPaneNavResult describes the outcome of shared split-pane input handling.
type SplitPaneNavResult struct {
	Handled      bool
	NeedsRefresh bool
	Cmd          tea.Cmd
}

// HandleSplitPaneNavigation handles shared split-pane motions:
// - left pane navigation (up/down)
// - right pane scrolling (ctrl+d/ctrl+u)
// - search mode enter/exit and query editing
func HandleSplitPaneNavigation(
	msg tea.KeyMsg,
	searching *bool,
	search *textinput.Model,
	cursor *int,
	itemCount int,
	diffViewport *viewport.Model,
	matchUp bool,
	matchDown bool,
	onSearchChanged func(),
) SplitPaneNavResult {
	if *searching {
		switch msg.String() {
		case "esc", "enter":
			*searching = false
			search.Blur()
			return SplitPaneNavResult{Handled: true, NeedsRefresh: true}
		}

		var cmd tea.Cmd
		*search, cmd = search.Update(msg)
		onSearchChanged()
		return SplitPaneNavResult{Handled: true, NeedsRefresh: true, Cmd: cmd}
	}

	switch {
	case msg.String() == "/":
		*searching = true
		search.Focus()
		return SplitPaneNavResult{Handled: true, Cmd: textinput.Blink}
	case matchUp:
		if *cursor > 0 && itemCount > 0 {
			*cursor = *cursor - 1
			return SplitPaneNavResult{Handled: true, NeedsRefresh: true}
		}
		return SplitPaneNavResult{Handled: true}
	case matchDown:
		if *cursor < itemCount-1 && itemCount > 0 {
			*cursor = *cursor + 1
			return SplitPaneNavResult{Handled: true, NeedsRefresh: true}
		}
		return SplitPaneNavResult{Handled: true}
	case msg.String() == "ctrl+d":
		diffViewport.PageDown()
		return SplitPaneNavResult{Handled: true}
	case msg.String() == "ctrl+u":
		diffViewport.PageUp()
		return SplitPaneNavResult{Handled: true}
	}

	return SplitPaneNavResult{}
}

func IsUpKey(msg tea.KeyMsg) bool {
	key := msg.String()
	return key == "up" || key == "k"
}

func IsDownKey(msg tea.KeyMsg) bool {
	key := msg.String()
	return key == "down" || key == "j"
}
