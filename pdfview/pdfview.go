// Package pdfview is a Bubble Tea v2 widget that renders PDF documents in
// the terminal. It offers two modes:
//
//   - TextMode extracts plain text via ledongthuc/pdf (pure Go, WASM-safe)
//     and substitutes glyph placeholders for embedded images.
//   - ImageMode rasterizes each page via a per-document Renderer and
//     feeds the bitmap to ntcharts/v2/picture, which renders Kitty
//     graphics when the terminal supports it and falls back to half-
//     block glyphs otherwise.
//
// ImageMode is opt-in: callers either supply Config.RendererFactory or
// rely on DefaultRendererFactory, which selects the right backend per
// build target:
//   - native (CGO-free) → go-pdfium via wazero, with the embedded
//     pdfium.wasm blob
//   - GOOS=js GOARCH=wasm → bridges via syscall/js into the host page's
//     @embedpdf/pdfium instance (see web/pdfium-bridge.js)
//   - GOOS=wasip1 GOARCH=wasm → nil (wazero-in-WASI is unusably slow)
//
// Mirrors the wrapping idioms of the sibling ntcharts-osm/mapview widget:
// value-receiver Update returning (Model, tea.Cmd), View returning a
// tea.View so Kitty graphics can ride through, and a KeyMap a host program
// can override field-by-field.
package pdfview

import (
	"fmt"
	"image"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
)

// Model is the pdfview widget. Use New or NewWithConfig to construct.
type Model struct {
	cfg   Config
	keys  KeyMap
	style Styles

	pic        picture.Model
	cols, rows int

	path string
	page int // 1-indexed; defaults to Config.InitialPage before any load
	mode Mode

	// docPages holds the per-page positioned-text + image-count caches.
	// Indexed by page-1. NumPages() returns len(docPages).
	docPages []pdfPage

	// sourceImage is the un-cropped image returned by the most recent
	// Renderer.RenderPage call (or SetPageImage). nil until the first
	// successful render for the current document. Cropped on demand to
	// the (zoom, panX, panY) viewport before being handed to picture.Model.
	sourceImage image.Image

	// Viewport state. zoom 0 means "fit the whole page to the cell rect"
	// (no crop). zoom N (N >= 1) means crop the source to a 1/(2^N) ×
	// 1/(2^N) sub-rectangle; picture.Model then scales that back up to
	// the cell rect, producing the chunky/dithered look that lets you
	// see individual rasterized glyphs.
	//
	// panX, panY are the normalized [0, 1] coordinates of the visible
	// viewport's top-left corner inside the source image. Stored
	// normalized (not in pixels) so the viewport survives a re-render
	// at a different DPI / cell rect.
	zoom       int
	panX, panY float64

	// cur is the Renderer for the currently-loaded PDF, opened via
	// cfg.RendererFactory in the SetPDF load Cmd. nil while no document is
	// loaded or when the factory is nil (typical on js/wasm). Closed and
	// replaced by every successful SetPDF; closed by Model.Close.
	cur Renderer

	// rendererErr is the most recent factory error, persisted (unlike
	// err, which is cleared on the next successful op) so hosts can
	// keep displaying it for the lifetime of the current document.
	// Cleared on SetPDF.
	rendererErr error

	// loadGen / renderGen serialize async work. Every SetPDF bumps loadGen;
	// every page rasterize bumps renderGen. Stale msgs are dropped by
	// comparing generation counters in Update.
	//
	// Pointers so the counters survive Bubble Tea's value-receiver idiom —
	// Init / Update / View receive a copy of Model, but the shared *uint64
	// keeps every copy in sync with the live counter.
	loadGen   *uint64
	renderGen *uint64

	err error
}

// New constructs a Model with the given cell rectangle and default config.
func New(cols, rows int) Model {
	return NewWithConfig(Config{Cols: cols, Rows: rows})
}

