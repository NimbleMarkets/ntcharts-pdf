// examples/pdfview/dpi_native.go: native default DPI.
//
// Returns 0 so pdfview.Config picks up its own default (DefaultRenderDPI
// = 300). Native builds have no heap-size issue since pdfium runs in
// the host's address space via wazero.

//go:build !js

package main

func browserDefaultRenderDPI() int { return 0 }
