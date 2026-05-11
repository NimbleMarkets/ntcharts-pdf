// pdfview/sanitize.go: terminal-safe string sanitization.
//
// PDF-derived text and host-supplied paths reach the user via terminal
// writes. Without filtering, a hostile PDF or filename can embed:
//
//   - C0 / C1 control sequences (cursor moves, color changes, OSC titles)
//   - DEL (0x7F)
//   - Bidi format chars (LRE/RLO/PDI/...) — the "Trojan Source" attack
//     class, which can swap apparent text order to spoof content
//   - Zero-width / invisible chars that nudge readers' visual parsing
//   - BOM, byte order marks, other non-printable Unicode
//
// sanitizeForTerminal removes all of the above and replaces line
// breaks / tabs with single spaces so a multi-line malicious string
// can't fan out across the status bar.

package pdfview

import (
	"strings"
	"unicode"
)

// sanitizeForTerminal returns s with control, format, and non-printable
// runes removed. Newlines and tabs become single spaces; trailing /
// leading whitespace is preserved (callers can TrimSpace if they want).
//
// The function is conservative: anything not in Unicode's printable
// category set (L, M, N, P, S, Zs) is dropped, plus an explicit deny
// list of bidi format chars that overlap with category Cf but are
// already excluded by !IsPrint — the redundancy documents intent.
func sanitizeForTerminal(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t', '\v', '\f':
			// Replace any whitespace-control with a single space so the
			// caller's status bar / cell still gets a separator.
			b.WriteByte(' ')
			continue
		}
		if !unicode.IsPrint(r) {
			// Catches Cc (control), Cf (format incl. bidi), Co (private
			// use), Cs (surrogates), Cn (non-character), and some
			// Z-category non-space separators.
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