// NewWithConfig constructs a Model from a Config. Zero-valued style fields
// are filled in from DefaultStyles.
func NewWithConfig(cfg Config) Model {
	if cfg.InitialPage <= 0 {
		cfg.InitialPage = 1
	}
	if cfg.RenderDPI <= 0 {
		cfg.RenderDPI = DefaultRenderDPI
	}
	cfg.Limits.applyDefaults()
	if cfg.RendererFactory == nil {
		cfg.RendererFactory = DefaultRendererFactoryWithLimits(cfg.Limits)
	}
	if cfg.RendererFactory != nil {
		cfg.RendererFactory = withLimits(cfg.RendererFactory, cfg.Limits)
	}
	// Detach InitialData from the caller's slice immediately. Init runs
	// asynchronously (later in the Bubble Tea program loop) and the
	// public ownership contract promises callers can mutate the buffer
	// the moment NewWithConfig returns. Doing the copy here guarantees
	// that; the copyBytes call inside Init becomes belt-and-suspenders.
	cfg.InitialData = copyBytes(cfg.InitialData)
	styles := DefaultStyles()
	if cfg.Styles != nil {
		styles = *cfg.Styles
	}

	var lg, rg uint64
	m := Model{
		cfg:   cfg,
		keys:  DefaultKeyMap(),
		style: styles,
		pic:   picture.NewWithConfig(cfg.PictureConfig),
		cols:  cfg.Cols,
		rows:  cfg.Rows,
		mode:  cfg.DefaultMode,
		page:  cfg.InitialPage,
		// path is the in-flight display label. Seed it from whichever
		// initial source will actually be loaded so View() can show
		// "Loading <name>…" or surface an error while the async load
		// is in flight — without this, a failed initial load hides
		// behind "No document loaded".
		path:      initialPathLabel(cfg),
		loadGen:   &lg,
		renderGen: &rg,
	}
	if c := m.pic.SetSize(cfg.Cols, cfg.Rows); c != nil {
		// SetSize during construction can't deliver a Cmd; the renderer
		// has nothing to render anyway. Drop the no-op.
		_ = c
	}
	return m
}

// Mode returns the current rendering mode.
func (m Model) Mode() Mode { return m.mode }

// RenderMode returns the underlying picture protocol used by ImageMode.
func (m Model) RenderMode() RenderMode { return m.pic.Mode() }

// Page returns the current 1-indexed page number. Before any document
// is loaded the value is Config.InitialPage (defaulting to 1) — there
// is no "0 = no document" sentinel; check NumPages() instead.
func (m Model) Page() int { return m.page }

// Name returns the display label for the currently-loaded document.
// For path-based loads it's whatever Path was supplied to SetPDF /
// Config.InitialPath (full path); for in-memory loads it's the name
// passed to SetPDFData / Config.InitialName (defaulting to "embedded").
// Empty when no document has been loaded. Hosts that want a short
// filename for status bars can apply filepath.Base() at the call site.
func (m Model) Name() string { return m.path }

// NumPages returns the page count of the loaded document, or 0.
func (m Model) NumPages() int { return len(m.docPages) }

// Err returns the last load or render error, cleared on the next successful
// operation. Hosts can surface this in their status bar.
func (m Model) Err() error { return m.err }

// KittySupported reports the terminal's Kitty graphics capability. The
// probe runs once per process (batched from Init via the underlying
// picture.Model); expect KittyCapabilityUnknown for the first few frames
// until the terminal responds (typically <50ms).
func (m Model) KittySupported() picture.KittyCapability { return m.pic.KittySupported() }

// HasRenderer reports whether an active Renderer is attached to the
// currently-loaded document. False means ImageMode requests will fall
// back to TextMode at View() time — either no factory is configured, or
// the factory was tried and errored (check RendererErr for the reason).
func (m Model) HasRenderer() bool { return m.cur != nil }

