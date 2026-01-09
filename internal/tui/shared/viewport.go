package shared

import (
	"github.com/charmbracelet/bubbles/viewport"
)

// SplitViewLayout holds the dimensions for a split-view layout
type SplitViewLayout struct {
	ListWidth     int
	DiffWidth     int
	ContentHeight int
}

// CalculateSplitViewLayout computes dimensions for a 40/60 split view
func CalculateSplitViewLayout(width, height, headerHeight, footerHeight int) SplitViewLayout {
	listWidth := int(float64(width) * 0.4)
	diffWidth := width - listWidth
	contentHeight := height - headerHeight - footerHeight

	return SplitViewLayout{
		ListWidth:     listWidth,
		DiffWidth:     diffWidth,
		ContentHeight: contentHeight,
	}
}

// InitializeViewports creates new viewports with the given layout
func InitializeViewports(layout SplitViewLayout) (viewport.Model, viewport.Model) {
	listViewport := viewport.New(layout.ListWidth-2, layout.ContentHeight)
	diffViewport := viewport.New(layout.DiffWidth-2, layout.ContentHeight)
	return listViewport, diffViewport
}

// UpdateViewportSizes updates existing viewports with new dimensions
func UpdateViewportSizes(listVP, diffVP *viewport.Model, layout SplitViewLayout) {
	listVP.Width = layout.ListWidth - 2
	listVP.Height = layout.ContentHeight
	diffVP.Width = layout.DiffWidth - 2
	diffVP.Height = layout.ContentHeight
}

// EnsureItemVisible adjusts viewport offset to keep the cursor visible
func EnsureItemVisible(vp *viewport.Model, cursor int) {
	if cursor < vp.YOffset {
		vp.YOffset = cursor
	} else if cursor >= vp.YOffset+vp.Height {
		vp.YOffset = cursor - vp.Height + 1
	}
}
