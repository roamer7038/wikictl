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

var (
	reFence   = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")
	reHeading = regexp.MustCompile(`^#{1,6}\s+(.*?)(?:\s+#+)?\s*$`)
	reLinksH  = regexp.MustCompile(`^## Links\s*$`)
)

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
		// Only a line starting with a backtick or a tilde after its indentation
		// can be a fence, so other lines are not matched against reFence.
		if c := strings.TrimLeft(t, " "); c != "" && (c[0] == '`' || c[0] == '~') {
			if m := reFence.FindStringSubmatch(t); m != nil {
				marker, info := m[1], m[2]
				if fence == "" {
					if marker[0] != '`' || !strings.Contains(info, "`") {
						fence = marker
						in = true
					}
				} else if marker[0] == fence[0] && len(marker) >= len(fence) && strings.Trim(info, " \t") == "" {
					fence = ""
				}
			}
		}
		out = append(out, Line{N: firstLine + i, Text: t, InFence: in})
	}
	return out
}

func isHeading(l Line) bool {
	return !l.InFence && strings.HasPrefix(l.Text, "#") && reHeading.MatchString(l.Text)
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
		if reLinksH.MatchString(l.Text) {
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
