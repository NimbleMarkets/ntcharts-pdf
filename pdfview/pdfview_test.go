package pdfview

import (
	"errors"
	"image"
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ledongthuc/pdf"
)

func TestNewDefaults(t *testing.T) {
	m := New(80, 24)
	if got := m.Page(); got != 1 {
		t.Errorf("Page() = %d, want 1 (InitialPage default)", got)
	}
	if got := m.NumPages(); got != 0 {
		t.Errorf("NumPages() = %d, want 0 (no doc loaded)", got)
	}
	if got := m.Mode(); got != TextMode {
		t.Errorf("Mode() = %v, want TextMode", got)
	}
	if m.Err() != nil {
		t.Errorf("Err() = %v, want nil", m.Err())
	}
}

func TestNewWithConfigInitialPageClamp(t *testing.T) {
	m := NewWithConfig(Config{Cols: 80, Rows: 24, InitialPage: 0})
	if got := m.Page(); got != 1 {
		t.Errorf("Page() = %d, want 1 when InitialPage is zero", got)
	}
}

func TestToggleMode(t *testing.T) {
	m := New(80, 24)
	if m.Mode() != TextMode {
		t.Fatalf("starting Mode = %v, want TextMode", m.Mode())
	}
	_ = m.ToggleMode()
	if m.Mode() != ImageMode {
		t.Errorf("after toggle Mode = %v, want ImageMode", m.Mode())
	}
	_ = m.ToggleMode()
	if m.Mode() != TextMode {
		t.Errorf("after second toggle Mode = %v, want TextMode", m.Mode())
	}
}

func TestSetPageWithoutDocumentNoop(t *testing.T) {
	m := New(80, 24)
	if cmd := m.SetPage(5); cmd != nil {
		t.Errorf("SetPage(5) on empty doc returned non-nil Cmd")
	}
	if m.Page() != 1 {
		t.Errorf("Page() changed to %d without a loaded doc", m.Page())
	}
}

func TestDefaultKeyMapBindsExpectedKeys(t *testing.T) {
	km := DefaultKeyMap()
	cases := map[string]struct {
		binding interface{ Keys() []string }
		want    string
	}{
		"Next":         {&km.Next, "n"},
		"Prev":         {&km.Prev, "p"},
		"ToggleMode":   {&km.ToggleMode, "m"},
		"ToggleRender": {&km.ToggleRender, "g"},
		"Reload":       {&km.Reload, "r"},
	}
	for name, c := range cases {
		keys := c.binding.Keys()
		if !slices.Contains(keys, c.want) {
			t.Errorf("%s binding keys = %v, want to include %q", name, keys, c.want)
		}
	}
}

// fakeRenderer is a Renderer that returns a single-pixel image. Used to
// verify the widget plumbs the renderer correctly without depending on
// pdfium being able to actually open the test file.
type fakeRenderer struct {
	calls  int
	page   int
	dpi    int
	closes int
	err    error
}

func (r *fakeRenderer) RenderPage(page, dpi int) (image.Image, error) {
	r.calls++
	r.page = page
	r.dpi = dpi
	if r.err != nil {
		return nil, r.err
	}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	return img, nil
}

func (r *fakeRenderer) Close() error {
	r.closes++
	return nil
}

// fakeFactory returns a RendererFactory that always yields the same
// fakeRenderer, so tests can both inject the renderer and inspect it
// after the load Cmd runs.
func fakeFactory(r *fakeRenderer) RendererFactory {
	return func(_ string) (Renderer, error) { return r, nil }
}

func TestLoadedMsgPopulatesState(t *testing.T) {
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	gen := bump(m.loadGen)

	// Pretend a load completed with our gen.
	msg := pdfLoadedMsg{
		path:  "/tmp/fake.pdf",
		pages: make([]pdfPage, 3),
		gen:   gen,
	}
	var cmd tea.Cmd
	m, cmd = m.Update(msg)
	_ = cmd
	if m.NumPages() != 3 {
		t.Errorf("NumPages() = %d, want 3", m.NumPages())
	}
	if m.Page() != 1 {
		t.Errorf("Page() = %d, want 1", m.Page())
	}
}

func TestLoadedMsgStaleGenIgnored(t *testing.T) {
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	bump(m.loadGen) // gen=1
	bump(m.loadGen) // gen=2 — current

	// Stale msg carrying gen=1 should be ignored.
	msg := pdfLoadedMsg{path: "/tmp/old.pdf", pages: make([]pdfPage, 9), gen: 1}
	m, _ = m.Update(msg)
	if m.NumPages() != 0 {
		t.Errorf("stale pdfLoadedMsg applied: NumPages = %d, want 0", m.NumPages())
	}
}

