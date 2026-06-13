// pdfview/jsrenderer_js.go: browser-WASM Renderer backed by
// booba-shim/pdfium (the @embedpdf/pdfium JS bridge installed by
// `go tool booba-shim-assets --shim=pdfium`).
//
// Conforms to the same pdfview.Renderer contract as the native
// pdfium-via-wazero implementation. Mutex serialization, idempotent
// Close, and the GC leak guard live in pdfium.Document; this file is
// only the 1-indexed-page / DPI-budget adapter.

//go:build js && wasm

package pdfview

import (
	"context"
	"fmt"
	"image"
	"math"

	"github.com/NimbleMarkets/booba-shim/pdfium"
)

// jsRenderer adapts a pdfium.Document to the Renderer interface.
type jsRenderer struct {
	doc             *pdfium.Document
	maxRenderPixels int
}

// DefaultRendererFactory returns a RendererFactory that delegates to
// the booba-shim/pdfium bridge. The first factory call (per process)
// initialises the JS bridge; subsequent calls just open new documents
// against the already-loaded PDFium runtime.
//
// If the host page hasn't loaded pdfium-shim.js, every factory
// invocation returns an error (pdfium.ErrBridgeMissing) and ImageMode
// degrades to TextMode.
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
		if len(data) == 0 {
			return nil, fmt.Errorf("empty PDF data for %q", name)
		}
		doc, err := pdfium.Load(context.Background(), data)
		if err != nil {
			return nil, fmt.Errorf("booba-shim/pdfium load %q: %w", name, err)
		}
		return &jsRenderer{doc: doc, maxRenderPixels: limits.MaxRenderPixels}, nil
	}
}

// RenderPage rasterizes a 1-indexed page. MaxRenderPixels enforcement:
// when the projected pixel count exceeds the budget, DPI is reduced to
// fit (graceful degradation matching the native implementation).
func (r *jsRenderer) RenderPage(pageNum, dpi int) (image.Image, error) {
	if pageNum < 1 {
		return nil, fmt.Errorf("invalid page %d (1-indexed)", pageNum)
	}
	if dpi <= 0 {
		dpi = DefaultRenderDPI
	}
	if r.maxRenderPixels > 0 {
		dpi = r.clampDPIToBudget(pageNum, dpi)
	}
	return r.doc.RenderPage(context.Background(), pageNum-1, dpi)
}

// clampDPIToBudget queries the page's size in points and returns a DPI
// clamped so the rendered bitmap won't exceed r.maxRenderPixels.
// Returns the original DPI if page dims are unavailable — best-effort,
// mirroring native behavior.
func (r *jsRenderer) clampDPIToBudget(pageNum, dpi int) int {
	wPt, hPt, err := r.doc.PageSize(pageNum - 1)
	if err != nil || wPt <= 0 || hPt <= 0 {
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

// Close releases the document. Idempotent; serialization against an
// in-flight RenderPage is handled inside pdfium.Document.
func (r *jsRenderer) Close() error { return r.doc.Close() }
