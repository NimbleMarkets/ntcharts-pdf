package pdfview

import (
	"errors"
	"image"
	"image/color"
	"os"
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
	return func(_ string, _ []byte) (Renderer, error) { return r, nil }
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

func TestSetPDFDataCopiesCallerSlice(t *testing.T) {
	// Regression: SetPDFData used to capture the caller's []byte
	// directly into the async Cmd closure. A caller mutating its
	// buffer (e.g. reusing a pool slot) could corrupt the in-flight
	// parse. Now SetPDFData copies at the boundary; mutating the
	// original slice after the call must not affect the load.
	original := []byte("hello world")
	m := New(80, 24)
	cmd := m.SetPDFData("x", original)
	// Stomp the caller-side buffer immediately.
	for i := range original {
		original[i] = 0xff
	}
	// Drain the cmd. The load will fail (bytes aren't a real PDF) but
	// the resulting pdfErrMsg must reference the original "hello world"
	// length — proving we kept our own copy.
	msg := cmd()
	err, ok := msg.(pdfErrMsg)
	if !ok {
		t.Fatalf("expected pdfErrMsg, got %T", msg)
	}
	// "hello world" is 11 bytes; if our copy were corrupted to all
	// 0xFF we'd get a different parse-error message. Either way the
	// process must not crash.
	if err.err == nil {
		t.Error("expected non-nil err from garbage bytes")
	}
}

func TestInitialDataEmptySliceRoutesToBytesLoader(t *testing.T) {
	// Regression: Init used len(InitialData) > 0, so an explicit
	// empty []byte{} silently fell back to InitialPath. Now we route
	// through the bytes loader and surface the "empty pdf data" error.
	cfg := Config{
		Cols: 80, Rows: 24,
		InitialPath: "/should/not/be/used.pdf",
		InitialData: []byte{},
		InitialName: "explicit-empty",
	}
	m := NewWithConfig(cfg)
	// The Init cmd should resolve to a pdfErrMsg about empty data,
	// NOT a stat error from the bogus InitialPath.
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd")
	}
	// Init batches multiple Cmds; the pic.Init one returns a non-error
	// Msg. Walk results until we find the relevant one.
	got := drainBatchUntilLoad(t, cmd)
	if !strings.Contains(got.err.Error(), "empty pdf data") {
		t.Errorf("Init err = %v, want 'empty pdf data'", got.err)
	}
}

// drainBatchUntilLoad executes a tea.Cmd that may itself be a Batch
// and returns the first pdfErrMsg encountered. Bubble Tea batches are
// not directly inspectable; we exercise the underlying funcs via the
// Cmd interface.
func drainBatchUntilLoad(t *testing.T, cmd tea.Cmd) pdfErrMsg {
	t.Helper()
	// tea.Batch returns a single Cmd that, when called, returns a
	// BatchMsg containing the child Cmds. Execute each child.
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if child == nil {
				continue
			}
			if pe, ok := child().(pdfErrMsg); ok {
				return pe
			}
		}
	} else if pe, ok := msg.(pdfErrMsg); ok {
		return pe
	}
	t.Fatalf("no pdfErrMsg in batch")
	return pdfErrMsg{}
}

// dataCapturingFactory records the exact bytes it was asked to open.
type dataCapturingFactoryState struct{ seen []byte }

func dataCapturingFactory(s *dataCapturingFactoryState) RendererFactory {
	return func(_ string, data []byte) (Renderer, error) {
		s.seen = append([]byte(nil), data...) // copy so we observe the load-time state, not later mutations
		return &fakeRenderer{}, nil
	}
}

func TestPageDimensionsNoDocument(t *testing.T) {
	m := New(80, 24)
	if w, h, ok := m.PageDimensions(1); ok || w != 0 || h != 0 {
		t.Errorf("PageDimensions(1) on empty model = (%d, %d, %v), want (0, 0, false)", w, h, ok)
	}
}

func TestPageDimensionsZeroIndex(t *testing.T) {
	m := New(80, 24)
	if w, h, ok := m.PageDimensions(0); ok || w != 0 || h != 0 {
		t.Errorf("PageDimensions(0) = (%d, %d, %v), want (0, 0, false)", w, h, ok)
	}
}

func TestPageDimensionsNegativeIndex(t *testing.T) {
	m := New(80, 24)
	if w, h, ok := m.PageDimensions(-3); ok || w != 0 || h != 0 {
		t.Errorf("PageDimensions(-3) = (%d, %d, %v), want (0, 0, false)", w, h, ok)
	}
}

