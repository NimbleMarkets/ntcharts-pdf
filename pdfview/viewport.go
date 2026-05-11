// pdfview/viewport.go: zoom + pan plumbing for ImageMode.
//
// The viewport is described in normalized [0, 1] source-image coords:
// (panX, panY) is the top-left corner of the visible rectangle and
// (1/2^zoom) is its width and height in normalized units. Storing pan
// as a fraction (rather than pixels) lets the viewport survive a
// re-render at a different DPI or cell-rect size.

package pdfview

import (
	"image"
	"image/draw"

	tea "charm.land/bubbletea/v2"
)

const (
	// zoomMax bounds the magnification. 6 means a 64× zoom — fine enough
	// to see individual rasterized pixels at 300 DPI on a Letter-sized
	// page (the cropped source becomes ~40×52 px, scaled back up to the
	// cell rect with maximum chunkiness).
	zoomMax = 6

	// panStep is the fraction of a viewport that one PanLeft/Right/Up/Down
	// press travels. 0.25 means four presses traverse the full visible
	// area — fine for inspection without feeling sluggish.
	panStep = 0.25
)

// subImager is the SubImage signature implemented by concrete image
// types in image/. *image.RGBA (pdfium's output, plus our deep-copy),
// *image.NRGBA, *image.YCbCr etc. all satisfy it. Hosts that hand us
// other image types via SetPageImage fall through to the draw.Draw path
// in cropToViewport.
type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// viewportSize returns the normalized [0, 1] side length of the current
// visible rectangle in source-image coords.
func (m Model) viewportSize() float64 {
	return 1.0 / float64(uint(1)<<uint(m.zoom))
}

// viewportCenter returns the current viewport's center in normalized
// [0, 1] source-image coords.
func (m Model) viewportCenter() (cx, cy float64) {
	vp := m.viewportSize()
	return m.panX + vp/2, m.panY + vp/2
}

// recenterTo positions the viewport so its center sits at (cx, cy),
// clamping so the viewport stays inside [0, 1] on both axes. Called by
// ZoomIn / ZoomOut to keep what the user is looking at fixed across
// zoom transitions.
func (m *Model) recenterTo(cx, cy float64) {
	vp := m.viewportSize()
	m.panX = cx - vp/2
	m.panY = cy - vp/2
	m.clampPan()
}

// clampPan keeps the viewport rectangle inside [0, 1] × [0, 1]. Called
// after every mutation of zoom or pan.
func (m *Model) clampPan() {
	vp := m.viewportSize()
	if m.panX < 0 {
		m.panX = 0
	}
	if m.panY < 0 {
		m.panY = 0
	}
	if m.panX+vp > 1 {
		m.panX = 1 - vp
	}
	if m.panY+vp > 1 {
		m.panY = 1 - vp
	}
	// At zoom 0 the viewport is the whole image; force pan to origin.
	if m.zoom == 0 {
		m.panX, m.panY = 0, 0
	}
}

// pan applies a (dx, dy) step where each unit is one panStep in
// viewport-size units. Direction sign follows screen convention:
// dx > 0 pans right, dy > 0 pans down.
func (m *Model) pan(dx, dy float64) tea.Cmd {
	if m.mode != ImageMode || m.sourceImage == nil || m.zoom == 0 {
		return nil
	}
	step := panStep * m.viewportSize()
	m.panX += dx * step
	m.panY += dy * step
	m.clampPan()
	return m.applyViewport()
}

// applyViewport crops the source image to the current viewport and
// hands the result to the embedded picture.Model for rendering. Returns
// picture's SetImage Cmd unchanged.
func (m *Model) applyViewport() tea.Cmd {
	if m.sourceImage == nil {
		return nil
	}
	view := cropToViewport(m.sourceImage, m.zoom, m.panX, m.panY)
	return m.pic.SetImage(view)
}

// cropToViewport returns the sub-image described by (zoom, panX, panY)
// in normalized source-image coords. At zoom 0 it returns src unchanged.
//
// Concrete image types from the image/ package satisfy subImager and
// return a no-copy view that shares the source's pixel buffer. For other
// image types (rare — a host-supplied image.Image via SetPageImage that
// isn't a recognized standard library type) the fall-through copies the
// pixels into a fresh *image.RGBA so the consumer always sees the
// expected coordinate system.
func cropToViewport(src image.Image, zoom int, panX, panY float64) image.Image {
	if src == nil || zoom <= 0 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	vp := 1.0 / float64(uint(1)<<uint(zoom))
	x0 := b.Min.X + int(panX*float64(w)+0.5)
	y0 := b.Min.Y + int(panY*float64(h)+0.5)
	rectW := int(vp*float64(w) + 0.5)
	rectH := int(vp*float64(h) + 0.5)
	if rectW < 1 {
		rectW = 1
	}
	if rectH < 1 {
		rectH = 1
	}
	if x0+rectW > b.Max.X {
		x0 = b.Max.X - rectW
	}
	if y0+rectH > b.Max.Y {
		y0 = b.Max.Y - rectH
	}
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	cropRect := image.Rect(x0, y0, x0+rectW, y0+rectH)
	if si, ok := src.(subImager); ok {
		return si.SubImage(cropRect)
	}
	out := image.NewRGBA(image.Rect(0, 0, rectW, rectH))
	draw.Draw(out, out.Bounds(), src, cropRect.Min, draw.Src)
	return out
}
