// examples/pdfview/main.go — single-pane pdfview demo.
//
// Loads the PDF named on the command line (defaulting to ./testdata/sample.pdf
// relative to wherever the binary runs) and renders it. Keys:
//
//	n/→/l       next page
//	p/←/h       prev page
//	m or t      toggle Text ↔ Image mode
//	g           toggle Glyph ↔ Kitty (Image mode only)
//	r           reload the current page (Image mode only)
//	?           toggle help
//	q / ctrl+c  quit
package main

import (
	_ "embed"
	"fmt"
	"os"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	booba "github.com/NimbleMarkets/go-booba"

	"github.com/NimbleMarkets/ntcharts-pdf/pdfview"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
)

// embeddedExample is the demo PDF compiled into the binary, used when
// the user runs the example without a path argument. Sized small (~270KB)
// so the binary stays reasonable. The same file is the canonical
// fixture for pdfview's integration tests.
//
//go:embed testdata/Example.pdf
var embeddedExample []byte

var (
	boxStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	errStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	badgeOkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	badgeOffStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	badgeWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// kittyBadge renders a compact indicator for the terminal's Kitty graphics
// capability. The probe takes a few ms to resolve so the initial state is
// "kitty:?".
func kittyBadge(cap picture.KittyCapability) string {
	switch cap {
	case picture.KittyCapabilitySupported:
		return badgeOkStyle.Render("kitty:✓")
	case picture.KittyCapabilityUnsupported:
		return badgeOffStyle.Render("kitty:✗")
	default:
		return badgeOffStyle.Render("kitty:?")
	}
}

// appKeys is the parent program's help / status surface. The pdfview widget
// owns the page/mode bindings; this struct only exists so the bubbles/help
// bubble has a single KeyMap to render from.
type appKeys struct {
	nav    key.Binding
	mode   key.Binding
	render key.Binding
	reload key.Binding
	zoom   key.Binding
	pan    key.Binding
	reset  key.Binding
	fit    key.Binding
	help   key.Binding
	quit   key.Binding
}

func newAppKeys() appKeys {
	return appKeys{
		nav:    key.NewBinding(key.WithKeys("n", "p", "h", "l"), key.WithHelp("n/p", "page")),
		mode:   key.NewBinding(key.WithKeys("m", "t"), key.WithHelp("m", "text/image")),
		render: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "glyph/kitty")),
		reload: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		zoom:   key.NewBinding(key.WithKeys("+", "-"), key.WithHelp("+/-", "zoom")),
		pan:    key.NewBinding(key.WithKeys("left", "right", "up", "down"), key.WithHelp("←↑↓→", "pan (zoomed)")),
		reset:  key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "reset")),
		fit:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fit mode")),
		help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k appKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.nav, k.zoom, k.pan, k.mode, k.fit, k.help, k.quit}
}

func (k appKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.nav, k.mode, k.render},
		{k.zoom, k.pan, k.reset},
		{k.fit, k.reload, k.help, k.quit},
	}
}

type model struct {
	pv            pdfview.Model
	help          help.Model
	keys          appKeys
	width, height int
}

