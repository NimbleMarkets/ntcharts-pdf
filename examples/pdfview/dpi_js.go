// examples/pdfview/dpi_js.go: browser default DPI.
//
// @embedpdf/pdfium ships with a fixed wasm heap; rendering at the
// package default (300 DPI) on a Letter page allocates ~34 MB which
// has been seen to trap inside FPDF_RenderPageBitmap. 120 DPI keeps
// the bitmap under ~7 MB and gives readable Kitty output in the
// browser-WASM example.

//go:build js && wasm

package main

func browserDefaultRenderDPI() int { return 120 }
