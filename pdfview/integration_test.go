package pdfview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// repoTestdata locates a PDF fixture regardless of where the test runs.
// The canonical home is examples/pdfview/testdata so the example can
// //go:embed it (embed disallows `..` paths). Tests usually run with
// WD = the package dir; the relative search below covers both that
// case and a run from the repo root.
func repoTestdata(t *testing.T, name string) string {
	t.Helper()
	for _, rel := range []string{
		"../examples/pdfview/testdata/" + name,
		"examples/pdfview/testdata/" + name,
	} {
		if _, err := os.Stat(rel); err == nil {
			abs, err := filepath.Abs(rel)
			if err == nil {
				return abs
			}
			return rel
		}
	}
	t.Skipf("testdata fixture %q not found relative to %s", name, mustGetwd(t))
	return ""
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

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

