package shared

import (
	"charm.land/bubbles/v2/viewport"
)

// SplitViewLayout holds the dimensions for a split-view layout
type SplitViewLayout struct {
	ListWidth     int
	DiffWidth     int
	ContentHeight int
}

// CalculateSplitViewLayout computes dimensions for a 40/60 split view
func CalculateSplitViewLayout(width, height, headerHeight, footerHeight int) SplitViewLayout {
	listWidth := int(float64(width) * 0.47)
	diffWidth := int(float64(width) * 0.47)
	contentHeight := height - headerHeight - footerHeight

	return SplitViewLayout{
		ListWidth:     listWidth,
		DiffWidth:     diffWidth,
		ContentHeight: contentHeight,
	}
}

// InitializeViewports creates new viewports with the given layout
func InitializeViewports(layout SplitViewLayout) (viewport.Model, viewport.Model) {
	listViewport := viewport.New(
		viewport.WithWidth(layout.ListWidth-2),
		viewport.WithHeight(layout.ContentHeight),
	)
	diffViewport := viewport.New(
		viewport.WithWidth(layout.DiffWidth-2),
		viewport.WithHeight(layout.ContentHeight),
	)
	return listViewport, diffViewport
}

// UpdateViewportSizes updates existing viewports with new dimensions
func UpdateViewportSizes(listVP, diffVP *viewport.Model, layout SplitViewLayout) {
	listVP.SetWidth(layout.ListWidth - 2)
	listVP.SetHeight(layout.ContentHeight)
	diffVP.SetWidth(layout.DiffWidth - 2)
	diffVP.SetHeight(layout.ContentHeight)
}

// EnsureItemVisible adjusts viewport offset to keep the cursor visible
func EnsureItemVisible(vp *viewport.Model, cursor int) {
	yOffset := vp.YOffset()
	height := vp.Height()

	if cursor < yOffset {
		vp.SetYOffset(cursor)
	} else if cursor >= yOffset+height {
		vp.SetYOffset(cursor - height + 1)
	}
}
