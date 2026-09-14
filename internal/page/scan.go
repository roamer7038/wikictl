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

// ScanLines splits body into lines and marks those inside code fences.
// firstLine is the line number of the first line of body.
//
// Fences follow CommonMark: a fence is a line of three or more backticks or
// tildes indented by up to three spaces, and the info string after backticks
// must not contain a backtick. The closing fence uses the same character, at
// least as many times as the opening one, followed only by spaces and tabs.
// A fence that is never closed runs to the end of the body.
func ScanLines(body []byte, firstLine int) []Line {
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
		out = append(out, Line{N: firstLine + i, Text: t, InFence: in})
	}
	return out
}

func isHeading(l Line) bool { return !l.InFence && reHeading.MatchString(l.Text) }

// LinksStart returns the index of the "## Links" heading when it is the last
// heading outside code fences, or -1 when the page has no Links section.
func LinksStart(lines []Line) int {
	last := -1
	for i, l := range lines {
		if isHeading(l) {
			last = i
		}
	}
	if last >= 0 && reLinksH.MatchString(lines[last].Text) {
		return last
	}
	return -1
}

// LinksNotLast returns the indexes of the "## Links" headings outside code
// fences that do not start the Links section because another heading follows.
func LinksNotLast(lines []Line, linksStart int) []int {
	var out []int
	for i, l := range lines {
		if i != linksStart && isHeading(l) && reLinksH.MatchString(l.Text) {
			out = append(out, i)
		}
	}
	return out
}

// Title returns the text of the first heading outside code fences that
// precedes the Links section. A closing sequence of "#" is removed only when
// a space or tab precedes it, so "# C#" has the title "C#".
func Title(lines []Line, linksStart int) (string, bool) {
	end := len(lines)
	if linksStart >= 0 {
		end = linksStart
	}
	for _, l := range lines[:end] {
		if isHeading(l) {
			return reHeading.FindStringSubmatch(l.Text)[1], true
		}
	}
	return "", false
}
