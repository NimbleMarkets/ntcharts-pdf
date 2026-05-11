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
	"fmt"
	"os"
	"path/filepath"

	booba "github.com/NimbleMarkets/go-booba"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/NimbleMarkets/ntcharts/v2/picture"
	"github.com/NimbleMarkets/ntcharts-pdf/pdfview"
)

var (
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	badgeOkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	badgeOffStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
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

func initialModel(path string) model {
	cfg := pdfview.Config{InitialPath: path}
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
	path := defaultPath()
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "usage: pdfview <path-to.pdf>")
		os.Exit(1)
	}
	// booba.Run doesn't return the final model, so this example doesn't
	// call pv.Close() at exit — pdfium's pool and any open doc handles
	// are reclaimed by the OS at process exit. Hosts that embed pdfview
	// inside a longer-lived program should call pv.Close() on shutdown
	// to release the document handle eagerly.
	if err := booba.Run(initialModel(path)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// defaultPath finds a sample PDF to demo. Looks for the repo's bundled
// testdata first, then sample.pdf next to the binary.
func defaultPath() string {
	for _, p := range []string{
		"./testdata/Example.pdf",
		"../../testdata/Example.pdf",
		filepath.Join(filepath.Dir(os.Args[0]), "sample.pdf"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
