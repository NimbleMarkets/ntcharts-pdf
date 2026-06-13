# CHANGELOG

## v0.3.0 (2026-06-13)

- **Browser renderer via booba-shim**: The browser-WASM backend now uses
  [`booba-shim/pdfium`](https://github.com/NimbleMarkets/booba-shim)
  instead of a private JS bridge. 

## v0.2.0 (2026-05-22)

### Added
- **Page-Image Cache**: Added a configurable LRU page-image cache for `ImageMode` to optimize page navigation transitions (`PageCacheSize` config option).
- **Dynamic DPI Auto-Scaling**: Added dynamic DPI calculations based on terminal window cell dimensions to improve rendering clarity (`DynamicDPI` config option).

### Changed
- **Resource Finalization**: Upgraded legacy finalization to use Go 1.24's robust `runtime.AddCleanup` for safer background cleanup of JS, native, and WASM Pdfium documents.
- **Test Suite Isolation**: Separated slow WASM integration tests into an isolated test suite (`-tags=integration`) and added a fast local unit test suite (runs in ~0.3s).

### Fixed
- **Terminal Redraws**: Enabled `AltScreen` in full-screen interactive example programs to fix VS Code terminal redraw and scrollback overlap issues.

## v0.1.0 (2026-05-12)

  * Initial release