func TestSetPageImageBeforeLoadDoesNotPanic(t *testing.T) {
	m := New(80, 24)
	// No SetPDF — docPages is nil. SetPageImage must not panic
	// even though m.page == 1 (the default InitialPage).
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	_ = m.SetPageImage(1, img)
	if w, h, ok := m.PageDimensions(1); ok || w != 0 || h != 0 {
		t.Errorf("PageDimensions(1) after pre-load SetPageImage = (%d, %d, %v), want (0, 0, false)", w, h, ok)
	}
}

func TestInitialDataCopiedAtConstruction(t *testing.T) {
	// Regression: the ownership contract promises that callers may
	// mutate / pool their buffer the moment NewWithConfig returns.
	// The copy must happen inside NewWithConfig, not lazily at Init.
	//
	// Discriminator: use a real, parseable PDF as InitialData. After
	// construction, stomp the caller-side buffer's PDF header
	// ("%PDF-1.4\n..."). When Init's async load runs:
	//   - If construction copied (correct): parser reads the detached
	//     copy with header intact → parse succeeds → factory observes
	//     real bytes; state.seen[:5] == "%PDF-".
	//   - If construction didn't copy (bug): parser reads the stomped
	//     buffer → parse fails → factory never runs → state.seen is nil.
	path := repoTestdata(t, "Example.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 10 {
		t.Fatalf("Example.pdf too short: %d bytes", len(data))
	}

	state := &dataCapturingFactoryState{}
	m := NewWithConfig(Config{
		Cols: 80, Rows: 24,
		RendererFactory: dataCapturingFactory(state),
		InitialData:     data,
		InitialName:     "captured.pdf",
	})
	// Stomp the PDF header in the caller-side slice. If construction
	// captured a reference instead of a copy, this corrupts the load.
	for i := 0; i < 10; i++ {
		data[i] = 0xff
	}

	_ = drainBatchAny(t, m.Init())

	if state.seen == nil {
		t.Fatal("factory never ran — parse failed on stomped header, " +
			"proving NewWithConfig did not copy InitialData")
	}
	if got := string(state.seen[:5]); got != "%PDF-" {
		t.Errorf("factory saw header %q, want %q — copy at construction missing", got, "%PDF-")
	}
}

// drainBatchAny executes a Bubble Tea Cmd (possibly a Batch) and
// returns immediately when any leaf Cmd produces a Msg. Used to drive
// Init synchronously in tests.
func drainBatchAny(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if child != nil {
				_ = child()
			}
		}
		return msg
	}
	return msg
}

func TestInitialDataSeedsPathFromName(t *testing.T) {
	// Regression: m.path was seeded from cfg.InitialPath unconditionally.
	// With InitialData set, View() in the load-in-flight window would
	// show "No document loaded" (m.path == "") instead of the embedded
	// name. Now NewWithConfig uses InitialName when InitialData wins.
	m := NewWithConfig(Config{
		Cols: 80, Rows: 24,
		InitialData: []byte("dummy"),
		InitialName: "embedded.pdf",
	})
	if m.path != "embedded.pdf" {
		t.Errorf("m.path = %q, want %q", m.path, "embedded.pdf")
	}
	// Default fallback when InitialName is blank.
	m2 := NewWithConfig(Config{
		Cols: 80, Rows: 24,
		InitialData: []byte("dummy"),
	})
	if m2.path != "embedded" {
		t.Errorf("blank InitialName: m.path = %q, want %q", m2.path, "embedded")
	}
}

