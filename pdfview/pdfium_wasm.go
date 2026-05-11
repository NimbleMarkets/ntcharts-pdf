// pdfview/pdfium_wasm.go: browser-WASM has no renderer. wazero (which
// go-pdfium uses internally) doesn't run inside browser-WASM — it needs
// host syscalls and the WASI snapshot, neither of which GOOS=js
// GOARCH=wasm supplies. ImageMode degrades to TextMode at View() time
// when DefaultRendererFactory returns nil.

//go:build js && wasm

package pdfview

// DefaultRendererFactory returns nil under js/wasm: there's no in-browser
// path to rasterize PDFs without an external service. Hosts that ship a
// server-side rasterizer can wire up a custom RendererFactory that hits
// their endpoint and returns an image.Image.
func DefaultRendererFactory() RendererFactory { return nil }

// DefaultRendererFactoryWithLimits accepts the limits argument for API
// parity with the native build but ignores it — there's no renderer to
// configure on js/wasm.
func DefaultRendererFactoryWithLimits(_ Limits) RendererFactory { return nil }
