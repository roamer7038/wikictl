package page

import (
	"errors"
	"path"
	"regexp"
	"strings"
)

// Link is one entry of the Links section, or a page reference found in the
// body (type "mentions").
type Link struct {
	Type   string
	Target string // wiki-root-relative path of the page, or the URL itself
	Note   string
	Line   int
	IsURL  bool
}

// Issue reports a violation of the wiki format.
type Issue struct {
	Path    string `json:"path"`
	Line    int    `json:"line"` // 0 when the issue concerns the whole file
	Code    string `json:"code"`
	Message string `json:"message"`
}

var (
	reScheme  = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)
	reBullet  = regexp.MustCompile(`^\s*[-*+]\s+(.+)$`)
	reTyped   = regexp.MustCompile(`^([a-z][a-z0-9_]*): (.+)$`)
	reMDLink  = regexp.MustCompile(`\]\(([^)]*)\)`)
	reInline  = regexp.MustCompile("`[^`]*`")
	reBracket = regexp.MustCompile(`^\[[^\]]*\]\(([^)]*)\)$`)
	rePrefix  = regexp.MustCompile(`^[a-z][a-z0-9_+.-]*:`)
	reURL     = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://\S`)
)

// ErrBadDest is returned for link destinations that cannot refer to a page:
// absolute paths, paths outside the wiki root, and files other than .md.
var ErrBadDest = errors.New("bad link destination")

// ResolveDest normalizes a link destination written in pagePath to a path
// relative to the wiki root. URLs are returned unchanged with isURL set.
// A "#fragment" and a "title" are dropped.
func ResolveDest(pagePath, dest string) (target string, isURL bool, err error) {
	dest = strings.TrimSpace(dest)
	if reScheme.MatchString(dest) {
		return dest, true, nil
	}
	if i := strings.Index(dest, " \""); i >= 0 {
		dest = dest[:i]
	}
	if i := strings.Index(dest, "#"); i >= 0 {
		dest = dest[:i]
	}
	if dest == "" || strings.HasPrefix(dest, "/") || !strings.HasSuffix(dest, ".md") {
		return "", false, ErrBadDest
	}
	p := path.Join(path.Dir(pagePath), dest)
	if strings.HasPrefix(p, "../") || p == ".." {
		return "", false, ErrBadDest
	}
	return p, false, nil
}

// ParseLinks interprets the Links section. lines starts with the heading line.
// A line is a bullet ("-", "*" or "+", possibly indented) followed by
// "<type>: <target> | <note>"; a bullet holding only "<target> | <note>" is
// an untyped relation of type "see_also". In an untyped line, a target that
// starts with "<word>:" must be a URL of the form "<scheme>://...", so that a
// mistyped "<type>:<target>" is reported instead of being taken as a URL.
func ParseLinks(lines []Line, pagePath string) ([]Link, []Issue) {
	var links []Link
	var issues []Issue
	if len(lines) == 0 {
		return nil, nil
	}
	const syntaxMsg = "line is not of the form `- <type>: <target> | <note>` or `- <target>`"
	for _, l := range lines[1:] {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		m := reBullet.FindStringSubmatch(l.Text)
		if m == nil {
			issues = append(issues, Issue{Path: pagePath, Line: l.N, Code: "links_syntax", Message: syntaxMsg})
			continue
		}
		typ, target, note := "", m[1], ""
		if t := reTyped.FindStringSubmatch(target); t != nil {
			typ, target = t[1], t[2]
		}
		if i := strings.Index(target, " | "); i >= 0 {
			target, note = target[:i], strings.TrimSpace(target[i+3:])
		}
		target = strings.TrimSpace(target)
		bracket := false
		if b := reBracket.FindStringSubmatch(target); b != nil {
			target, bracket = b[1], true
		}
		if typ == "" && ((!bracket && strings.ContainsAny(target, " \t")) ||
			(rePrefix.MatchString(target) && !reURL.MatchString(target))) {
			issues = append(issues, Issue{Path: pagePath, Line: l.N, Code: "links_syntax", Message: syntaxMsg})
			continue
		}
		got, isURL, err := ResolveDest(pagePath, target)
		if err != nil {
			msg := syntaxMsg
			if typ != "" {
				msg = "invalid link destination: " + target
			}
			issues = append(issues, Issue{Path: pagePath, Line: l.N, Code: "links_syntax", Message: msg})
			continue
		}
		if typ == "" {
			typ = "see_also"
		}
		links = append(links, Link{Type: typ, Target: got, Note: note, Line: l.N, IsURL: isURL})
	}
	return links, issues
}

// BodyLinks returns the page references in the body (outside code fences and
// code spans) as links of type "mentions", one per distinct target.
func BodyLinks(lines []Line, pagePath string) []Link {
	var out []Link
	seen := map[string]bool{}
	for _, l := range lines {
		if l.InFence {
			continue
		}
		text := reInline.ReplaceAllString(l.Text, "")
		for _, m := range reMDLink.FindAllStringSubmatch(text, -1) {
			got, isURL, err := ResolveDest(pagePath, m[1])
			if err != nil || isURL || seen[got] {
				continue
			}
			seen[got] = true
			out = append(out, Link{Type: "mentions", Target: got, Line: l.N})
		}
	}
	return out
}
