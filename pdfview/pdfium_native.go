// pdfview/pdfium_native.go: go-pdfium (PDFium-via-wazero) renderer for
// native builds. CGO-free, zero system dependencies, ships the ~5 MB
// pdfium.wasm blob inside the Go binary.
//
// Build tag excludes browser-WASM targets: wazero needs host syscalls
// and threading that GOOS=js GOARCH=wasm doesn't provide.
// DefaultRendererFactory returns nil there (see pdfium_wasm.go), so
// ImageMode degrades to TextMode in the browser without complaint.

//go:build !js && !wasm

package pdfview

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// pdfium pool initialisation is process-wide and somewhat expensive
// (hundreds of ms cold — wazero compiles the embedded pdfium.wasm
// module). Pool creation is deferred to the first factory call via
// sync.Once and the pool is reused for every subsequent Renderer.
var (
	pdfiumOnce sync.Once
	pdfiumPool pdfium.Pool
	pdfiumErr  error
)

// pdfiumPoolConfig produces the Config used for the process-wide pool.
// MaxTotal:4 leaves room for a small number of concurrent renders (e.g.
// parallel page prefetch later) without ballooning memory; MinIdle:1
// keeps one warm worker ready so the first Render is nearly instant
// after the cold-start probe.
func pdfiumPoolConfig() webassembly.Config {
	return webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 4,
	}
}

// pdfiumRenderer holds a single open document and the pdfium instance
// that owns it. Each loaded PDF gets its own Renderer; the underlying
// instance is returned to the pool on Close.
//
// mu serializes RenderPage and Close. The widget's SetPDF closes the
// previous Renderer synchronously on the UI goroutine while an earlier
// renderPageCmd may still be running on its own goroutine — without the
// lock that's a data race on instance + pdfium-internal state and a
// likely UB. Render durations (hundreds of ms in pdfium) are short
// enough that blocking SetPDF on the in-flight call is acceptable.
type pdfiumRenderer struct {
	mu       sync.Mutex
	instance pdfium.Pdfium
	docRef   references.FPDF_DOCUMENT

	// maxRenderPixels caps the bitmap area of a single rasterized page.
	// 0 = disabled. When the projected pixel count at the caller-supplied
	// DPI exceeds the cap, RenderPage lowers the DPI in-place to fit
	// rather than failing — graceful degradation matches the rest of
	// the widget's "soft fail" defaults.
	maxRenderPixels int
}

// DefaultRendererFactory returns a RendererFactory that opens documents
// via go-pdfium's WASM backend with default Limits applied. The pool is
// lazily initialised on the first call so programs that never enter
// ImageMode pay nothing.
//
// For most callers, NewWithConfig handles this automatically (it wraps
// the returned factory with the file-size / DPI clamps too). Use
// DefaultRendererFactoryWithLimits if you need tuned per-renderer caps.
func DefaultRendererFactory() RendererFactory {
	return DefaultRendererFactoryWithLimits(Limits{})
}

// DefaultRendererFactoryWithLimits is the same factory parameterised by
// resource caps. The MaxRenderPixels cap is enforced inside the pdfium
// renderer (it's the only layer that knows the page's physical
// dimensions); MaxFileBytes / MaxRenderDPI are enforced by the
// withLimits wrapper NewWithConfig applies over the top.
func DefaultRendererFactoryWithLimits(limits Limits) RendererFactory {
	limits.applyDefaults()
	return func(path string) (Renderer, error) {
		pdfiumOnce.Do(func() {
			pdfiumPool, pdfiumErr = webassembly.Init(pdfiumPoolConfig())
		})
		if pdfiumErr != nil {
			return nil, fmt.Errorf("pdfium pool init: %w", pdfiumErr)
		}
		if pdfiumPool == nil {
			return nil, errors.New("pdfium pool not initialised")
		}

		// MaxFileBytes is enforced here, in the local-filesystem renderer,
		// rather than the universal factory wrapper — only filesystem
		// paths support os.Stat. Custom factories using URLs / IDs /
		// in-memory handles wouldn't benefit from a stat-based check.
		if limits.MaxFileBytes > 0 {
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("stat %q: %w", path, err)
			}
			if info.Size() > limits.MaxFileBytes {
				return nil, fmt.Errorf(
					"PDF %q is %d bytes, exceeds MaxFileBytes=%d",
					path, info.Size(), limits.MaxFileBytes,
				)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", path, err)
		}

		inst, err := pdfiumPool.GetInstance(30 * time.Second)
		if err != nil {
			return nil, fmt.Errorf("pdfium GetInstance: %w", err)
		}
		doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
		if err != nil {
			_ = inst.Close()
			return nil, fmt.Errorf("pdfium OpenDocument: %w", err)
		}
		return &pdfiumRenderer{
			instance:        inst,
			docRef:          doc.Document,
			maxRenderPixels: limits.MaxRenderPixels,
		}, nil
	}
}