func TestErrMsgRecordsError(t *testing.T) {
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	gen := bump(m.loadGen)
	want := errors.New("boom")
	m, _ = m.Update(pdfErrMsg{err: want, gen: gen})
	if got := m.Err(); !errors.Is(got, want) {
		t.Errorf("Err() = %v, want %v", got, want)
	}
}

func TestRendererInvokedOnImageModeWithLoadedDoc(t *testing.T) {
	fake := &fakeRenderer{}
	m := NewWithConfig(Config{Cols: 80, Rows: 24, RendererFactory: fakeFactory(fake), RenderDPI: 200})

	gen := bump(m.loadGen)
	m, _ = m.Update(pdfLoadedMsg{
		path:     "/tmp/fake.pdf",
		pages:    make([]pdfPage, 2),
		renderer: fake,
		gen:      gen,
	})
	// Toggling to ImageMode should dispatch a renderPageCmd.
	cmd := m.ToggleMode()
	if cmd == nil {
		t.Fatal("ToggleMode -> ImageMode returned nil Cmd; expected a render")
	}
	// Executing the Cmd should call the fake renderer.
	msg := cmd()
	if fake.calls != 1 {
		t.Errorf("fakeRenderer.calls = %d, want 1", fake.calls)
	}
	if fake.page != 1 {
		t.Errorf("fakeRenderer.page = %d, want 1", fake.page)
	}
	if fake.dpi != 200 {
		t.Errorf("fakeRenderer.dpi = %d, want 200 (from Config.RenderDPI)", fake.dpi)
	}
	if _, ok := msg.(pageRenderedMsg); !ok {
		t.Errorf("renderPageCmd msg type = %T, want pageRenderedMsg", msg)
	}
}

func TestSetPDFClosesPreviousRenderer(t *testing.T) {
	first := &fakeRenderer{}
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	gen := bump(m.loadGen)
	m, _ = m.Update(pdfLoadedMsg{
		path: "/tmp/a.pdf", pages: make([]pdfPage, 1),
		renderer: first, gen: gen,
	})
	if first.closes != 0 {
		t.Fatalf("precondition: first.closes = %d, want 0", first.closes)
	}
	// Issue a second SetPDF — the old renderer should be closed eagerly,
	// before the new load even runs.
	_ = m.SetPDF("/tmp/b.pdf")
	if first.closes != 1 {
		t.Errorf("SetPDF did not close previous renderer; closes = %d", first.closes)
	}
}

func TestModelCloseReleasesRenderer(t *testing.T) {
	fake := &fakeRenderer{}
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	gen := bump(m.loadGen)
	m, _ = m.Update(pdfLoadedMsg{
		path: "/tmp/a.pdf", pages: make([]pdfPage, 1),
		renderer: fake, gen: gen,
	})
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if fake.closes != 1 {
		t.Errorf("after Model.Close fakeRenderer.closes = %d, want 1", fake.closes)
	}
	// Idempotent: second Close should be a no-op.
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if fake.closes != 1 {
		t.Errorf("second Close double-released; closes = %d", fake.closes)
	}
}