// RendererErr returns the most recent RendererFactory error, or nil. The
// load otherwise succeeded — TextMode works fine; only ImageMode is
// unavailable. Cleared on SetPDF.
func (m Model) RendererErr() error { return m.rendererErr }

// Close releases any renderer-side resources held by the currently-loaded
// document (e.g. pdfium document handle and pool instance). Safe to call
// when no document is loaded. Hosts should call this on shutdown when
// they want a clean exit; leaking the renderer at process exit is fine
// for short-lived TUIs.
func (m *Model) Close() error {
	if m.cur == nil {
		return nil
	}
	err := m.cur.Close()
	m.cur = nil
	return err
}

// KeyMap exposes the active bindings; mutate fields to rebind.
func (m *Model) KeyMap() *KeyMap { return &m.keys }

// SetSize updates the cell rectangle and forwards to the picture.Model.
// The returned Cmd may carry a Kitty re-placement frame; batch it through
// the program's tea.Cmd pipeline.
func (m *Model) SetSize(cols, rows int) tea.Cmd {
	m.cols, m.rows = cols, rows
	cmd := m.pic.SetSize(cols, rows)
	// Re-rasterize the current page at the new size when in ImageMode
	// and we have something loaded.
	if m.mode == ImageMode && m.path != "" && m.page > 0 {
		return tea.Batch(cmd, m.renderPageCmd(m.page))
	}
	return cmd
}

// SetPDFData loads an in-memory PDF and resets the current page to
// InitialPage. name is the display label shown in status bars; it is
// not parsed. Useful for embedded fixtures (`//go:embed`), HTTP-
// fetched documents, or any source where the caller already holds the
// bytes — no temp-file dance required.
//
// The supplied data slice is copied before being handed to the async
// load Cmd, so callers may safely reuse / mutate / pool the underlying
// buffer the moment SetPDFData returns. The copy lives only as long as
// the load Cmd's goroutine, then is released back to the garbage
// collector.
//
// See SetPDF for the equivalent path-based loader.
func (m *Model) SetPDFData(name string, data []byte) tea.Cmd {
	if m.cur != nil {
		_ = m.cur.Close()
		m.cur = nil
	}
	m.path = name
	m.page = m.cfg.InitialPage
	m.docPages = nil
	m.sourceImage = nil
	m.rendererErr = nil
	m.zoom, m.panX, m.panY = 0, 0, 0
	m.err = nil
	gen := bump(m.loadGen)
	bump(m.renderGen)
	// Defensive copy: the async load Cmd reads from `data` later, and
	// pdf.NewReader holds a ReaderAt over the bytes for the lifetime
	// of the load. Without the copy, a caller pooling buffers could
	// see corruption / race with parsing.
	return loadPDFFromBytesCmd(name, copyBytes(data), gen, m.cfg.RendererFactory, m.cfg.Limits)
}

// copyBytes is a small helper for the data-ownership defense at the
// SetPDFData / Init boundaries. Returns nil for nil input so callers
// can distinguish "no data supplied" from "empty data supplied".
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// SetPDF loads a PDF from disk and resets the current page to InitialPage.
// Returns a Cmd that performs text extraction + renderer open off the UI
// goroutine; on completion the widget's Update consumes a pdfLoadedMsg
// (or pdfErrMsg on failure). Closes any previously-loaded renderer
// synchronously before the new load dispatches.
func (m *Model) SetPDF(path string) tea.Cmd {
	if m.cur != nil {
		_ = m.cur.Close()
		m.cur = nil
	}
	m.path = path
	m.page = m.cfg.InitialPage
	m.docPages = nil
	m.sourceImage = nil
	m.rendererErr = nil
	// Reset viewport on document change. Per-page viewport persistence is
	// useful for flipping through a book at fixed zoom; switching docs
	// always starts fit-to-page.
	m.zoom, m.panX, m.panY = 0, 0, 0
	m.err = nil
	gen := bump(m.loadGen)
	// Invalidate any in-flight render from the previous document — without
	// this bump, a stale pageRenderedMsg whose page happens to match the
	// new document's current page would be accepted as fresh.
	bump(m.renderGen)
	return loadPDFFromPathCmd(path, gen, m.cfg.RendererFactory, m.cfg.Limits)
}

