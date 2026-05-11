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
}

// DefaultRendererFactory returns a RendererFactory that opens documents
// via go-pdfium's WASM backend. The pool is lazily initialised on the
// first call so programs that never enter ImageMode pay nothing.
//
// Returns nil if pool initialisation fails — that's the same "no renderer"
// degraded mode the WASM stub uses, so the host's status bar UX stays
// consistent across platforms.
func DefaultRendererFactory() RendererFactory {
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
			instance: inst,
			docRef:   doc.Document,
		}, nil
	}
}

// RenderPage rasterizes a 1-indexed page at the requested DPI. The
// returned image is a deep copy of pdfium's internal RGBA buffer — pdfium
// reclaims that buffer on Cleanup, so the copy is what makes the image
// safe to hold past the call.
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
