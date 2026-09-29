module github.com/NimbleMarkets/ntcharts-pdf

go 1.26.8

// Awaiting upstream merge of WASM support
replace charm.land/bubbletea/v2 => github.com/neomantra/bubbletea/v2 v2.0.0-20260928192001-1b36865b418a

tool (
	github.com/NimbleMarkets/booba-shim/cmd/booba-shim-assets
	github.com/NimbleMarkets/go-booba/cmd/booba-assets
)

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.0.10
	charm.land/lipgloss/v2 v2.0.6
	github.com/NimbleMarkets/booba-shim v0.1.0
	github.com/NimbleMarkets/go-booba v0.7.0
	github.com/NimbleMarkets/ntcharts/v2 v2.4.0
	github.com/klippa-app/go-pdfium v1.21.0
	github.com/ledongthuc/pdf v0.0.0-20260907135840-6c8c28e0e8a0
)

require (
	github.com/NimbleMarkets/pixterm v0.0.0-20260501211346-dc18ac6c1a0f // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260928045949-bbf040aedf25 // indirect
	github.com/charmbracelet/x/ansi v0.11.8 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/disintegration/imaging v1.6.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jolestar/go-commons-pool/v2 v2.1.2 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/xo/terminfo v1.2.0 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