// SetPage jumps to the given 1-indexed page. Returns a render Cmd when
// ImageMode is active; in TextMode the cached text is shown immediately
// and the returned Cmd is nil.
func (m *Model) SetPage(n int) tea.Cmd {
	if len(m.docPages) == 0 {
		return nil
	}
	if n < 1 {
		n = 1
	}
	if n > len(m.docPages) {
		n = len(m.docPages)
	}
	if n == m.page {
		return nil
	}
	m.page = n
	m.sourceImage = nil
	if m.mode == ImageMode {
		return m.renderPageCmd(n)
	}
	return nil
}

// NextPage advances one page (clamped).
func (m *Model) NextPage() tea.Cmd { return m.SetPage(m.page + 1) }

// PrevPage moves back one page (clamped).
func (m *Model) PrevPage() tea.Cmd { return m.SetPage(m.page - 1) }

// ToggleMode swaps Text↔Image. Switching into ImageMode triggers a render
// of the current page; switching back to TextMode clears the picture image
// so the terminal doesn't leave a stale Kitty placement on screen.
func (m *Model) ToggleMode() tea.Cmd {
	if m.mode == TextMode {
		m.mode = ImageMode
		if m.path != "" && m.page > 0 {
			return m.renderPageCmd(m.page)
		}
		return nil
	}
	m.mode = TextMode
	// Drop any on-screen Kitty placement.
	return m.pic.SetImage(nil)
}

// ToggleRenderMode swaps Glyph↔Kitty for ImageMode rendering.
func (m *Model) ToggleRenderMode() tea.Cmd { return m.pic.Toggle() }

// Reload re-rasterizes the current page in ImageMode (no-op in TextMode).
func (m *Model) Reload() tea.Cmd {
	if m.mode != ImageMode || m.path == "" || m.page == 0 {
		return nil
	}
	return m.renderPageCmd(m.page)
}

// SetPageImage installs a caller-supplied image for the given 1-indexed
// page. Useful when the host has its own renderer pipeline (e.g. a server
// pre-rasterizes pages and ships PNG bytes). Replaces the current page's
// image when page == m.Page() and re-applies the current viewport.
func (m *Model) SetPageImage(page int, img image.Image) tea.Cmd {
	if page <= 0 || page != m.page {
		return nil
	}
	m.sourceImage = img
	return m.applyViewport()
}

// Zoom returns the current zoom level (0 = fit whole page; N >= 1 = each
// axis cropped to 1/2^N of the source).
func (m Model) Zoom() int { return m.zoom }

// ZoomIn doubles the visible magnification, recentering on the current
// viewport center. Caps at zoomMax to keep the cropped rectangle from
// disappearing entirely. No-op outside ImageMode or before a source
// image has been rendered.
func (m *Model) ZoomIn() tea.Cmd {
	if m.mode != ImageMode || m.sourceImage == nil || m.zoom >= zoomMax {
		return nil
	}
	cx, cy := m.viewportCenter()
	m.zoom++
	m.recenterTo(cx, cy)
	return m.applyViewport()
}

// ZoomOut halves the magnification (toward fit-to-page), recentering on
// the current viewport center. No-op when already at zoom 0.
func (m *Model) ZoomOut() tea.Cmd {
	if m.mode != ImageMode || m.sourceImage == nil || m.zoom <= 0 {
		return nil
	}
	cx, cy := m.viewportCenter()
	m.zoom--
	m.recenterTo(cx, cy)
	return m.applyViewport()
}

