// pdfview/jsbridge_js.go: syscall/js plumbing for the browser-WASM
// PDF renderer.
//
// Architecture: the host page loads `@embedpdf/pdfium` (or any
// compatible shim) and assigns a small adapter object to
// `window.ntchartsPDFium`. This file's setupBridge() function grabs
// that object once per process (sync.Once-gated, behind awaitPromise
// for the asynchronous init step) and stores its function references
// in package-level js.Values that the renderer reuses.
//
// The contract the host must provide (see web/pdfium-bridge.js for an
// example implementation):
//
//   window.ntchartsPDFium = {
//     init: () => Promise<void>,
//     loadDocument: (Uint8Array) => Promise<number>,        // returns doc handle
//     closeDocument: (number) => void,
//     numPages: (number) => number,
//     pageSize: (number, pageIdx) =>
//         { widthPt: number, heightPt: number },
//     renderPage: (number, pageIdx, dpi) => Promise<{
//         data: Uint8ClampedArray,   // RGBA, row-major, top-to-bottom
//         width: number, height: number,
//     }>,
//   };

//go:build js && wasm

package pdfview

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"
)

// Package-level handles populated by setupBridge. Access only after a
// successful setupBridge() call.
var (
	bridgeOnce sync.Once
	bridgeErr  error
	bridgeAPI  js.Value
)

// setupBridge initialises the JS-side @embedpdf/pdfium instance and
// caches the function references our renderer needs. Idempotent and
// safe to call from multiple goroutines; the underlying
// `ntchartsPDFium.init()` Promise only runs once per process.
func setupBridge() error {
	bridgeOnce.Do(func() {
		api := js.Global().Get("ntchartsPDFium")
		if !api.Truthy() {
			bridgeErr = errors.New(
				"window.ntchartsPDFium not present — the host page must load " +
					"web/pdfium-bridge.js (or equivalent) before launching this WASM binary")
			return
		}
		// init() returns a Promise<void> that resolves once
		// PDFium's wasm has been instantiated and FPDF_InitLibrary
		// (or equivalent) has run.
		if _, err := awaitPromise(api.Call("init")); err != nil {
			bridgeErr = fmt.Errorf("ntchartsPDFium.init: %w", err)
			return
		}
		bridgeAPI = api
	})
	return bridgeErr
}

// awaitPromise blocks the calling goroutine until the supplied JS
// Promise resolves or rejects, returning the resolved value or a
// stringified rejection error.
//
// Safe under GOOS=js: Go's scheduler yields to the JS event loop
// whenever a goroutine blocks on a channel receive, so the Promise's
// microtask gets a chance to run while we're parked on `<-done`. Do
// NOT call this from the program's main goroutine — Bubble Tea Cmds
// already run in their own goroutines, which is exactly the right
// place for this.
//
// Both then-/catch-callbacks are Released after the channel fires;
// failing to release leaks the js.Func table entry.
func awaitPromise(p js.Value) (js.Value, error) {
	if !p.Truthy() {
		return js.Null(), errors.New("awaitPromise: nil Promise")
	}
	done := make(chan struct{})
	var (
		result    js.Value
		rejectMsg string
		rejected  bool
	)
	var thenCb, catchCb js.Func
	thenCb = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			result = args[0]
		}
		close(done)
		return nil
	})
	catchCb = js.FuncOf(func(_ js.Value, args []js.Value) any {
		rejected = true
		switch {
		case len(args) == 0:
			rejectMsg = "Promise rejected with no value"
		case args[0].Truthy() && args[0].Get("message").Truthy():
			rejectMsg = args[0].Get("message").String()
		default:
			rejectMsg = args[0].String()
		}
		close(done)
		return nil
	})
	p.Call("then", thenCb).Call("catch", catchCb)
	<-done
	thenCb.Release()
	catchCb.Release()
	if rejected {
		return js.Null(), errors.New(rejectMsg)
	}
	return result, nil
}

// goBytesToJS allocates a Uint8Array on the JS side and copies src
// into it. Used to hand a PDF buffer over to @embedpdf/pdfium for
// loading. The returned js.Value is owned by JS — the GC there will
// reclaim it once nothing holds a reference; Go doesn't need to
// release it explicitly.
func goBytesToJS(src []byte) js.Value {
	u8 := js.Global().Get("Uint8Array").New(len(src))
	js.CopyBytesToJS(u8, src)
	return u8
}
