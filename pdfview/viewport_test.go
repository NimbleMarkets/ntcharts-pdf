package pdfview

import (
	"image"
	"image/color"
	"testing"
)

// gradientImage produces a 100×100 image whose pixels encode their X
// position in the red channel and Y position in the green channel.
// Useful for asserting that a viewport crop landed where we expect:
// the cropped sub-image's (0,0) pixel reads back the source-space
// coordinates the crop started at.
func gradientImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
		}
	}
	return img
}

func TestCropToViewport_Zoom0Identity(t *testing.T) {
	src := gradientImage()
	got := cropToViewport(src, 0, 0, 0)
	if got != image.Image(src) {
		// Pointer equality is correct here: zoom 0 must skip cropping
		// entirely so picture.Model gets the un-shrunk source.
		t.Errorf("zoom 0 should return src unchanged")
	}
}

func TestCropToViewport_ZoomCropsHalfThenQuarter(t *testing.T) {
	src := gradientImage()
	// zoom 1 → 50×50 starting at (0,0) when pan = (0,0)
	got := cropToViewport(src, 1, 0, 0)
	if got.Bounds().Dx() != 50 || got.Bounds().Dy() != 50 {
		t.Errorf("zoom 1: dx=%d dy=%d, want 50×50", got.Bounds().Dx(), got.Bounds().Dy())
	}
	// zoom 2 → 25×25
	got = cropToViewport(src, 2, 0, 0)
	if got.Bounds().Dx() != 25 || got.Bounds().Dy() != 25 {
		t.Errorf("zoom 2: dx=%d dy=%d, want 25×25", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestCropToViewport_PanShiftsRectangle(t *testing.T) {
	src := gradientImage()
	// zoom 1 panX=0.5 should start at x=50 — the gradient at (50, 0)
	// has R=50, G=0.
	got := cropToViewport(src, 1, 0.5, 0)
	// The cropped sub-image preserves source coordinates (SubImage on
	// *image.RGBA shares the backing array). Sample (50, 0) directly.
	r, g, _, _ := got.At(50, 0).RGBA()
	if r>>8 != 50 || g>>8 != 0 {
		t.Errorf("pan(0.5, 0) source (50,0) = (R=%d G=%d), want (50, 0)", r>>8, g>>8)
	}
	// Boundary clamp: panX=1.0 must clamp so the visible rect stays inside.
	got = cropToViewport(src, 1, 1.0, 0)
	if min := got.Bounds().Min.X; min != 50 {
		t.Errorf("over-clamped pan: bounds.Min.X = %d, want 50", min)
	}
}

func TestZoomInRecenters(t *testing.T) {
	m := New(80, 24)
	m.mode = ImageMode
	m.sourceImage = gradientImage()
	// Set initial viewport: zoom 1, pan to top-right quadrant.
	m.zoom, m.panX, m.panY = 1, 0.5, 0
	cx0, cy0 := m.viewportCenter() // 0.75, 0.25
	_ = m.ZoomIn()                 // doubles factor (zoom 2)
	cx1, cy1 := m.viewportCenter()
	if abs(cx0-cx1) > 0.01 || abs(cy0-cy1) > 0.01 {
		t.Errorf("ZoomIn did not preserve center: before=(%f,%f) after=(%f,%f)",
			cx0, cy0, cx1, cy1)
	}
	if m.zoom != 2 {
		t.Errorf("zoom after ZoomIn = %d, want 2", m.zoom)
	}
}

func TestZoomInClampsAtMax(t *testing.T) {
	m := New(80, 24)
	m.mode = ImageMode
	m.sourceImage = gradientImage()
	for i := 0; i < zoomMax+5; i++ {
		_ = m.ZoomIn()
	}
	if m.zoom != zoomMax {
		t.Errorf("ZoomIn past max: zoom=%d, want %d", m.zoom, zoomMax)
	}
}

func TestPanClampsAtBoundary(t *testing.T) {
	m := New(80, 24)
	m.mode = ImageMode
	m.sourceImage = gradientImage()
	m.zoom = 1
	// Drive pan past the right edge — should clamp at 0.5 (1 - 1/2).
	for i := 0; i < 50; i++ {
		_ = m.PanRight()
	}
	if m.panX > 0.5+1e-9 {
		t.Errorf("PanRight overshot: panX=%f, want <= 0.5", m.panX)
	}
}

func TestPanNoopAtZoomZero(t *testing.T) {
	m := New(80, 24)
	m.mode = ImageMode
	m.sourceImage = gradientImage()
	if cmd := m.PanLeft(); cmd != nil {
		t.Error("PanLeft at zoom 0 should return nil Cmd")
	}
	if m.panX != 0 {
		t.Errorf("panX changed at zoom 0: %f", m.panX)
	}
}

func TestResetViewClearsZoomAndPan(t *testing.T) {
	m := New(80, 24)
	m.mode = ImageMode
	m.sourceImage = gradientImage()
	m.zoom, m.panX, m.panY = 3, 0.4, 0.3
	_ = m.ResetView()
	if m.zoom != 0 || m.panX != 0 || m.panY != 0 {
		t.Errorf("ResetView: zoom=%d panX=%f panY=%f", m.zoom, m.panX, m.panY)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