// PanLeft, PanRight, PanUp, PanDown shift the viewport by panStep of its
// own width / height. They no-op outside ImageMode, when no source is
// loaded, or when zoom == 0 (the whole page fits — nothing to pan into).
func (m *Model) PanLeft() tea.Cmd  { return m.pan(-1, 0) }
func (m *Model) PanRight() tea.Cmd { return m.pan(+1, 0) }
func (m *Model) PanUp() tea.Cmd    { return m.pan(0, -1) }
func (m *Model) PanDown() tea.Cmd  { return m.pan(0, +1) }

// Fit returns the current FitMode (how the image is mapped onto the cell
// rectangle when rendered by picture.Model).
func (m Model) Fit() FitMode { return m.pic.Fit() }

// CycleFit advances the FitMode through Contain → Fill → Cover → Contain.
// Returns picture.Model's re-render Cmd; safe to call in any mode (no-op
// effect when sourceImage is nil but the Cmd still bumps picture's seq).
func (m *Model) CycleFit() tea.Cmd {
	next := FitContain
	switch m.pic.Fit() {
	case FitContain:
		next = FitFill
	case FitFill:
		next = FitCover
	case FitCover:
		next = FitContain
	}
	return m.pic.SetFit(next)
}

// ResetView snaps zoom to 0 and pan to the page origin (i.e. fit-to-rect
// with no crop). Useful as a "0" hotkey escape hatch.
func (m *Model) ResetView() tea.Cmd {
	if m.mode != ImageMode || m.sourceImage == nil {
		m.zoom, m.panX, m.panY = 0, 0, 0
		return nil
	}
	if m.zoom == 0 && m.panX == 0 && m.panY == 0 {
		return nil
	}
	m.zoom, m.panX, m.panY = 0, 0, 0
	return m.applyViewport()
}

// Init returns a Cmd that kicks off the initial load (if either
// Config.InitialData or Config.InitialPath is set) plus the picture
// model's own init (Kitty capability probe). The pdfLoadedMsg
// delivered later populates m.docPages and friends via Update — no
// value-receiver mutation needed here.
//
// InitialData wins over InitialPath when present, including the
// explicit empty-slice case (InitialData: []byte{} routes to the
// bytes loader, which surfaces "empty pdf data" through pdfErrMsg).
// nil = "not supplied"; non-nil = "use these bytes".
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.pic.Init()}
	switch {
	case m.cfg.InitialData != nil:
		gen := bump(m.loadGen)
		name := initialName(m.cfg)
		// Defensive copy at the boundary; see SetPDFData for rationale.
		cmds = append(cmds, loadPDFFromBytesCmd(name, copyBytes(m.cfg.InitialData), gen, m.cfg.RendererFactory, m.cfg.Limits))
	case m.cfg.InitialPath != "":
		gen := bump(m.loadGen)
		cmds = append(cmds, loadPDFFromPathCmd(m.cfg.InitialPath, gen, m.cfg.RendererFactory, m.cfg.Limits))
	}
	return tea.Batch(cmds...)
}

// initialName resolves the display label used for an in-memory initial
// load. Falls back to "embedded" when the caller didn't supply one.
func initialName(cfg Config) string {
	if cfg.InitialName != "" {
		return cfg.InitialName
	}
	return "embedded"
}

// initialPathLabel picks the display label seeded into m.path at
// construction time. Mirrors Init's "InitialData wins" precedence so
// the in-flight Loading message and any pre-load error match the
// source that's actually being loaded.
func initialPathLabel(cfg Config) string {
	if cfg.InitialData != nil {
		return initialName(cfg)
	}
	return cfg.InitialPath
}

// bump increments *p and returns the new value. Sole writer is the UI
// goroutine (Bubble Tea Update loop), so no atomic ordering is needed —
// the pointer indirection exists only so value-receiver method copies
// share a single counter.
func bump(p *uint64) uint64 {
	*p++
	return *p
}

