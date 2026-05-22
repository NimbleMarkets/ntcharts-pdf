// pdfview/config.go: configuration, key bindings, modes.

package pdfview

import (
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
)

// Mode selects how the current PDF page is presented.
type Mode int8

const (
	TextMode  Mode = iota // Extracted text with styled image placeholders
	ImageMode             // Rasterized page via picture.Model
)

// RenderMode is the underlying picture protocol for ImageMode. Aliased so
// callers can reach picture's constants (RenderGlyph, RenderKitty) without
// importing picture directly.
type RenderMode = picture.PictureMode

const (
	RenderGlyph = picture.PictureGlyph
	RenderKitty = picture.PictureKitty
)

// FitMode controls how the page image is mapped onto the cell rectangle.
// Re-exported from picture so callers can drive picture.Model behavior
// (Contain / Fill / Cover) through pdfview's API surface.
type FitMode = picture.FitMode

const (
	FitContain = picture.FitContain // preserve aspect ratio, letterbox (default)
	FitFill    = picture.FitFill    // stretch to fill the cell rectangle
	FitCover   = picture.FitCover   // preserve aspect ratio, crop to fill
)

// DefaultRenderDPI is the resolution passed to Renderer.RenderPage when
// Config.RenderDPI is unset. 300 DPI matches the new harness's target
// fidelity — high enough that Kitty graphics look sharp, low enough that
// each page rasterizes in well under a second on typical hardware.
const DefaultRenderDPI = 300

// Limits caps resource consumption when loading and rendering PDFs.
// Defaults (applied when a field is zero) are conservative for hosts
// that may handle untrusted documents — they keep a single document
// from monopolising memory / CPU. Setting a field to a negative value
// disables that cap entirely (use only with trusted input).
type Limits struct {
	// MaxFileBytes caps the byte size of the PDF file on disk that the
	// default factory will read into memory. Zero = 256 MiB default.
	MaxFileBytes int64

	// MaxPages caps the number of pages a PDF may report. Zero = 10000
	// default. A malformed or hostile PDF can claim millions of pages,
	// causing make([]pdfPage, n) to OOM before any page is read.
	MaxPages int

	// MaxImagesPerPage caps the number of XObject image placeholders
	// counted per page. Zero = 1024 default. Hostile /XObject dicts
	// with millions of entries can't burn the load thread.
	MaxImagesPerPage int

	// MaxRenderDPI caps the effective DPI passed to Renderer.RenderPage.
	// Zero = 600 default. Combined with MaxRenderPixels this puts a
	// hard ceiling on rasterized bitmap memory.
	MaxRenderDPI int

	// MaxRenderPixels caps the projected pixel count of a rasterized
	// page. The default renderer queries page dimensions before
	// rendering and reduces DPI when the projected output would exceed
	// this limit. Zero = 100 million pixel default (≈ 400 MB at RGBA).
	MaxRenderPixels int
}

// applyDefaults fills zero-valued fields with the package defaults.
// Negative values are preserved as the "unbounded" sentinel.
func (l *Limits) applyDefaults() {
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = 256 << 20 // 256 MiB
	}
	if l.MaxPages == 0 {
		l.MaxPages = 10_000
	}
	if l.MaxImagesPerPage == 0 {
		l.MaxImagesPerPage = 1024
	}
	if l.MaxRenderDPI == 0 {
		l.MaxRenderDPI = 600
	}
	if l.MaxRenderPixels == 0 {
		l.MaxRenderPixels = 100_000_000
	}
}

// KeyMap binds the widget's actions to keys. Disabled bindings (zero
// key.Binding) let host programs hand the corresponding action off to
// another widget.
//
// Arrow-key bindings under Pan* and the page-nav bindings Next/Prev
// intentionally overlap on `right`/`left`. Hosts decide which one wins
// based on context (typical: pan when ImageMode and zoom > 0, page nav
// otherwise — see examples/pdfview for the wiring).
type KeyMap struct {
	Next         key.Binding
	Prev         key.Binding
	ToggleMode   key.Binding
	ToggleRender key.Binding
	Reload       key.Binding
	ZoomIn       key.Binding
	ZoomOut      key.Binding
	ResetView    key.Binding
	CycleFit     key.Binding
	PanLeft      key.Binding
	PanRight     key.Binding
	PanUp        key.Binding
	PanDown      key.Binding
}

