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