func TestStaleRenderMsgDroppedAfterSetPDF(t *testing.T) {
	// Regression for: SetPDF used to bump only loadGen, leaving renderGen
	// untouched. An in-flight render from the previous document whose
	// page number happened to match could slip through Update's gen
	// check and clobber the new document's state.
	rA := &fakeRenderer{}
	rB := &fakeRenderer{}
	m := NewWithConfig(Config{Cols: 80, Rows: 24})

	// Synthesize doc A's "loaded" state. SetPDF bumps loadGen + renderGen
	// internally, and the pdfLoadedMsg flow installs the renderer.
	_ = m.SetPDF("/tmp/a.pdf")
	m, _ = m.Update(pdfLoadedMsg{
		path: "/tmp/a.pdf", pages: make([]pdfPage, 1),
		renderer: rA, gen: *m.loadGen,
	})
	if m.cur != rA {
		t.Fatalf("doc A: m.cur = %v, want rA", m.cur)
	}

	// Enter ImageMode — this builds the renderPageCmd but we capture it
	// instead of executing it, simulating an in-flight render.
	staleRender := m.ToggleMode()
	if staleRender == nil {
		t.Fatal("expected render Cmd from ToggleMode")
	}

	// Switch to doc B. Must close rA, bump loadGen + renderGen, and
	// install rB as the live renderer.
	_ = m.SetPDF("/tmp/b.pdf")
	if rA.closes != 1 {
		t.Errorf("doc A renderer not closed eagerly; closes = %d", rA.closes)
	}
	m, _ = m.Update(pdfLoadedMsg{
		path: "/tmp/b.pdf", pages: make([]pdfPage, 1),
		renderer: rB, gen: *m.loadGen,
	})
	if m.cur != rB {
		t.Fatalf("doc B: m.cur = %v, want rB", m.cur)
	}

	// Snapshot the post-load state, then deliver the stale render's msg
	// (built against doc A's renderer + gens). Update must drop it.
	prevImage := m.sourceImage
	prevSourceB := rB.calls
	staleMsg := staleRender()
	if _, ok := staleMsg.(pageRenderedMsg); !ok {
		t.Fatalf("stale render produced %T, want pageRenderedMsg", staleMsg)
	}
	m, _ = m.Update(staleMsg)

	if m.sourceImage != prevImage {
		t.Error("stale pageRenderedMsg leaked into m.sourceImage")
	}
	if rB.calls != prevSourceB {
		t.Errorf("stale msg triggered rB render: calls = %d, want %d", rB.calls, prevSourceB)
	}
}

func TestStaleLoadedMsgClosesRenderer(t *testing.T) {
	stale := &fakeRenderer{}
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	bump(m.loadGen) // gen=1
	bump(m.loadGen) // gen=2 — current

	// Stale msg carrying gen=1 should be ignored AND its Renderer closed
	// so we don't leak a pdfium handle.
	msg := pdfLoadedMsg{path: "/tmp/old.pdf", pages: make([]pdfPage, 9), renderer: stale, gen: 1}
	m, _ = m.Update(msg)
	if stale.closes != 1 {
		t.Errorf("stale renderer not closed; closes = %d", stale.closes)
	}
}

func TestRendererErrSurfaces(t *testing.T) {
	wantErr := errors.New("kaboom")
	fake := &fakeRenderer{err: wantErr}
	m := NewWithConfig(Config{Cols: 80, Rows: 24, RendererFactory: fakeFactory(fake)})
	gen := bump(m.loadGen)
	m, _ = m.Update(pdfLoadedMsg{
		path: "/tmp/fake.pdf", pages: make([]pdfPage, 1),
		renderer: fake, gen: gen,
	})
	cmd := m.ToggleMode()
	if cmd == nil {
		t.Fatal("expected render Cmd")
	}
	// Capture the pageRenderErrMsg and feed it back into Update.
	m, _ = m.Update(cmd())
	if got := m.Err(); !errors.Is(got, wantErr) {
		t.Errorf("Err() = %v, want %v", got, wantErr)
	}
}

