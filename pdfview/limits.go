// pdfview/limits.go: RendererFactory wrapper that enforces resource caps.
//
// withLimits is platform-independent — it wraps any RendererFactory
// (the default pdfium one, a user's custom factory, the test fake)
// with a thin layer that performs a pre-open file-size check and
// clamps the per-call DPI parameter. MaxRenderPixels enforcement is
// the renderer's responsibility because only it knows the page's
// physical dimensions; the pdfium renderer queries pdfium directly
// and reduces DPI to fit the budget rather than failing the call.

package pdfview

import (
	"fmt"
	"image"
	"os"
)

// withLimits returns a RendererFactory that decorates `inner` with the
// hardening checks specified by `limits`. Each opened Renderer is
// wrapped in a limitedRenderer that clamps RenderPage's dpi parameter
// before forwarding the call. NewWithConfig calls this automatically;
// hosts wiring up a custom factory get the same protections.
func withLimits(inner RendererFactory, limits Limits) RendererFactory {
	if inner == nil {
		return nil
	}
	return func(path string) (Renderer, error) {
		if limits.MaxFileBytes > 0 {
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			if info.Size() > limits.MaxFileBytes {
				return nil, fmt.Errorf(
					"PDF %q is %d bytes, exceeds MaxFileBytes=%d",
					path, info.Size(), limits.MaxFileBytes,
				)
			}
		}
		r, err := inner(path)
		if err != nil {
			return nil, err
		}
		if r == nil {
			return nil, nil
		}
		return &limitedRenderer{inner: r, limits: limits}, nil
	}
}

// limitedRenderer enforces per-call DPI clamping. MaxRenderPixels is
// best handled inside the concrete renderer (it needs page dimensions),
// so this wrapper only clamps DPI; the pdfium renderer additionally
// applies the pixel-budget check internally.
type limitedRenderer struct {
	inner  Renderer
	limits Limits
}

func (l *limitedRenderer) RenderPage(pageNum, dpi int) (image.Image, error) {
	if l.limits.MaxRenderDPI > 0 && dpi > l.limits.MaxRenderDPI {
		dpi = l.limits.MaxRenderDPI
	}
	return l.inner.RenderPage(pageNum, dpi)
}

func (l *limitedRenderer) Close() error { return l.inner.Close() }
