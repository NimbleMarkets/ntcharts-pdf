// pdfview/jsrenderer_js.go: browser-WASM Renderer backed by
// @embedpdf/pdfium via the syscall/js bridge defined in jsbridge_js.go.
//
// Conforms to the same pdfview.Renderer contract as the native
// pdfium-via-wazero implementation: bytes-first factory, per-document
// instance, mutex-serialized RenderPage / Close. The Promise-based
// JS calls are awaited inside RenderPage so callers see a familiar
// blocking interface — Bubble Tea's renderPageCmd runs in its own
// goroutine, so the block is invisible to the program loop.

//go:build js && wasm

package pdfview

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sync"
	"syscall/js"
)

// jsRenderer holds one open document and the handle that
// @embedpdf/pdfium gave us for it. handleVal is the JS-side numeric
// identifier passed back to closeDocument / numPages / renderPage.
//
// mu serializes RenderPage and Close mirroring the native
// pdfiumRenderer's contract — the widget's SetPDF closes the previous
// Renderer synchronously on the UI goroutine, and that close must
// wait for any concurrent render that's awaiting a Promise to finish.
type jsRenderer struct {
	mu              sync.Mutex
	handleVal       js.Value
	maxRenderPixels int
	closed          bool
}

// DefaultRendererFactory returns a RendererFactory that delegates to
// the host page's @embedpdf/pdfium instance. The first call (per
// process) initialises the JS bridge; subsequent calls just open new
// documents against the already-loaded PDFium runtime.
//
// If the host hasn't wired up `window.ntchartsPDFium` (e.g. plain
// `go build` for the browser with no bridge script), every factory
// invocation returns an error and ImageMode degrades to TextMode.
func DefaultRendererFactory() RendererFactory {
	return DefaultRendererFactoryWithLimits(Limits{})
}

// DefaultRendererFactoryWithLimits is the same factory parameterised
// by resource caps. MaxRenderPixels is enforced inside the renderer
// (it queries page size via the bridge); MaxRenderDPI is enforced by
// the withLimits wrapper NewWithConfig applies upstream.
func DefaultRendererFactoryWithLimits(limits Limits) RendererFactory {
	limits.applyDefaults()
	return func(name string, data []byte) (Renderer, error) {
		if err := setupBridge(); err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("empty PDF data for %q", name)
		}
		u8 := goBytesToJS(data)
		handle, err := awaitPromise(bridgeAPI.Call("loadDocument", u8))
		if err != nil {
			return nil, fmt.Errorf("ntchartsPDFium.loadDocument %q: %w", name, err)
		}
		if !handle.Truthy() {
			return nil, fmt.Errorf("ntchartsPDFium.loadDocument %q: returned falsy handle", name)
		}
		return &jsRenderer{
			handleVal:       handle,
			maxRenderPixels: limits.MaxRenderPixels,
		}, nil
	}
}

// RenderPage rasterizes a 1-indexed page via the bridge and returns
// the result as an *image.RGBA. MaxRenderPixels enforcement: when
// the projected pixel count exceeds the budget, DPI is reduced to fit
// (graceful degradation matches the native implementation).
//
// The decoded RGBA bytes arrive in a JS Uint8ClampedArray; we copy
// them out via syscall/js.CopyBytesToGo into a fresh Go-side slice
// so the returned image isn't a stale reference into JS heap memory.
func (r *jsRenderer) RenderPage(pageNum, dpi int) (image.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("jsRenderer closed")
	}
	if pageNum < 1 {
		return nil, fmt.Errorf("invalid page %d (1-indexed)", pageNum)
	}
	if dpi <= 0 {
		dpi = DefaultRenderDPI
	}
	if r.maxRenderPixels > 0 {
		dpi = r.clampDPIToBudget(pageNum, dpi)
	}

	result, err := awaitPromise(
		bridgeAPI.Call("renderPage", r.handleVal, pageNum-1, dpi),
	)
	if err != nil {
		return nil, fmt.Errorf("ntchartsPDFium.renderPage page=%d: %w", pageNum, err)
	}
	w := result.Get("width").Int()
	h := result.Get("height").Int()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("renderPage page=%d: invalid dims %dx%d", pageNum, w, h)
	}
	data := result.Get("data")
	if !data.Truthy() {
		return nil, fmt.Errorf("renderPage page=%d: missing data buffer", pageNum)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	n := js.CopyBytesToGo(img.Pix, data)
	if n != len(img.Pix) {
		return nil, fmt.Errorf("renderPage page=%d: copied %d bytes, want %d (mismatched RGBA layout)",
			pageNum, n, len(img.Pix))
	}
	return img, nil
}

// clampDPIToBudget queries pdfium for the page's size in points and
// returns a DPI clamped so the rendered bitmap won't exceed
// r.maxRenderPixels. Returns the original DPI if the bridge can't
// supply page dims — best-effort, mirroring native behavior.
func (r *jsRenderer) clampDPIToBudget(pageNum, dpi int) int {
	sz := bridgeAPI.Call("pageSize", r.handleVal, pageNum-1)
	if !sz.Truthy() {
		return dpi
	}
	wPt := sz.Get("widthPt").Float()
	hPt := sz.Get("heightPt").Float()
	if wPt <= 0 || hPt <= 0 {
		return dpi
	}
	wIn := wPt / 72.0
	hIn := hPt / 72.0
	projected := wIn * hIn * float64(dpi) * float64(dpi)
	if projected <= float64(r.maxRenderPixels) {
		return dpi
	}
	maxDPI := math.Sqrt(float64(r.maxRenderPixels) / (wIn * hIn))
	if maxDPI < 1 {
		maxDPI = 1
	}
	return int(maxDPI)
}

// Close releases the document handle on the JS side. Idempotent;
// blocks on the mutex so a concurrent in-flight RenderPage finishes
// before the document is destroyed (matching the native renderer).
func (r *jsRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	bridgeAPI.Call("closeDocument", r.handleVal)
	r.handleVal = js.Null()
	return nil
}