func TestUpdateDispatchesKeyMapBindings(t *testing.T) {
	// Verify the widget's Update consumes tea.KeyMsg via KeyMap, calling
	// the corresponding action method. Pre-load a 2-page document so
	// NextPage produces a visible page change.
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	gen := bump(m.loadGen)
	m, _ = m.Update(pdfLoadedMsg{
		path: "/tmp/x.pdf", pages: make([]pdfPage, 2), gen: gen,
	})
	if m.Page() != 1 {
		t.Fatalf("preconditions: Page() = %d, want 1", m.Page())
	}

	// "n" is bound to KeyMap.Next; should advance to page 2.
	m, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.Page() != 2 {
		t.Errorf("after 'n' KeyMsg: Page() = %d, want 2", m.Page())
	}
	// "m" toggles mode.
	if mode := m.Mode(); mode != TextMode {
		t.Fatalf("preconditions: Mode() = %v, want TextMode", mode)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.Mode() != ImageMode {
		t.Errorf("after 'm' KeyMsg: Mode() = %v, want ImageMode", m.Mode())
	}
}

func TestLayoutRunsPacksAdjacentGlyphs(t *testing.T) {
	// Simulate ledongthuc/pdf's per-glyph output: each letter of "Hello"
	// is its own run, contiguous in PDF space (X gap < FontSize*0.2 each).
	// After packing, the row should contain "Hello" with no internal
	// gaps despite each glyph projecting to a different rounded col.
	mb := mediaBox{0, 0, 612, 792}
	runs := []pdf.Text{
		{X: 100, Y: 700, W: 12, FontSize: 12, S: "H"},
		{X: 112, Y: 700, W: 8, FontSize: 12, S: "e"},
		{X: 120, Y: 700, W: 6, FontSize: 12, S: "l"},
		{X: 126, Y: 700, W: 6, FontSize: 12, S: "l"},
		{X: 132, Y: 700, W: 10, FontSize: 12, S: "o"},
	}
	grid := layoutRuns(runs, mb, 80, 24)
	// Packing is what we're verifying — not the exact row the projection
	// landed on. Scan the whole grid.
	all := joinGrid(grid)
	if !strings.Contains(all, "Hello") {
		t.Errorf("packed text not found; rendered grid:\n%s", all)
	}
}

func TestLayoutRunsBreaksDistantRuns(t *testing.T) {
	// Two runs separated by a large PDF-space gap should NOT pack — the
	// second run is placed at its projected column, preserving column
	// layout that's common in tables.
	mb := mediaBox{0, 0, 612, 792}
	runs := []pdf.Text{
		{X: 50, Y: 700, W: 50, FontSize: 12, S: "Left"},
		{X: 500, Y: 700, W: 50, FontSize: 12, S: "Right"},
	}
	grid := layoutRuns(runs, mb, 80, 24)
	// Find the row that holds both tokens. The exact row index depends on
	// projection rounding; what we're verifying is the gap policy.
	var rowStr string
	for _, row := range grid {
		s := string(row)
		if strings.Contains(s, "Left") && strings.Contains(s, "Right") {
			rowStr = s
			break
		}
	}
	if rowStr == "" {
		t.Fatalf("Left and Right not co-located on any row; grid:\n%s", joinGrid(grid))
	}
	leftIdx := strings.Index(rowStr, "Left")
	rightIdx := strings.Index(rowStr, "Right")
	if rightIdx-leftIdx < 30 {
		t.Errorf("Left and Right are too close: left@%d right@%d in %q", leftIdx, rightIdx, rowStr)
	}
}

func TestRendererFactoryErrorSurfacesViaRendererErr(t *testing.T) {
	// Regression: factory errors used to be silently dropped, so
	// HasRenderer() returned true while m.cur was nil. Now RendererErr
	// carries the cause and HasRenderer reflects actual attachment.
	wantErr := errors.New("pdfium boom")
	factory := func(string) (Renderer, error) { return nil, wantErr }
	m := NewWithConfig(Config{Cols: 80, Rows: 24, RendererFactory: factory})
	gen := bump(m.loadGen)
	m, _ = m.Update(pdfLoadedMsg{
		path:        "/tmp/x.pdf",
		pages:       make([]pdfPage, 1),
		renderer:    nil,
		rendererErr: wantErr,
		gen:         gen,
	})
	if m.HasRenderer() {
		t.Error("HasRenderer() = true after factory error; want false")
	}
	if got := m.RendererErr(); !errors.Is(got, wantErr) {
		t.Errorf("RendererErr() = %v, want %v", got, wantErr)
	}
}

func TestSetPDFClearsRendererErr(t *testing.T) {
	prior := errors.New("old failure")
	m := NewWithConfig(Config{Cols: 80, Rows: 24})
	m.rendererErr = prior
	_ = m.SetPDF("/tmp/new.pdf")
	if m.RendererErr() != nil {
		t.Errorf("SetPDF did not clear RendererErr; still %v", m.RendererErr())
	}
}

func TestInitialPathLoadErrorShownInView(t *testing.T) {
	// Regression: View() short-circuited on m.path == "" before checking
	// m.err, so a failed InitialPath load showed "No document loaded"
	// instead of the actual error. NewWithConfig now pre-sets m.path
	// from cfg.InitialPath; the error check fires first.
	m := NewWithConfig(Config{Cols: 80, Rows: 24, InitialPath: "/tmp/does-not-exist.pdf"})

	// Simulate the load Cmd's failure delivering a pdfErrMsg.
	gen := bump(m.loadGen)
	wantErr := errors.New("open: no such file")
	m, _ = m.Update(pdfErrMsg{err: wantErr, gen: gen})

	content := m.View().Content
	if strings.Contains(content, "No document loaded") {
		t.Errorf("View showed 'No document loaded' instead of error; content=%q", content)
	}
	if !strings.Contains(content, wantErr.Error()) {
		t.Errorf("View Content missing error text %q; got %q", wantErr.Error(), content)
	}
}

func TestViewWithoutDocumentShowsStatus(t *testing.T) {
	m := New(80, 24)
	v := m.View()
	if v.Content == "" {
		t.Error("View() Content is empty; want a no-document placeholder")
	}
}
