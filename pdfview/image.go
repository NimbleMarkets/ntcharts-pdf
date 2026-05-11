// pdfview/image.go: ImageMode plumbing.
//
// The widget owns a per-document Renderer whose lifetime is tied to the
// loaded PDF — SetPDF closes the previous one and opens a new one via the
// configured RendererFactory. Concrete factories (pdfium WASM today,
// future swap-ins) live in build-tagged sibling files so the browser-WASM
// build stays free of host-dependent code.

package pdfview

import "image"

// Renderer rasterizes pages of a single loaded PDF document. Implementations
// are constructed via a RendererFactory (which opens the document) and
// released via Close.
//
// pageNum is 1-indexed to match the public Model API.
type Renderer interface {
	RenderPage(pageNum, dpi int) (image.Image, error)
	Close() error
}

// RendererFactory opens a PDF at path and returns a Renderer bound to it.
// The widget invokes the factory on every SetPDF call; nil is a valid
// return when the platform has no renderer available (e.g. js/wasm),
// which causes ImageMode to gracefully degrade to TextMode.
type RendererFactory func(path string) (Renderer, error)

// pageRenderedMsg is delivered when a Renderer.RenderPage call succeeds.
// gen / loadGen are checked against the Model's current counters in
// Update to drop stale results — gen alone suffices today (SetPDF bumps
// renderGen), but tagging with loadGen too is defense-in-depth: it
// guarantees that a render produced by a previous document's Renderer
// can never be accepted for the current document even if the gen
// counters happen to wrap.
type pageRenderedMsg struct {
	page    int
	img     image.Image
	gen     uint64
	loadGen uint64
}

// pageRenderErrMsg is delivered when a Renderer.RenderPage call fails.
type pageRenderErrMsg struct {
	page    int
	err     error
	gen     uint64
	loadGen uint64
}
