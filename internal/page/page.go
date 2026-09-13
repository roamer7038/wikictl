// Package page interprets the wiki page format: frontmatter, title, body,
// the Links section and relative links. It also performs the line-based
// edits that mv needs. It does not parse Markdown beyond those rules.
package page

import (
	"path"
	"strings"
)

// Page is a parsed page.
type Page struct {
	Path        string
	Summary     string
	Title       string
	Body        string // body without the frontmatter and the Links section
	Frontmatter map[string]any
	Links       []Link  // typed links from the Links section
	Mentions    []Link  // page references from the body
	Issues      []Issue // format violations found while parsing
}

// Parse interprets a whole page. Violations are collected in Issues and the
// parts that can still be interpreted are filled in.
func Parse(p string, content []byte) *Page {
	pg := &Page{Path: p}
	pg.Issues = append(pg.Issues, PathIssues(p)...)
	fm, rest, n, ok := SplitFrontmatter(content)
	if !ok {
		pg.Issues = append(pg.Issues, Issue{Path: p, Line: 1, Code: "missing_summary", Message: "frontmatter is missing"})
		rest = content
	} else {
		m, err := ParseFrontmatter(fm)
		if err != nil {
			pg.Issues = append(pg.Issues, Issue{Path: p, Line: 1, Code: "frontmatter_invalid", Message: err.Error()})
		} else {
			pg.Frontmatter = m
			if s, _ := m["summary"].(string); strings.TrimSpace(s) != "" {
				pg.Summary = s
			} else {
				pg.Issues = append(pg.Issues, Issue{Path: p, Line: 1, Code: "missing_summary", Message: "summary is missing"})
			}
		}
	}
	lines := ScanLines(rest, n+1)
	ls := LinksStart(lines)
	if t, ok := Title(lines, ls); ok {
		pg.Title = t
	} else {
		pg.Title = strings.TrimSuffix(path.Base(p), ".md")
	}
	bodyLines := lines
	if ls >= 0 {
		bodyLines = lines[:ls]
		var iss []Issue
		pg.Links, iss = ParseLinks(lines[ls:], p)
		pg.Issues = append(pg.Issues, iss...)
	}
	pg.Mentions = BodyLinks(bodyLines, p)
	var sb strings.Builder
	for _, l := range bodyLines {
		sb.WriteString(l.Text)
		sb.WriteByte('\n')
	}
	if body := strings.TrimRight(sb.String(), "\n"); body != "" {
		pg.Body = body + "\n"
	}
	return pg
}
