// pdfview/text.go: PDF loading, positioned text extraction, and the
// TextMode renderer that projects each text run onto a terminal cell grid.

package pdfview

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ledongthuc/pdf"
)

// mediaBox is a page's coordinate rectangle in PDF points. Origin is the
// bottom-left of the page; Y increases upward (PDF convention). Defaults
// to US Letter (612×792 pts) when a page declares no MediaBox.
type mediaBox struct{ x0, y0, x1, y1 float64 }

func (b mediaBox) width() float64  { return b.x1 - b.x0 }
func (b mediaBox) height() float64 { return b.y1 - b.y0 }

// pdfPage caches everything renderTextPage needs to lay out a single
// page. runs is the positioned text content; imageCount drives the
// placeholder UI; media gives the bounds we project into.
type pdfPage struct {
	runs       []pdf.Text
	media      mediaBox
	imageCount int
}

// pdfLoadedMsg is delivered when a SetPDF Cmd completes successfully.
// renderer is non-nil when the configured RendererFactory produced one
// for this load; the Model takes ownership of Closing it. rendererErr
// is non-nil when the factory was configured but errored — TextMode is
// fully usable in that case, but ImageMode degrades and hosts can show
// the error to explain why.
type pdfLoadedMsg struct {
	path        string
	pages       []pdfPage
	renderer    Renderer
	rendererErr error
	gen         uint64
}

// pdfErrMsg is delivered when a SetPDF Cmd fails.
type pdfErrMsg struct {
	err error
	gen uint64
}

