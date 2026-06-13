module github.com/NimbleMarkets/ntcharts-pdf

go 1.25.0

// Awaiting upstream merge of WASM support
replace charm.land/bubbletea/v2 => github.com/neomantra/bubbletea/v2 v2.0.0-20260506185856-6506c47fa2f3

tool (
	github.com/NimbleMarkets/booba-shim/cmd/booba-shim-assets
	github.com/NimbleMarkets/go-booba/cmd/booba-assets
)

require (
	charm.land/bubbles/v2 v2.1.0
	charm.land/bubbletea/v2 v2.0.6
	charm.land/lipgloss/v2 v2.0.3
	github.com/NimbleMarkets/booba-shim v0.1.0
	github.com/NimbleMarkets/go-booba v0.6.1-0.20260511134559-58814d532cc1
	github.com/NimbleMarkets/ntcharts/v2 v2.0.4-0.20260511142858-c7594fa69807
	github.com/klippa-app/go-pdfium v1.19.3
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
)

require (
	github.com/NimbleMarkets/pixterm v0.0.0-20260429102514-4e8bc7f0c8ee // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260511121909-c840852527f3 // indirect
	github.com/charmbracelet/x/ansi v0.11.7 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/disintegration/imaging v1.6.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jolestar/go-commons-pool/v2 v2.1.2 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.0 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/image v0.40.0 // indirect
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