// Update routes messages through the picture.Model, consumes the
// widget's own async load/render notifications, and dispatches the
// configured KeyMap bindings.
//
// Key dispatch is intentionally non-modal: each binding maps to one
// action method. Pan bindings no-op at zoom 0 (nothing to pan into);
// hosts that want "arrow at zoom 0 = page nav" can pre-handle arrow
// keys in their own Update before forwarding to ours, or override
// KeyMap.Next/Prev to include the arrow keys.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if cmd := m.dispatchKey(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case pdfLoadedMsg:
		if msg.gen != *m.loadGen {
			// Stale load: a newer SetPDF superseded it. Close the Renderer
			// the stale load built so we don't leak its document handle /
			// pdfium pool instance.
			if msg.renderer != nil {
				_ = msg.renderer.Close()
			}
			break
		}
		m.path = msg.path
		m.docPages = msg.pages
		m.cur = msg.renderer
		m.rendererErr = msg.rendererErr
		m.err = nil
		if m.page > len(m.docPages) {
			m.page = len(m.docPages)
		}
		if m.page < 1 {
			m.page = 1
		}
		if m.mode == ImageMode {
			cmds = append(cmds, m.renderPageCmd(m.page))
		}

	case pdfErrMsg:
		if msg.gen == *m.loadGen {
			m.err = msg.err
		}

	case pageRenderedMsg:
		if msg.gen != *m.renderGen || msg.loadGen != *m.loadGen || msg.page != m.page {
			break // stale doc, stale render, or page changed under us
		}
		m.sourceImage = msg.img
		m.err = nil
		if c := m.applyViewport(); c != nil {
			cmds = append(cmds, c)
		}

	case pageRenderErrMsg:
		if msg.gen == *m.renderGen && msg.loadGen == *m.loadGen && msg.page == m.page {
			m.err = msg.err
		}
	}

	// Forward all messages — including the ones we just handled — to the
	// picture model so it can react to Kitty frame applications and resize
	// notifications. picture.Update is a no-op for messages it doesn't
	// recognize.
	if c := m.pic.Update(msg); c != nil {
		cmds = append(cmds, c)
	}

	return m, tea.Batch(cmds...)
}

// dispatchKey resolves a tea.KeyMsg against m.keys and calls the
// matching action method. Returns the Cmd from that method (possibly
// nil); the caller batches it into Update's outgoing cmd slice. No-op
// when no binding matches — non-bound keys are simply ignored,
// leaving hosts free to handle them at a higher layer.
func (m *Model) dispatchKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Next):
		return m.NextPage()
	case key.Matches(msg, m.keys.Prev):
		return m.PrevPage()
	case key.Matches(msg, m.keys.ToggleMode):
		return m.ToggleMode()
	case key.Matches(msg, m.keys.ToggleRender):
		return m.ToggleRenderMode()
	case key.Matches(msg, m.keys.Reload):
		return m.Reload()
	case key.Matches(msg, m.keys.ZoomIn):
		return m.ZoomIn()
	case key.Matches(msg, m.keys.ZoomOut):
		return m.ZoomOut()
	case key.Matches(msg, m.keys.ResetView):
		return m.ResetView()
	case key.Matches(msg, m.keys.CycleFit):
		return m.CycleFit()
	case key.Matches(msg, m.keys.PanLeft):
		return m.PanLeft()
	case key.Matches(msg, m.keys.PanRight):
		return m.PanRight()
	case key.Matches(msg, m.keys.PanUp):
		return m.PanUp()
	case key.Matches(msg, m.keys.PanDown):
		return m.PanDown()
	}
	return nil
}