// DefaultKeyMap matches the bindings documented in the README status bar:
// n/→/l next, p/←/h prev, m or t toggle Text/Image, g toggle Glyph/Kitty,
// r reload current page, +/- zoom, arrows pan, 0 reset.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Next:         key.NewBinding(key.WithKeys("n", "l"), key.WithHelp("n/l", "next")),
		Prev:         key.NewBinding(key.WithKeys("p", "h"), key.WithHelp("p/h", "prev")),
		ToggleMode:   key.NewBinding(key.WithKeys("m", "t"), key.WithHelp("m", "text/image")),
		ToggleRender: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "glyph/kitty")),
		Reload:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		ZoomIn:       key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "zoom in")),
		ZoomOut:      key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "zoom out")),
		ResetView:    key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "reset view")),
		CycleFit:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fit mode")),
		PanLeft:      key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "pan left")),
		PanRight:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "pan right")),
		PanUp:        key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "pan up")),
		PanDown:      key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "pan down")),
	}
}

// Styles bundles all lipgloss styling the widget uses. The zero value of
// Styles yields uncolored defaults; DefaultStyles supplies sensible ones.
type Styles struct {
	Text        lipgloss.Style
	Placeholder lipgloss.Style
	Status      lipgloss.Style
	Error       lipgloss.Style
}

// DefaultStyles produces the styling used by the example program. Designed
// to look readable on both dark and light terminals — no hard-coded
// background colors.
func DefaultStyles() Styles {
	return Styles{
		Text: lipgloss.NewStyle(),
		Placeholder: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			Padding(0, 1).
			Foreground(lipgloss.Color("242")),
		Status: lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
		Error:  lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	}
}

// Config configures a Model at construction.
type Config struct {
	// Cols/Rows is the cell rectangle the widget renders into. Use SetSize
	// at runtime when the layout changes; these are the initial values.
	Cols, Rows int

	// InitialPath, if set, queues a path-based load via the Cmd returned
	// from Init(). Equivalent to calling SetPDF(InitialPath) after
	// construction. Ignored when InitialData is also set — bytes win.
	InitialPath string

	// InitialData, when non-nil, queues an in-memory load via the Cmd
	// returned from Init(). Equivalent to SetPDFData(InitialName,
	// InitialData). nil = "not supplied"; an explicit empty slice
	// routes to the bytes loader and surfaces "empty pdf data" through
	// pdfErrMsg.
	//
	// The byte slice is copied inside NewWithConfig, so callers may
	// reuse or mutate the underlying buffer the moment NewWithConfig
	// returns. Useful for `//go:embed`-ed fixtures and HTTP-fetched
	// documents.
	InitialData []byte

	// InitialName is the display label used when InitialData is set
	// (falls back to "embedded" when blank). Has no effect unless
	// InitialData != nil.
	InitialName string

	// InitialPage is the 1-indexed first page shown. Defaults to 1 when 0.
	InitialPage int

	// DefaultMode selects the initial mode (TextMode if zero).
	DefaultMode Mode

	// RendererFactory opens a PDF from bytes and returns a Renderer
	// bound to that document. When nil the constructor uses
	// DefaultRendererFactory, which resolves per build target:
	// go-pdfium (wazero) on native, the @embedpdf/pdfium JS bridge on
	// GOOS=js GOARCH=wasm, nil on GOOS=wasip1 GOARCH=wasm. When the
	// factory returns nil ImageMode falls back to TextMode at View()
	// time.
	RendererFactory RendererFactory

	// RenderDPI controls the rasterization resolution passed to the
	// Renderer. Zero falls back to DefaultRenderDPI.
	RenderDPI int

	// PageCacheSize controls the number of rasterized page images kept in
	// memory. Zero defaults to 3; set to a negative value to disable caching.
	PageCacheSize int

	// DynamicDPI automatically scales the rasterization DPI based on the
	// widget cell size (Cols/Rows) and the page point dimensions.
	DynamicDPI bool

	// CellPixelWidth and CellPixelHeight specify the pixel dimensions of a
	// single terminal cell, used only when DynamicDPI is true. If zero,
	// defaults to 8x16 pixels per cell.
	CellPixelWidth  int
	CellPixelHeight int

	// Limits caps resource consumption for untrusted documents. Zero-
	// valued fields use sensible defaults (see Limits doc); set a field
	// to -1 to disable that specific cap for trusted input.
	Limits Limits

	// PictureConfig is forwarded verbatim to the underlying picture.Model.
	PictureConfig picture.Config

	// Styles overrides the default lipgloss styling. Nil uses DefaultStyles.
	Styles *Styles
}
