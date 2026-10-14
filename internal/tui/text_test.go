package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestProcessTextIsSafeAndFitsDisplayWidth(t *testing.T) {
	input := "node\x1b[2J\x1b]0;malicious title\a\n\r\t\x9b\u202e秘密👩‍💻é"
	clean := displayText(input)
	if strings.ContainsAny(clean, "\x1b\a\n\r\t\u202e") || strings.Contains(clean, "malicious title") {
		t.Fatalf("unsafe process text: %q", clean)
	}
	for _, text := range []string{input, "日本語のプロセス", "👩‍💻 workers", "école", "plain", string([]byte{0xff, 0xfe})} {
		for width := range 25 {
			fitted := fitText(text, width)
			if !utf8.ValidString(fitted) || ansi.StringWidth(fitted) != width {
				t.Fatalf("%q width %d => %q width %d", text, width, fitted, ansi.StringWidth(fitted))
			}
		}
	}
}
