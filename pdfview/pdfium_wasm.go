// pdfview/pdfium_wasm.go: WASM environments have no built-in renderer.
// go-pdfium's wazero-based backend is used for native builds (see
// pdfium_native.go), but for GOARCH=wasm targets (browser or WASI) we
// gracefully degrade to TextMode.
//
// ImageMode remains available if the host wires up a custom
// RendererFactory (e.g. hitting a remote rasterizer API).

//go:build wasm

package pdfview

// DefaultRendererFactory returns nil on WASM targets (browser-js and
// WASI): wazero — which go-pdfium relies on — needs host syscalls and
// threading that GOARCH=wasm runtimes don't supply. Hosts that ship a
// server-side rasterizer can wire up a custom RendererFactory that
// hits their endpoint and returns an image.Image. Such custom
// factories are responsible for their own resource caps (file size,
// timeouts, etc.) — Config.Limits.MaxFileBytes is only honored by the
// default local-filesystem renderer.
func DefaultRendererFactory() RendererFactory { return nil }

// DefaultRendererFactoryWithLimits accepts the limits argument for API
// parity with the native build but ignores it — there's no renderer
// to configure on WASM targets.
func DefaultRendererFactoryWithLimits(_ Limits) RendererFactory { return nil }
