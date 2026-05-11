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

	// InitialPath, if set, queues a load via the Cmd returned from Init().
	// Equivalent to calling SetPDF(InitialPath) after construction.
	InitialPath string

	// InitialPage is the 1-indexed first page shown. Defaults to 1 when 0.
	InitialPage int

	// DefaultMode selects the initial mode (TextMode if zero).
	DefaultMode Mode

	// RendererFactory opens a PDF at the given path and returns a Renderer
	// bound to that document. When nil the constructor uses
	// DefaultRendererFactory; platforms without a built-in renderer (js/wasm)
	// transparently leave it nil and ImageMode falls back to TextMode at
	// View() time.
	RendererFactory RendererFactory

	// RenderDPI controls the rasterization resolution passed to the
	// Renderer. Zero falls back to DefaultRenderDPI.
	RenderDPI int

	// PictureConfig is forwarded verbatim to the underlying picture.Model.
	PictureConfig picture.Config

	// Styles overrides the default lipgloss styling. Nil uses DefaultStyles.
	Styles *Styles
}
