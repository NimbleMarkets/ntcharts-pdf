# examples/pdfview/demo.tape — VHS recording for the pdfview demo gif.
#
# Run via `task vhs` from the repo root. VHS renders into a non-Kitty
# terminal emulator, so this tape captures the Glyph rendering path only —
# the same pdfium-rasterized images you'd see in a Kitty terminal, just
# displayed via half-block ANSI blocks. The 'g' toggle is included for
# completeness but the visual change is minimal under Glyph.

Output examples/pdfview/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

# Launch — defaultPath finds testdata/Example.pdf automatically.
Type "./bin/ntcharts-pdfview"
Enter
Sleep 1500ms

# TextMode tour: page through Example.pdf's 3 pages.
Type "n"
Sleep 800ms
Type "n"
Sleep 800ms
Type "p"
Sleep 800ms

# Toggle to ImageMode — pdfium rasterizes the page on demand.
Type "m"
Sleep 2500ms

# Page forward in ImageMode to show the second rasterized page.
Type "n"
Sleep 2000ms
Type "n"
Sleep 2000ms

# Cycle back to TextMode and quit.
Type "m"
Sleep 600ms
Type "q"
