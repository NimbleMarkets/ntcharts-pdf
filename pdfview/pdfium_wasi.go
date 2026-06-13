// pdfview/pdfium_wasi.go: WASI builds (GOOS=wasip1 GOARCH=wasm) have
// no built-in renderer. wazero — which go-pdfium uses internally —
// can run inside WASI via its interpreter backend, but the compiler
// only targets amd64/arm64 hosts; an interpreted pdfium would be
// catastrophically slow for the use case. Hosts that need ImageMode
// here can wire up a custom RendererFactory (e.g. hitting a remote
// rasterizer API).
//
// The browser path (GOOS=js GOARCH=wasm) is handled separately in
// jsrenderer_js.go, a thin adapter over the booba-shim/pdfium package
// which bridges to the host page's @embedpdf/pdfium instance via
// syscall/js.

//go:build wasm && !js

package pdfview

// DefaultRendererFactory returns nil under WASI: see file header for
// rationale. Custom factories are responsible for their own resource
// caps; Config.Limits.MaxFileBytes is only honored by the default
// local-filesystem renderer.
func DefaultRendererFactory() RendererFactory { return nil }

// DefaultRendererFactoryWithLimits accepts the limits argument for API
// parity with the native and js builds but ignores it.
func DefaultRendererFactoryWithLimits(_ Limits) RendererFactory { return nil }