// loadPDFFromPathCmd reads the file at path (subject to MaxFileBytes)
// and runs the bytes-based loader against the resulting in-memory copy.
// Returns a Cmd suitable for the Bubble Tea program loop.
func loadPDFFromPathCmd(path string, gen uint64, factory RendererFactory, limits Limits) tea.Cmd {
	return func() tea.Msg {
		if limits.MaxFileBytes > 0 {
			info, err := os.Stat(path)
			if err != nil {
				return pdfErrMsg{err: fmt.Errorf("stat %q: %w", path, err), gen: gen}
			}
			if info.Size() > limits.MaxFileBytes {
				return pdfErrMsg{
					err: fmt.Errorf("PDF %q is %d bytes, exceeds MaxFileBytes=%d",
						path, info.Size(), limits.MaxFileBytes),
					gen: gen,
				}
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return pdfErrMsg{err: fmt.Errorf("read %q: %w", path, err), gen: gen}
		}
		return runLoadFromBytes(path, data, gen, factory, limits)
	}
}

// loadPDFFromBytesCmd loads an in-memory PDF without touching the disk.
// name is the display label carried into pdfLoadedMsg.path so the
// status bar can identify the document.
func loadPDFFromBytesCmd(name string, data []byte, gen uint64, factory RendererFactory, limits Limits) tea.Cmd {
	return func() tea.Msg { return runLoadFromBytes(name, data, gen, factory, limits) }
}

// runLoadFromBytes is the shared body of both loaders. Parses the PDF
// from bytes, runs per-page extraction (recover-wrapped — see
// extractPage), and asks the factory for a Renderer.
//
// limits.MaxFileBytes is checked against len(data) here as well so the
// SetPDFData path can't bypass it.
// limits.MaxPages caps the pdfPage slice allocation; an attacker
// claiming billions of pages can't OOM us before the first read.
// Renderer-open failures don't fail the whole load — TextMode works
// fine; only ImageMode degrades. The factory's error rides through
// pdfLoadedMsg.rendererErr for hosts to surface.
//
// The named-return defer catches panics from anywhere in the parser
// path (pdf.NewReader, NumPage, r.Page(i) — extractPage has its own
// per-page recover, but pre-page calls are unguarded otherwise) and
// turns them into pdfErrMsg so the load goroutine never crashes the
// program.
func runLoadFromBytes(name string, data []byte, gen uint64, factory RendererFactory, limits Limits) (msg tea.Msg) {
	defer func() {
		if r := recover(); r != nil {
			msg = pdfErrMsg{
				err: fmt.Errorf("panic loading %q: %v", name, r),
				gen: gen,
			}
		}
	}()

	if limits.MaxFileBytes > 0 && int64(len(data)) > limits.MaxFileBytes {
		return pdfErrMsg{
			err: fmt.Errorf("PDF %q is %d bytes, exceeds MaxFileBytes=%d",
				name, len(data), limits.MaxFileBytes),
			gen: gen,
		}
	}
	if len(data) == 0 {
		return pdfErrMsg{err: errors.New("empty pdf data"), gen: gen}
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return pdfErrMsg{err: fmt.Errorf("parse %q: %w", name, err), gen: gen}
	}
	n := r.NumPage()
	if n <= 0 {
		return pdfErrMsg{err: errors.New("pdf reports zero pages"), gen: gen}
	}
	if limits.MaxPages > 0 && n > limits.MaxPages {
		return pdfErrMsg{
			err: fmt.Errorf("pdf reports %d pages, exceeds MaxPages=%d", n, limits.MaxPages),
			gen: gen,
		}
	}
	pages := make([]pdfPage, n)
	for i := 1; i <= n; i++ {
		pages[i-1] = extractPage(r.Page(i), limits)
	}

	var renderer Renderer
	var rendererErr error
	if factory != nil {
		renderer, rendererErr = factory(name, data)
	}
	return pdfLoadedMsg{
		path:        name,
		pages:       pages,
		renderer:    renderer,
		rendererErr: rendererErr,
		gen:         gen,
	}
}

// extractPage pulls positioned text + image-count from a single page,
// guarded by a recover so a malformed page can't crash the load Cmd.
// On panic it returns a zero-valued pdfPage with just the default media
// box, which renderTextPage handles cleanly as an empty page.
func extractPage(page pdf.Page, limits Limits) (out pdfPage) {
	out.media = defaultMediaBox()
	if page.V.IsNull() {
		return out
	}
	defer func() {
		if r := recover(); r != nil {
			// Reset to minimal-safe state. Don't propagate — the
			// reviewer's DoS hardening explicitly asked for per-page
			// failures to stay local.
			out = pdfPage{media: defaultMediaBox()}
		}
	}()
	out.media = pageMediaBox(page)
	out.runs = page.Content().Text
	out.imageCount = countPageImages(page, limits.MaxImagesPerPage)
	return out
}

// pageMediaBox walks the page-tree node chain looking for the first
// MediaBox declaration, mirroring the PDF spec's inheritance rule (a
// child page inherits MediaBox from its closest ancestor that declares
// one). Falls back to US Letter dimensions when the document is silent —
// chosen because it's the most common default and gives sane projections
// for most real-world PDFs.
func pageMediaBox(p pdf.Page) mediaBox {
	for cur := p.V; !cur.IsNull(); cur = cur.Key("Parent") {
		mb := cur.Key("MediaBox")
		if mb.Kind() != pdf.Array || mb.Len() != 4 {
			continue
		}
		return mediaBox{
			x0: mb.Index(0).Float64(),
			y0: mb.Index(1).Float64(),
			x1: mb.Index(2).Float64(),
			y1: mb.Index(3).Float64(),
		}
	}
	return defaultMediaBox()
}

func defaultMediaBox() mediaBox { return mediaBox{0, 0, 612, 792} }

// countPageImages walks /Resources/XObject and counts entries whose
// Subtype is "Image". Returns 0 when the dictionary is absent — image
// placeholders are a nice-to-have, never load-blocking. maxImages > 0
// short-circuits the count once the cap is reached; a hostile dict
// with millions of entries can't burn the load goroutine.
func countPageImages(p pdf.Page, maxImages int) int {
	xobj := p.Resources().Key("XObject")
	if xobj.IsNull() {
		return 0
	}
	count := 0
	for _, k := range xobj.Keys() {
		entry := xobj.Key(k)
		if entry.Key("Subtype").Name() == "Image" {
			count++
			if maxImages > 0 && count >= maxImages {
				return count
			}
		}
	}
	return count
}

// renderTextPage projects the current page's positioned text runs onto a
// (cols × rows) cell grid, preserving the document's spatial layout.
// Each run's leading character is placed at its projected (col, row);
// remaining characters advance one cell per rune.
//
// Collisions (multiple runs landing on the same cell) follow last-write-
// wins, matching the painter's-algorithm ordering of the PDF content
// stream. Out-of-bounds runs (or characters that overflow the right
// edge) are clipped.
//
// Image placeholders are appended below the grid as a styled bullet list
// — they don't share the PDF's coordinate space cleanly, and stacking
// them inline would collide with text runs.
func (m Model) renderTextPage() string {
	if m.page <= 0 || m.page > len(m.docPages) {
		return m.style.Status.Render("(empty)")
	}
	pg := m.docPages[m.page-1]

	cols, rows := m.cols, m.rows
	if cols <= 0 {
		cols = 80
	}

	// Render the placeholder block first so we know its actual height —
	// each bordered placeholder is 3 lines (top border, content, bottom
	// border), so a fixed "1 row per image" reservation truncates the
	// block. Measuring after rendering keeps grid + placeholders sized
	// to the available cell rectangle regardless of placeholder style.
	var placeholderBlock string
	phHeight := 0
	if pg.imageCount > 0 {
		placeholders := make([]string, 0, pg.imageCount)
		for i := 1; i <= pg.imageCount; i++ {
			placeholders = append(placeholders, m.style.Placeholder.Render(
				fmt.Sprintf("🖼  image %d (press 'm' to rasterize)", i),
			))
		}
		placeholderBlock = lipgloss.JoinVertical(lipgloss.Left, placeholders...)
		phHeight = strings.Count(placeholderBlock, "\n") + 1
	}

	gridRows := max(1, rows-phHeight)
	grid := layoutRuns(pg.runs, pg.media, cols, gridRows)
	body := joinGrid(grid)

	rendered := m.style.Text.Render(body)
	if placeholderBlock != "" {
		rendered = lipgloss.JoinVertical(lipgloss.Left, rendered, placeholderBlock)
	}
	// Final safety clamp: if the host's cell-rect is so short that even
	// one grid row plus the placeholder overflows, clip rather than
	// letting the over-tall output corrupt the parent layout.
	if m.rows > 0 {
		rendered = lipgloss.NewStyle().MaxHeight(m.rows).Render(rendered)
	}
	return rendered
}

// layoutRuns projects positioned text runs onto a (cols × rows) rune
// grid. Pure function for testability — exposed un-exported to keep the
// rendering layer separable from the lipgloss styling pass.
//
// ledongthuc/pdf typically emits one run per glyph (with X, Y, W, and
// FontSize), so naive per-run placement scatters the letters of a word
// across multiple cells. We bucket runs by projected row, sort each
// bucket left-to-right, and pack runs whose PDF-space X gap is below a
// font-relative threshold contiguously — collapsing per-glyph runs back
// into readable words while preserving the original inter-word and
// inter-column whitespace.
func layoutRuns(runs []pdf.Text, mb mediaBox, cols, rows int) [][]rune {
	grid := make([][]rune, rows)
	for i := range grid {
		grid[i] = make([]rune, cols)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}
	if cols <= 0 || rows <= 0 || mb.width() <= 0 || mb.height() <= 0 || len(runs) == 0 {
		return grid
	}

	type placedRun struct {
		col    int
		endX   float64 // PDF-space right edge: X + W
		startX float64 // PDF-space left edge: X
		emSize float64 // font size in points, drives the "is this a word break" threshold
		text   string
	}

	// Bucket by projected row. Within a bucket the order is whatever
	// order the PDF content stream emitted the runs; we re-sort by X
	// below, since content-stream order isn't guaranteed to be left-to-
	// right (think bidi, columns, or overprint).
	byRow := make(map[int][]placedRun, rows)
	for _, t := range runs {
		col := int((t.X-mb.x0)/mb.width()*float64(cols-1) + 0.5)
		row := int((mb.y1-t.Y)/mb.height()*float64(rows-1) + 0.5)
		if row < 0 || row >= rows {
			continue
		}
		byRow[row] = append(byRow[row], placedRun{
			col:    col,
			startX: t.X,
			endX:   t.X + t.W,
			emSize: t.FontSize,
			text:   t.S,
		})
	}

	for row, items := range byRow {
		sort.Slice(items, func(i, j int) bool { return items[i].startX < items[j].startX })

		cursor := 0
		for i, it := range items {
			// Gap policy: pack contiguously when the PDF-space gap to the
			// previous run is below 20 % of an em (heuristic — tight
			// enough that same-word glyphs always pack, loose enough that
			// most word-space gaps register as breaks). Larger gaps fall
			// back to honoring the projected column to preserve table /
			// multi-column layout.
			placeAt := it.col
			if i > 0 {
				prev := items[i-1]
				gap := it.startX - prev.endX
				if gap < prev.emSize*0.2 {
					placeAt = cursor
				} else if placeAt <= cursor {
					// Ensure at least one space between distinct words
					// when projection rounded them onto the same column.
					placeAt = cursor + 1
				}
			} else if placeAt < 0 {
				placeAt = 0
			}

			c := placeAt
			for _, r := range it.text {
				if c >= cols {
					break
				}
				// Sanitize per-rune at placement time: any non-printable
				// rune (control, format/bidi, surrogate, non-character)
				// is silently skipped so a malicious PDF can't reposition
				// the cursor or run Trojan-Source bidi tricks inside the
				// grid.
				if c >= 0 && unicode.IsPrint(r) {
					grid[row][c] = r
				}
				c++
			}
			cursor = c
		}
	}
	return grid
}

// joinGrid converts the rune grid to a string. Trailing spaces per row
// are preserved — they keep the cell rectangle's geometry stable so
// lipgloss border / Width operations don't reflow the layout.
func joinGrid(grid [][]rune) string {
	var b strings.Builder
	for i, row := range grid {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(row))
	}
	return b.String()
}
