// Package page interprets the wiki page format: frontmatter, title, body,
// the Links section and relative links. It also performs the line-based
// edits that mv needs. It does not parse Markdown beyond those rules.
package page

import (
	"fmt"
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
	URLs        []Link  // http and https URLs linked from the body
	Issues      []Issue // format violations found while parsing
}

// MaxPageSize is the largest page, in bytes, that Parse interprets.
const MaxPageSize = 1 << 20

// Parse interprets a whole page. Violations are collected in Issues and the
// parts that can still be interpreted are filled in. A page over MaxPageSize
// is not interpreted: only its path issues, a page_too_large issue and the
// title from the file name are set, as TooLarge does.
func Parse(p string, content []byte) *Page {
	if len(content) > MaxPageSize {
		return TooLarge(p)
	}
	pg := named(p)
	fm, rest, n, ok := splitFrontmatter(content)
	if !ok {
		pg.Issues = append(pg.Issues, Issue{Path: p, Line: 1, Code: "missing_summary", Message: "frontmatter is missing"})
		rest = content
	} else {
		m, err := parseFrontmatter(fm)
		if err != nil {
			pg.Issues = append(pg.Issues, Issue{Path: p, Line: 1, Code: "frontmatter_invalid", Message: err.Error()})
		} else {
			pg.Frontmatter = m
			if s, ok := summaryOf(m); ok {
				pg.Summary = s
			} else {
				pg.Issues = append(pg.Issues, Issue{Path: p, Line: 1, Code: "missing_summary", Message: "summary is missing"})
			}
		}
	}
	lines := scanLines(rest, n+1)
	ls, notLast, title := headings(lines)
	for _, i := range notLast {
		pg.Issues = append(pg.Issues, Issue{Path: p, Line: lines[i].N, Code: "links_syntax", Message: `"## Links" is not the last heading, so the lines after it are not read as links`})
	}
	// A closing sequence of "#" is removed only when a space or tab precedes
	// it, so "# C#" has the title "C#".
	if title >= 0 {
		pg.Title = reHeading.FindStringSubmatch(lines[title].Text)[1]
	}
	bodyLines := lines
	if ls >= 0 {
		bodyLines = lines[:ls]
		var iss []Issue
		pg.Links, iss = parseLinks(lines[ls:], p)
		pg.Issues = append(pg.Issues, iss...)
	}
	pg.Mentions, pg.URLs = bodyLinks(bodyLines, p)
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

// TooLarge returns the page that Parse returns for p when its content is over
// MaxPageSize, for a caller that knows the size without reading the content.
func TooLarge(p string) *Page {
	pg := named(p)
	pg.Issues = append(pg.Issues, TooLargeIssue(p))
	return pg
}

// TooLargeIssue returns the page_too_large issue of p.
func TooLargeIssue(p string) Issue {
	return Issue{Path: p, Line: 0, Code: "page_too_large", Message: fmt.Sprintf("page is larger than %d bytes", MaxPageSize)}
}

// named returns the page p with only its path issues and the title from the
// file name.
func named(p string) *Page {
	return &Page{Path: p, Title: strings.TrimSuffix(path.Base(p), ".md"), Issues: PathIssues(p)}
}

// summaryOf returns the summary from the frontmatter. "description" is
// accepted as a synonym; "summary" wins when both are non-blank.
func summaryOf(m map[string]any) (string, bool) {
	for _, k := range []string{"summary", "description"} {
		if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
			return s, true
		}
	}
	return "", false
}
