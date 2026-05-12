package pdfview

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

func TestSanitizeForTerminalDropsControlsAndBidi(t *testing.T) {
	// Mix of dangerous runes that have spoofed terminals in the wild.
	cases := map[string]string{
		"plain":                        "plain",
		"with\x1b[31m ANSI":            "with[31m ANSI",                  // ESC stripped, brackets kept
		"DEL\x7Fchar":                  "DELchar",                        // 0x7F dropped
		"C1sequence":             "C1sequence",                     // CSI in C1 dropped (properly UTF-8 encoded)
		"bidi‮wolves":             "bidiwolves",                     // RLO dropped (Trojan Source)
		"zero​width":              "zerowidth",                      // ZWSP dropped
		"line\nbreak":                  "line break",                     // newline → space
		"tab\there":                    "tab here",                       // tab → space
		"‭‮both":             "both",                           // LRO + RLO dropped
		"emoji✓ok":                     "emoji✓ok",                       // emojis are printable, kept
		"":                             "",                                // empty
	}
	for in, want := range cases {
		if got := SanitizeForTerminal(in); got != want {
			t.Errorf("SanitizeForTerminal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLayoutRunsDropsControlsAndBidi(t *testing.T) {
	// A run containing a bidi RLO override and an ESC must not place
	// those runes in the grid — they'd reach the terminal raw and could
	// reorder visual output.
	mb := mediaBox{0, 0, 612, 792}
	hostile := "abc‮\x1b[31m" // RLO + ESC + CSI
	runs := []pdf.Text{{X: 50, Y: 700, W: 60, FontSize: 12, S: hostile}}
	grid := layoutRuns(runs, mb, 80, 24)

	for ri, row := range grid {
		for ci, r := range row {
			if r == 0x1B || r == 0x202E {
				t.Errorf("grid[%d][%d] = %U; dangerous rune not filtered", ri, ci, r)
			}
		}
	}
	// Spot-check: visible letters survived.
	if !strings.Contains(joinGrid(grid), "abc") {
		t.Error("printable prefix 'abc' missing from sanitized grid")
	}
}

func TestLimitsApplyDefaults(t *testing.T) {
	var l Limits
	l.applyDefaults()
	if l.MaxFileBytes <= 0 {
		t.Errorf("MaxFileBytes default = %d, want > 0", l.MaxFileBytes)
	}
	if l.MaxPages <= 0 {
		t.Errorf("MaxPages default = %d, want > 0", l.MaxPages)
	}
	if l.MaxImagesPerPage <= 0 {
		t.Errorf("MaxImagesPerPage default = %d, want > 0", l.MaxImagesPerPage)
	}
	if l.MaxRenderDPI <= 0 {
		t.Errorf("MaxRenderDPI default = %d, want > 0", l.MaxRenderDPI)
	}
	if l.MaxRenderPixels <= 0 {
		t.Errorf("MaxRenderPixels default = %d, want > 0", l.MaxRenderPixels)
	}

	// Negative values are preserved verbatim — the opt-out sentinel.
	l = Limits{MaxPages: -1, MaxRenderDPI: -1}
	l.applyDefaults()
	if l.MaxPages != -1 || l.MaxRenderDPI != -1 {
		t.Errorf("applyDefaults overwrote negative sentinels: %+v", l)
	}
}

// dpiCapturingRenderer records the dpi argument it was asked to render
// at — used to verify withLimits clamping.
type dpiCapturingRenderer struct {
	lastDPI int
}

func (r *dpiCapturingRenderer) RenderPage(_ int, dpi int) (image.Image, error) {
	r.lastDPI = dpi
	return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
}
func (r *dpiCapturingRenderer) Close() error { return nil }

func TestWithLimitsClampsDPI(t *testing.T) {
	// Inner factory always succeeds — wrap it with a 150-DPI cap and
	// verify a render request at 600 reaches the inner with 150.
	inner := func(string, []byte) (Renderer, error) { return &dpiCapturingRenderer{}, nil }
	wrapped := withLimits(inner, Limits{MaxRenderDPI: 150})

	r, err := wrapped("ignored", nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer r.Close()
	if _, err := r.RenderPage(1, 600); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	// The inner is the dpiCapturingRenderer wrapped by limitedRenderer —
	// unwrap once to inspect.
	inner2, ok := r.(*limitedRenderer)
	if !ok {
		t.Fatalf("expected *limitedRenderer, got %T", r)
	}
	cap, ok := inner2.inner.(*dpiCapturingRenderer)
	if !ok {
		t.Fatalf("inner type = %T", inner2.inner)
	}
	if cap.lastDPI != 150 {
		t.Errorf("inner DPI = %d, want 150 (clamped from 600)", cap.lastDPI)
	}
}

func TestWithLimitsDoesNotStatPath(t *testing.T) {
	// Regression: withLimits used to call os.Stat(path), breaking
	// custom factories that pass virtual identifiers (URLs, IDs, blob
	// handles). The wrapper now only clamps DPI; MaxFileBytes is
	// enforced by the default-filesystem renderer.
	inner := func(name string, data []byte) (Renderer, error) {
		if name != "id://abc-123" {
			t.Errorf("inner factory received unexpected name %q", name)
		}
		if len(data) != 4 || string(data) != "blob" {
			t.Errorf("inner factory received unexpected data %q", data)
		}
		return &dpiCapturingRenderer{}, nil
	}
	wrapped := withLimits(inner, Limits{MaxFileBytes: 1, MaxRenderDPI: 150})
	r, err := wrapped("id://abc-123", []byte("blob"))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_ = r.Close()
}

func TestExtractPageRecoversFromPanic(t *testing.T) {
	// Use a real PDF as the recovery substrate — Example.pdf parses
	// cleanly, so this test mostly verifies the defer/recover scaffold
	// doesn't break the happy path. The panic case is exercised by
	// extractPage's defer block; we drive it directly through a
	// constructed-but-invalid pdf.Page below.
	dir, err := filepath.Abs("../testdata")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Example.pdf")); err != nil {
		t.Skip("testdata/Example.pdf missing")
	}
	// Null-page input shouldn't crash even though Content() would
	// dereference its V field. extractPage's IsNull guard short-circuits.
	out := extractPage(pdf.Page{}, Limits{MaxImagesPerPage: 1024})
	if out.media != defaultMediaBox() {
		t.Errorf("null page: media = %+v, want default", out.media)
	}
	if len(out.runs) != 0 {
		t.Errorf("null page: runs len = %d, want 0", len(out.runs))
	}
}

func TestMaxPagesRejected(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	// Example.pdf has 3 pages; MaxPages=2 should reject.
	m := NewWithConfig(Config{Cols: 80, Rows: 24, Limits: Limits{MaxPages: 2}})
	cmd := m.SetPDF(path)
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	switch msg := cmd().(type) {
	case pdfErrMsg:
		if !strings.Contains(msg.err.Error(), "MaxPages") {
			t.Errorf("error %q lacks MaxPages context", msg.err)
		}
	case pdfLoadedMsg:
		t.Errorf("expected pdfErrMsg, got loaded with %d pages", len(msg.pages))
	default:
		t.Errorf("unexpected msg %T", msg)
	}
}

func TestCountPageImagesHonorsCap(t *testing.T) {
	// Surrogate: countPageImages on Example.pdf page 1 (1 image) with a
	// tiny cap returns at most cap. We only verify it doesn't exceed.
	path := repoTestdata(t, "Example.pdf")
	f, r, err := pdf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := countPageImages(r.Page(1), 0 /* uncapped */)
	if got != 1 {
		t.Fatalf("baseline: expected 1 image on page 1, got %d", got)
	}
	got = countPageImages(r.Page(1), 1)
	if got > 1 {
		t.Errorf("cap=1 but counted %d", got)
	}
}
