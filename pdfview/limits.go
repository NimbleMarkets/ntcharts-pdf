// pdfview/limits.go: RendererFactory wrapper that enforces caps that
// don't depend on a Renderer's storage model (currently just per-call
// DPI clamping). MaxFileBytes is enforced inside the default local
// renderer because file-size checks via os.Stat only make sense for
// filesystem paths — a custom factory might receive URLs, document
// IDs, in-memory handles, or browser blob refs where os.Stat would
// fail. MaxRenderPixels lives in the concrete renderer because only
// it can query page dimensions.

package pdfview

import "image"

// withLimits returns a RendererFactory that decorates `inner` with the
// path-agnostic resource caps (currently MaxRenderDPI). Custom
// factories opting out of DPI clamping can set Limits.MaxRenderDPI to
// -1; the file-size cap is not enforced by this wrapper, so factories
// using virtual paths work unmodified.
func withLimits(inner RendererFactory, limits Limits) RendererFactory {
	if inner == nil {
		return nil
	}
	return func(path string) (Renderer, error) {
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