func initialModel(cfg pdfview.Config) model {
	return model{
		pv:   pdfview.NewWithConfig(cfg),
		help: help.New(),
		keys: newAppKeys(),
	}
}

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m *model) resize() tea.Cmd {
	if m.width == 0 || m.height == 0 {
		return nil
	}
	m.help.SetWidth(m.width)
	// Help bubble height swings between 1 (short) and 3 (FullHelp) rows;
	// measure it after toggling so the pdfview shrinks instead of
	// scrolling the top of the box off-screen.
	helpH := lipgloss.Height(m.help.View(m.keys))
	innerW := m.width - 2
	innerH := m.height - helpH - 1 /* footer */ - 2 /* box borders */
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	return m.pv.SetSize(innerW, innerH)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.resize()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			// Close the renderer synchronously before we tell the
			// program to quit. Bubble Tea hands Update a copy of the
			// latest model, but the Renderer interface value inside
			// m.pv.cur is a (pointer, type) tuple — the underlying
			// *pdfiumRenderer is shared. Close acquires its mutex
			// (waiting for any in-flight render) and releases the
			// pdfium document handle + pool instance.
			_ = m.pv.Close()
			return m, tea.Quit
		case "?":
			m.help.ShowAll = !m.help.ShowAll
			return m, m.resize()

		// Modal arrow-key policy lives here, not in the widget:
		// "arrow at zoom 0 = page nav" is host-specific. When the
		// widget is actually zoomed, fall through to the widget so
		// its KeyMap dispatches the pan action.
		case "left":
			if m.pv.Mode() != pdfview.ImageMode || m.pv.Zoom() == 0 {
				return m, m.pv.PrevPage()
			}
		case "right":
			if m.pv.Mode() != pdfview.ImageMode || m.pv.Zoom() == 0 {
				return m, m.pv.NextPage()
			}
		}
	}

	// All other key + non-key messages flow through the widget. The
	// widget's Update routes KeyMsg through its KeyMap, calling the
	// matching action methods (NextPage / ToggleMode / ZoomIn / etc.).

	var cmd tea.Cmd
	m.pv, cmd = m.pv.Update(msg)
	return m, cmd
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("loading…")
	}
	// picture.Model embeds Kitty placement escapes directly inside View().Content,
	// so wrapping the content in a lipgloss border preserves them. Glyph mode
	// just contains styled half-block chars and composes cleanly.
	body := boxStyle.Render(m.pv.View().Content)

	modeName := "Text"
	if m.pv.Mode() == pdfview.ImageMode {
		modeName = "Image"
		if !m.pv.HasRenderer() {
			// ImageMode is selected but no Renderer is attached — either
			// no factory configured (typical on js/wasm), or the factory
			// errored. Surface the specific cause when we have one so
			// the user understands why pressing 'm' produced no change.
			// Sanitize the error text: factory errors can echo back
			// hostile filenames or PDF-derived bytes.
			label := "Image (no renderer)"
			if err := m.pv.RendererErr(); err != nil {
				label = fmt.Sprintf("Image (renderer error: %s)", pdfview.SanitizeForTerminal(err.Error()))
			}
			modeName = badgeWarnStyle.Render(label)
		}
	}
	renderName := "Glyph"
	if m.pv.RenderMode() == pdfview.RenderKitty {
		renderName = "Kitty"
	}
	zoomTag := ""
	if z := m.pv.Zoom(); z > 0 {
		zoomTag = fmt.Sprintf("  ×%d", 1<<z)
	}
	fitName := "Contain"
	switch m.pv.Fit() {
	case pdfview.FitFill:
		fitName = "Fill"
	case pdfview.FitCover:
		fitName = "Cover"
	}
	status := fmt.Sprintf("page %d/%d%s  %s  %s  fit:%s  %s",
		m.pv.Page(), m.pv.NumPages(), zoomTag, modeName, renderName, fitName, kittyBadge(m.pv.KittySupported()))
	if err := m.pv.Err(); err != nil {
		status = errStyle.Render(err.Error()) + "  " + status
	}
	footer := footerStyle.Width(m.width).Render(status)

	out := lipgloss.JoinVertical(lipgloss.Left, body, footer, m.help.View(m.keys))
	return tea.NewView(out)
}

func main() {
	cfg, err := initialConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Resource cleanup is wired into the Update handler for `q` /
	// `ctrl+c`, which calls pv.Close() synchronously before returning
	// tea.Quit. The Bubble Tea program model is value-passed, but the
	// Renderer interface holds a shared *pdfiumRenderer, so closing
	// through the local copy still releases the real document handle
	// and the wazero pool instance.
	if err := booba.Run(initialModel(cfg)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// initialConfig assembles the pdfview.Config the example launches with.
// argv[1] wins as a filesystem path; otherwise the embedded
// Example.pdf bytes are passed in directly via Config.InitialData —
// no temp file, no os.ReadFile, no filesystem entanglement.
//
// The browser build (GOOS=js GOARCH=wasm) ships with a fixed wasm heap
// inside @embedpdf/pdfium; 300 DPI on a Letter page wants ~34 MB just
// for the BGRA bitmap, which can push pdfium's auto-grown heap past
// where it copes well. browserDefaultRenderDPI() returns a tighter
// default for that path; native builds keep the package default (300).
func initialConfig() (pdfview.Config, error) {
	dpi := browserDefaultRenderDPI()
	if len(os.Args) > 1 {
		path := os.Args[1]
		if _, err := os.Stat(path); err != nil {
			return pdfview.Config{}, fmt.Errorf("argv[1] %q: %w", path, err)
		}
		return pdfview.Config{InitialPath: path, RenderDPI: dpi}, nil
	}
	return pdfview.Config{
		InitialData: embeddedExample,
		InitialName: "Example.pdf",
		RenderDPI:   dpi,
	}, nil
}
