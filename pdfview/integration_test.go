//go:build integration

package pdfview

import (
	"image"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// drainLoad runs the Cmd returned by SetPDF synchronously and feeds the
// resulting msg back through Update, returning the post-load Model.
// loadPDFCmd does its work inside the Cmd closure, so executing the Cmd
// is enough to produce the pdfLoadedMsg.
func drainLoad(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("SetPDF returned nil Cmd")
	}
	msg := cmd()
	m, _ = m.Update(msg)
	return m
}

func TestLoadExamplePDF(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	m := New(80, 24)
	m = drainLoad(t, m, m.SetPDF(path))

	if err := m.Err(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if got := m.NumPages(); got != 3 {
		t.Errorf("NumPages() = %d, want 3", got)
	}
	if got := m.Page(); got != 1 {
		t.Errorf("Page() = %d, want 1", got)
	}
	// Page 1 of Example.pdf has the text "Black RGB Gray RGB Neutral CIELAB"
	// (a colorimetric table). With spatial layout the substring lives at
	// its projected (col, row), not at the start of the rendered string —
	// search the full View() Content.
	view := m.View().Content
	if !strings.Contains(view, "CIELAB") {
		t.Errorf("View() Content missing expected substring %q", "CIELAB")
	}
}

func TestImageOnlyPageRendersPlaceholder(t *testing.T) {
	// Example.pdf pages 2 and 3 are pure-image (the content stream is a
	// single XObject Do call — no text operators). They exercise the
	// "no text, has image" branch of TextMode rendering: zero runs but
	// a placeholder for the embedded image.
	path := repoTestdata(t, "Example.pdf")
	m := New(80, 24)
	m = drainLoad(t, m, m.SetPDF(path))

	if err := m.Err(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if _ = m.SetPage(2); m.Page() != 2 {
		t.Fatalf("SetPage(2) -> %d", m.Page())
	}
	if got := len(m.docPages[1].runs); got != 0 {
		t.Errorf("docPages[1].runs len = %d, want 0 (image-only page)", got)
	}
	if got := m.docPages[1].imageCount; got < 1 {
		t.Errorf("docPages[1].imageCount = %d, want >= 1", got)
	}
	if v := m.View().Content; v == "" {
		t.Error("View() Content is empty for image-only page")
	}
}

func TestSetPDFDataMatchesSetPDF(t *testing.T) {
	// Round-trip the same PDF two ways — once via SetPDF(path), once
	// via SetPDFData(name, bytes) — and assert both produce the same
	// page count, the same text presence, and a working renderer.
	path := repoTestdata(t, "Example.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	mPath := New(80, 24)
	mPath = drainLoad(t, mPath, mPath.SetPDF(path))

	mData := New(80, 24)
	mData = drainLoad(t, mData, mData.SetPDFData("Example.pdf", data))

	if mPath.NumPages() != mData.NumPages() {
		t.Errorf("NumPages mismatch: path=%d data=%d", mPath.NumPages(), mData.NumPages())
	}
	if got := mData.Page(); got != 1 {
		t.Errorf("SetPDFData Page() = %d, want 1", got)
	}
	// Display name carries through: SetPDFData uses the supplied name
	// as the View() "path" label.
	if want, got := "Example.pdf", mData.path; got != want {
		t.Errorf("SetPDFData path label = %q, want %q", got, want)
	}
	// Text content reaches View identically (page-1 CIELAB is at the
	// same projected cell either way).
	if !strings.Contains(mPath.View().Content, "CIELAB") || !strings.Contains(mData.View().Content, "CIELAB") {
		t.Error("CIELAB missing from at least one View")
	}
}

func TestPageNavigationOnLoadedDoc(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	m := New(80, 24)
	m = drainLoad(t, m, m.SetPDF(path))

	_ = m.NextPage()
	if m.Page() != 2 {
		t.Errorf("after NextPage Page() = %d, want 2", m.Page())
	}
	_ = m.SetPage(99)
	if m.Page() != 3 {
		t.Errorf("SetPage(99) Page() = %d, want 3 (clamped to NumPages)", m.Page())
	}
	_ = m.PrevPage()
	if m.Page() != 2 {
		t.Errorf("after PrevPage Page() = %d, want 2", m.Page())
	}
}

func TestPageDimensionsLoadedButNotRendered(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	m := New(80, 24)
	m = drainLoad(t, m, m.SetPDF(path))

	for n := 1; n <= m.NumPages(); n++ {
		if w, h, ok := m.PageDimensions(n); ok || w != 0 || h != 0 {
			t.Errorf("PageDimensions(%d) before rasterization = (%d, %d, %v), want (0, 0, false)", n, w, h, ok)
		}
	}
}

func TestPageDimensionsOutOfRangeAfterLoad(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	m := New(80, 24)
	m = drainLoad(t, m, m.SetPDF(path))

	if w, h, ok := m.PageDimensions(m.NumPages() + 1); ok || w != 0 || h != 0 {
		t.Errorf("PageDimensions(NumPages+1) = (%d, %d, %v), want (0, 0, false)", w, h, ok)
	}
}

func TestPageDimensionsFromSetPageImage(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	m := New(80, 24)
	m = drainLoad(t, m, m.SetPDF(path))

	img := image.NewRGBA(image.Rect(0, 0, 400, 600))
	_ = m.SetPageImage(1, img)

	w, h, ok := m.PageDimensions(1)
	if !ok {
		t.Fatalf("PageDimensions(1) after SetPageImage = ok=false, want true")
	}
	if w != 400 || h != 600 {
		t.Errorf("PageDimensions(1) = (%d, %d), want (400, 600)", w, h)
	}
}

func TestPageDimensionsLatestWriteWins(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	m := New(80, 24)
	m = drainLoad(t, m, m.SetPDF(path))

	_ = m.SetPageImage(1, image.NewRGBA(image.Rect(0, 0, 400, 600)))
	_ = m.SetPageImage(1, image.NewRGBA(image.Rect(0, 0, 800, 1200)))

	w, h, ok := m.PageDimensions(1)
	if !ok || w != 800 || h != 1200 {
		t.Errorf("PageDimensions(1) after second SetPageImage = (%d, %d, %v), want (800, 1200, true)", w, h, ok)
	}
}
