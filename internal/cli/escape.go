package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// escapeControl replaces the C0 and C1 control characters in s, except tab,
// with their \xNN notation, so that page content shown in text output cannot
// drive the terminal. A byte in 0x80-0x9f that is not part of valid UTF-8 is
// escaped as well, since 8-bit terminals read it as a C1 control.
func escapeControl(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n == 1 && s[i] >= 0x80 && s[i] <= 0x9f:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r != '\t' && (r < 0x20 || (r >= 0x7f && r <= 0x9f)):
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// escapeField is escapeControl for one field of a line whose fields are
// separated by tabs, where a tab in the value would shift the columns: it is
// printed as \x09 like every other control character. log needs it, since an
// author's name and a commit subject may hold a tab.
func escapeField(s string) string {
	return escapeControl(strings.ReplaceAll(s, "\t", `\x09`))
}

// escapeMessage is escapeControl for a message that may span several lines,
// such as git's stderr in an error: each newline is kept and followed by an
// indent of two spaces, so that no line of the message can pass for a line
// of wikictl's own output. Trailing newlines are removed.
func escapeMessage(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = escapeControl(l)
	}
	return strings.Join(lines, "\n  ")
}
