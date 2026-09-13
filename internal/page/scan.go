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
	reFence   = regexp.MustCompile("^(```+|~~~+)")
	reHeading = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*\s*$`)
	reLinksH  = regexp.MustCompile(`^## Links\s*$`)
)

// ScanLines splits body into lines and marks those inside code fences.
// firstLine is the line number of the first line of body.
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
		if m := reFence.FindString(strings.TrimLeft(t, " ")); m != "" {
			if fence == "" {
				fence = m
				in = true
			} else if m[0] == fence[0] && len(m) >= len(fence) {
				fence = ""
				in = true
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

// Title returns the text of the first heading outside code fences that
// precedes the Links section.
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