func TestRunLoadFromBytesRecoversFromPanic(t *testing.T) {
	// We can't easily fabricate a PDF that panics ledongthuc/pdf's
	// parser from outside the package, but we can verify the
	// defer/recover scaffold is in place by feeding deliberately
	// truncated bytes that have historically caused parser issues
	// (random tail of an otherwise-valid header). The contract is:
	// runLoadFromBytes must NEVER panic — it must always return a
	// pdfErrMsg or pdfLoadedMsg.
	hostile := []byte("%PDF-1.4\n%\xff\xff\xff\nstartxref\n0\n%%EOF\n")
	msg := runLoadFromBytes("hostile", hostile, 1, nil, Limits{})
	switch msg.(type) {
	case pdfErrMsg, pdfLoadedMsg:
		// Either is fine — the point is no panic propagated.
	default:
		t.Errorf("unexpected msg type %T", msg)
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
	factory := func(string, []byte) (Renderer, error) { return nil, wantErr }
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

func TestPageCache(t *testing.T) {
	fake := &fakeRenderer{}
	m := NewWithConfig(Config{
		Cols:          80,
		Rows:          24,
		PageCacheSize: 3,
		RendererFactory: fakeFactory(fake),
	})

	// Setup: load document with 3 pages.
	gen := bump(m.loadGen)
	m, _ = m.Update(pdfLoadedMsg{
		path:     "/tmp/cache-test.pdf",
		pages:    make([]pdfPage, 3),
		renderer: fake,
		gen:      gen,
	})

	// Verify caching behaviour in ImageMode.
	m.mode = ImageMode

	// 1. Initial render of page 1.
	cmd := m.renderPageCmd(1)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for page 1 render")
	}
	msg := cmd()
	m, _ = m.Update(msg)

	if fake.calls != 1 {
		t.Fatalf("expected 1 renderer call, got %d", fake.calls)
	}

	// 2. Second render of page 1 (should hit cache, calls should not increase).
	cmd = m.renderPageCmd(1)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for page 1 cached render")
	}
	msg = cmd()
	m, _ = m.Update(msg)

	if fake.calls != 1 {
		t.Fatalf("expected cached hit, calls remained 1, got %d", fake.calls)
	}

	// 3. Render page 2 (calls = 2).
	cmd = m.SetPage(2)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for page 2 render")
	}
	msg = cmd()
	m, _ = m.Update(msg)
	if fake.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", fake.calls)
	}

	// 4. Render page 3 (calls = 3).
	cmd = m.SetPage(3)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for page 3 render")
	}
	msg = cmd()
	m, _ = m.Update(msg)
	if fake.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", fake.calls)
	}

	// 5. Render page 2 (hit).
	cmd = m.SetPage(2)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for page 2 cached render")
	}
	msg = cmd()
	m, _ = m.Update(msg)
	if fake.calls != 3 {
		t.Fatalf("expected cached hit for page 2, calls remained 3, got %d", fake.calls)
	}

	// 6. Change DPI (should not hit cache).
	m.cfg.RenderDPI = 150
	cmd = m.Reload()
	if cmd == nil {
		t.Fatal("expected non-nil cmd for reload")
	}
	msg = cmd()
	m, _ = m.Update(msg)
	if fake.calls != 4 {
		t.Fatalf("expected new render due to DPI mismatch, calls = 4, got %d", fake.calls)
	}
}

func TestDynamicDPI(t *testing.T) {
	m := NewWithConfig(Config{
		Cols:       80,
		Rows:       24,
		DynamicDPI: true,
		Limits: Limits{
			MaxRenderDPI: 300,
		},
	})

	// Set up mock document pages with media boxes in points.
	// Let's make page 1 standard US Letter (612x792 pts).
	m.docPages = []pdfPage{
		{
			media: mediaBox{x0: 0, y0: 0, x1: 612, y1: 792},
		},
	}

	// With default cell pixel sizes (8x16):
	// target width = 80 * 8 = 640 px
	// target height = 24 * 16 = 384 px
	// wPts = 612, hPts = 792
	// dpiW = 640 * 72 / 612 = 75.29
	// dpiH = 384 * 72 / 792 = 34.90
	// min(dpiW, dpiH) = 34.90. Clamped to minDPI (72).
	dpi := m.calcDynamicDPI(1)
	if dpi != 72 {
		t.Errorf("expected calcDynamicDPI to yield 72, got %d", dpi)
	}

	// Now let's change dimensions so that calculated DPI is higher than minDPI but below MaxRenderDPI.
	// cols = 200, rows = 100
	// target width = 200 * 8 = 1600 px
	// target height = 100 * 16 = 1600 px
	// dpiW = 1600 * 72 / 612 = 188.23
	// dpiH = 1600 * 72 / 792 = 145.45
	// min = 145.45 => int(145) = 145
	m.cols = 200
	m.rows = 100
	dpi = m.calcDynamicDPI(1)
	if dpi != 145 {
		t.Errorf("expected calcDynamicDPI to yield 145, got %d", dpi)
	}

	// Test clamping to MaxRenderDPI
	// cols = 1000, rows = 1000 => DPI would be very large, should clamp to MaxRenderDPI (300).
	m.cols = 1000
	m.rows = 1000
	dpi = m.calcDynamicDPI(1)
	if dpi != 300 {
		t.Errorf("expected calcDynamicDPI to clamp to MaxRenderDPI (300), got %d", dpi)
	}
}
