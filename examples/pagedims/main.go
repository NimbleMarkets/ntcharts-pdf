// examples/pagedims/main.go — aspect-matched cell rectangle demo.
//
// Shows how to use pdfview.Model.PageDimensions to size the widget's
// cell rectangle to a page's true aspect ratio, so picture.FitContain
// renders the page edge-to-edge with zero letterbox.
//
// Run:
//
//	go run ./examples/pagedims examples/pdfview/testdata/Example.pdf
//
// Keys:
//
//	n / p / arrows  page nav
//	q / ctrl+c      quit
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	booba "github.com/NimbleMarkets/go-booba"

	"github.com/NimbleMarkets/ntcharts-pdf/pdfview"
)

// cellPixelW / cellPixelH are the terminal cell pixel size the picture
// package defaults to. Real consumers should source these from their
// own picture.Config or a CSI 16t terminal query; the example uses the
// package defaults so the aspect formula is visible.
const (
	cellPixelW = 8
	cellPixelH = 16
)

type model struct {
	pv     pdfview.Model
	width  int
	height int
	cols   int
	rows   int
}

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if k := msg.String(); k == "q" || k == "ctrl+c" {
			_ = m.pv.Close()
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	var cmd tea.Cmd
	m.pv, cmd = m.pv.Update(msg)

	// Recompute the cell rectangle on every Update. PageDimensions
	// returns ok=false until the first pageRenderedMsg lands, so the
	// initial layout uses the full body; once dimensions arrive, the
	// next Update tick (a key, a frame, anything) tightens the rect.
	if cols, rows := m.aspectCellrect(); cols != m.cols || rows != m.rows {
		m.cols, m.rows = cols, rows
		cmd = tea.Batch(cmd, m.pv.SetSize(cols, rows))
	}
	return m, cmd
}

// aspectCellrect returns the largest cell rectangle inside the body
// whose aspect ratio matches the current page. Falls back to the full
// body when page dimensions aren't yet available.
func (m model) aspectCellrect() (cols, rows int) {
	bodyW, bodyH := m.width, m.height-1 // 1 row for footer
	if bodyW < 1 {
		bodyW = 1
	}
	if bodyH < 1 {
		bodyH = 1
	}
	pageW, pageH, ok := m.pv.PageDimensions(m.pv.Page())
	if !ok {
		return bodyW, bodyH
	}
	// Convert the page's pixel aspect to a cell aspect by dividing each
	// axis by its cell pixel size. cellPixelW < cellPixelH on typical
	// fonts, so a square pixel region maps to a taller-than-wide cell
	// region — exactly the asymmetry hosts need to compensate for.
	pageCellAR := float64(pageW*cellPixelH) / float64(pageH*cellPixelW)
	bodyCellAR := float64(bodyW) / float64(bodyH)
	if pageCellAR >= bodyCellAR {
		return bodyW, int(float64(bodyW) / pageCellAR)
	}
	return int(float64(bodyH) * pageCellAR), bodyH
}

func (m model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading…")
	}
	body := m.pv.View().Content
	status := "page dims: pending (waiting for first render)"
	if pageW, pageH, ok := m.pv.PageDimensions(m.pv.Page()); ok {
		status = fmt.Sprintf("page %d/%d  %dx%d px  →  cellrect %dx%d cells  (n/p, q quit)",
			m.pv.Page(), m.pv.NumPages(), pageW, pageH, m.cols, m.rows)
	}
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Width(m.width).Render(status)
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, body, footer))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pagedims <path-to.pdf>")
		os.Exit(1)
	}
	cfg := pdfview.Config{
		InitialPath: os.Args[1],
		DefaultMode: pdfview.ImageMode,
	}
	if err := booba.Run(model{pv: pdfview.NewWithConfig(cfg)}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