// RenderPage rasterizes a 1-indexed page at the requested DPI. The
// returned image is a deep copy of pdfium's internal RGBA buffer — pdfium
// reclaims that buffer on Cleanup, so the copy is what makes the image
// safe to hold past the call.
//
// When maxRenderPixels is set, the renderer queries pdfium for the
// page's physical size and computes the projected pixel count at the
// requested DPI. If the projection exceeds the budget, DPI is reduced
// to fit (graceful degradation) — the user gets a chunkier render
// rather than an error.
func (r *pdfiumRenderer) RenderPage(pageNum, dpi int) (image.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.instance == nil {
		return nil, errors.New("pdfium renderer closed")
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
	resp, err := r.instance.RenderPageInDPI(&requests.RenderPageInDPI{
		Page: requests.Page{
			ByIndex: &requests.PageByIndex{
				Document: r.docRef,
				Index:    pageNum - 1,
			},
		},
		DPI: dpi,
	})
	if err != nil {
		return nil, fmt.Errorf("RenderPageInDPI page=%d: %w", pageNum, err)
	}
	defer resp.Cleanup()

	src := resp.Result.Image
	if src == nil {
		return nil, fmt.Errorf("RenderPageInDPI page=%d: nil image", pageNum)
	}
	// Deep-copy out of WASM-backed memory before Cleanup runs. Keep the
	// same Bounds and Stride so the copy is bit-identical.
	out := *src
	out.Pix = append([]byte(nil), src.Pix...)
	return &out, nil
}

// clampDPIToBudget queries pdfium for the page's size in points and
// returns a DPI clamped so that the rendered bitmap won't exceed
// r.maxRenderPixels. Returns the original DPI if the query fails — a
// missing page-size probe shouldn't block rendering, only stop our
// best-effort budget enforcement.
func (r *pdfiumRenderer) clampDPIToBudget(pageNum, dpi int) int {
	sz, err := r.instance.FPDF_GetPageSizeByIndex(&requests.FPDF_GetPageSizeByIndex{
		Document: r.docRef,
		Index:    pageNum - 1,
	})
	if err != nil || sz.Width <= 0 || sz.Height <= 0 {
		return dpi
	}
	wIn := sz.Width / 72.0
	hIn := sz.Height / 72.0
	projected := wIn * hIn * float64(dpi) * float64(dpi)
	if projected <= float64(r.maxRenderPixels) {
		return dpi
	}
	// max DPI satisfying wIn * hIn * DPI² <= budget
	maxDPI := math.Sqrt(float64(r.maxRenderPixels) / (wIn * hIn))
	if maxDPI < 1 {
		maxDPI = 1
	}
	return int(maxDPI)
}

// Close releases the document and returns the instance to the pool. The
// pool itself stays alive for the rest of the process; subsequent
// factory calls reuse it.
//
// Acquires r.mu, so Close blocks until any concurrent RenderPage call
// finishes. Idempotent — a second Close is a no-op.
func (r *pdfiumRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.instance == nil {
		return nil
	}
	_, _ = r.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: r.docRef})
	err := r.instance.Close()
	r.instance = nil
	return err
}