// renderPageCmd returns a Cmd that dispatches a page render via the
// currently-open Renderer. Returns nil when there is no Renderer (no
// factory wired up, or the load Cmd hasn't completed yet) or no path
// loaded; both cases are legitimate "ImageMode degrades to text"
// fallbacks handled by View.
//
// The capture of *m.loadGen + bumped renderGen pins the render to a
// specific (document, request) pair. The renderer pointer itself is
// also captured, so a SetPDF-triggered Close on the previous Renderer
// must serialize with this in-flight RenderPage call — see
// pdfiumRenderer's sync.Mutex.
func (m *Model) renderPageCmd(page int) tea.Cmd {
	if m.cur == nil || m.path == "" {
		return nil
	}
	gen := bump(m.renderGen)
	loadGen := *m.loadGen
	r := m.cur
	dpi := m.cfg.RenderDPI
	return func() tea.Msg {
		img, err := r.RenderPage(page, dpi)
		if err != nil {
			return pageRenderErrMsg{err: err, page: page, gen: gen, loadGen: loadGen}
		}
		return pageRenderedMsg{page: page, img: img, gen: gen, loadGen: loadGen}
	}
}

// View renders the current page according to the active mode. ImageMode
// falls back to TextMode when no renderer is wired up or no image has
// been rasterized yet — that's the WASM-friendly graceful degradation.
func (m Model) View() tea.View {
	// Error first: a load failure should not hide behind "No document
	// loaded" just because m.path is unset (Init's load fail leaves it
	// blank in older builds — and even with the NewWithConfig fix, an
	// errored explicit SetPDF clears docPages and we want the error,
	// not the empty-doc placeholder, to win).
	//
	// Sanitize externally-derived strings (path, err) before they reach
	// the terminal: a hostile filename or PDF parse error can carry
	// terminal control sequences or bidi format chars otherwise.
	if m.err != nil && len(m.docPages) == 0 {
		return tea.NewView(m.placeholderView(m.style.Error,
			fmt.Sprintf("error: %s", SanitizeForTerminal(m.err.Error()))))
	}
	if m.path == "" {
		return tea.NewView(m.placeholderView(m.style.Status, "No document loaded"))
	}
	if len(m.docPages) == 0 {
		// Path set but pages not yet populated — async load in flight.
		return tea.NewView(m.placeholderView(m.style.Status,
			fmt.Sprintf("Loading %s…", SanitizeForTerminal(m.path))))
	}
	if m.mode == ImageMode && m.sourceImage != nil {
		// picture.View()'s content is only as tall as the rendered image
		// needs — Kitty mode emits the placement as a short escape +
		// trailing blank lines that may not reach m.rows, and FitContain
		// can letterbox without padding the vertical axis. Pinning the
		// returned content to (cols × rows) via lipgloss.Place keeps the
		// host's surrounding border at a stable size across mode and
		// fit-mode transitions; the existing image content is preserved
		// (Place doesn't truncate when target dims meet the content's
		// natural size).
		pv := m.pic.View()
		if m.cols > 0 && m.rows > 0 {
			pv.Content = lipgloss.Place(m.cols, m.rows, lipgloss.Left, lipgloss.Top, pv.Content)
		}
		return pv
	}
	// Either TextMode, or ImageMode with no rasterized image yet. Size
	// the rendered text to the same (cols × rows) envelope so a
	// TextMode-while-rasterize-pending frame doesn't collapse the host
	// box between toggle and pageRenderedMsg.
	content := m.renderTextPage()
	if m.cols > 0 && m.rows > 0 {
		content = lipgloss.Place(m.cols, m.rows, lipgloss.Left, lipgloss.Top, content)
	}
	return tea.NewView(content)
}

// placeholderView renders a single-line message centered inside the
// widget's cell rectangle, so the host's surrounding border stays at
// its allotted size while a load is in flight or before any document
// has been supplied. Falls back to the bare styled text when SetSize
// hasn't been called yet (m.cols / m.rows still zero).
func (m Model) placeholderView(base lipgloss.Style, text string) string {
	if m.cols <= 0 || m.rows <= 0 {
		return base.Render(text)
	}
	return base.
		Width(m.cols).
		Height(m.rows).
		Align(lipgloss.Center, lipgloss.Center).
		Render(text)
}
