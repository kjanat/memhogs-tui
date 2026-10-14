package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// displayText keeps untrusted process metadata on one line without terminal
// escapes, control characters, or bidirectional text overrides.
func displayText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' ||
			(r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func fitText(s string, width int) string {
	s = ansi.Truncate(displayText(s), max(width, 0), "…")
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}
