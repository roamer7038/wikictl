package page

import (
	"regexp"
	"strings"
)

// Line is one line of the body. N is the 1-based line number within the whole file.
type Line struct {
	N       int
	Text    string
	InFence bool
}

// reHeading matches an ATX heading; the first group is its text. isHeading
// tells whether a line matches it.
var reHeading = regexp.MustCompile(`^#{1,6}\s+(.*?)(?:\s+#+)?\s*$`)

// scanLines splits body into lines and marks those inside code fences.
// firstLine is the line number of the first line of body.
//
// Fences follow CommonMark: a fence is a line of three or more backticks or
// tildes indented by up to three spaces, and the info string after backticks
// must not contain a backtick. The closing fence uses the same character, at
// least as many times as the opening one, followed only by spaces and tabs.
// A fence that is never closed runs to the end of the body.
func scanLines(body []byte, firstLine int) []Line {
	s := strings.TrimSuffix(string(body), "\n")
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	out := make([]Line, 0, len(raw))
	var fence string
	for i, t := range raw {
		t = strings.TrimRight(t, "\r")
		in := fence != ""
		if marker, info, ok := fenceLine(t); ok {
			if fence == "" {
				if marker[0] != '`' || !strings.Contains(info, "`") {
					fence = marker
					in = true
				}
			} else if marker[0] == fence[0] && len(marker) >= len(fence) && strings.Trim(info, " \t") == "" {
				fence = ""
			}
		}
		out = append(out, Line{N: firstLine + i, Text: t, InFence: in})
	}
	return out
}

// fenceLine reports whether t, indented by up to three spaces, starts with
// three or more backticks or tildes. marker is that run of backticks or tildes,
// and info the rest of t.
func fenceLine(t string) (marker, info string, ok bool) {
	i := indent(t)
	if i < 0 || t[i] != '`' && t[i] != '~' {
		return "", "", false
	}
	j := i
	for j < len(t) && t[j] == t[i] {
		j++
	}
	if j-i < 3 {
		return "", "", false
	}
	return t[i:j], t[j:], true
}

// isSpace reports whether c is white space as matched by \s in a regular
// expression.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// isHeading reports whether l is a heading outside code fences: one to six "#"
// at the start of the line, followed by white space.
func isHeading(l Line) bool {
	t := l.Text
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	return !l.InFence && 1 <= n && n <= 6 && n < len(t) && isSpace(t[n])
}

// isLinksHeading reports whether t is "## Links" followed only by white space.
func isLinksHeading(t string) bool {
	rest, ok := strings.CutPrefix(t, "## Links")
	for i := 0; ok && i < len(rest); i++ {
		ok = isSpace(rest[i])
	}
	return ok
}

// headings reads the headings outside code fences. linksStart is the index of
// the "## Links" heading when it is the last heading, or -1 when the page has
// no Links section. notLast holds the indexes of the other "## Links"
// headings, which do not start the Links section because another heading
// follows. title is the index of the first heading when it precedes the Links
// section, or -1.
func headings(lines []Line) (linksStart int, notLast []int, title int) {
	linksStart, title = -1, -1
	last := -1
	for i, l := range lines {
		if !isHeading(l) {
			continue
		}
		if title < 0 {
			title = i
		}
		last = i
		if isLinksHeading(l.Text) {
			notLast = append(notLast, i)
		}
	}
	if n := len(notLast); n > 0 && notLast[n-1] == last {
		linksStart, notLast = last, notLast[:n-1]
	}
	if title == linksStart {
		title = -1
	}
	return linksStart, notLast, title
}
