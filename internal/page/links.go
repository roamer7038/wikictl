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
	reScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)
	reBullet = regexp.MustCompile(`^\s*[-*+]\s+(.+)$`)
	reTyped  = regexp.MustCompile(`^([a-z][a-z0-9_]*): (.+)$`)
	rePrefix = regexp.MustCompile(`^[a-z][a-z0-9_+.-]*:`)
	reURL    = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://\S`)
)

// ErrBadDest is returned for link destinations that cannot refer to a page:
// absolute paths, paths outside the wiki root, and files other than .md.
var ErrBadDest = errors.New("bad link destination")

// ResolveDest normalizes a link destination written in pagePath to a path
// relative to the wiki root. dest is the destination without angle brackets
// and title. URLs are returned unchanged with isURL set. A query and a
// fragment are dropped.
func ResolveDest(pagePath, dest string) (target string, isURL bool, err error) {
	if reScheme.MatchString(dest) {
		return dest, true, nil
	}
	dest = destPath(dest)
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
		dest, bracket := "", false
		if ls := findLinks(target); len(ls) == 1 && ls[0].start == 0 && ls[0].end == len(target) && target[0] == '[' {
			dest, bracket = target[ls[0].destStart:ls[0].destEnd], true
		} else if start, end, next, ok := parseDest(target, 0); ok && next == len(target) {
			dest = target[start:end]
		}
		if typ == "" && ((!bracket && strings.ContainsAny(target, " \t")) ||
			(rePrefix.MatchString(target) && !reURL.MatchString(target))) {
			issues = append(issues, Issue{Path: pagePath, Line: l.N, Code: "links_syntax", Message: syntaxMsg})
			continue
		}
		got, isURL, err := ResolveDest(pagePath, dest)
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
		for _, m := range findLinks(l.Text) {
			got, isURL, err := ResolveDest(pagePath, l.Text[m.destStart:m.destEnd])
			if err != nil || isURL || seen[got] {
				continue
			}
			seen[got] = true
			out = append(out, Link{Type: "mentions", Target: got, Line: l.N})
		}
	}
	return out
}

// inlineLink is the position of an inline link or image in a line.
type inlineLink struct {
	start, end         int // from "[" or "![" to the closing ")"
	destStart, destEnd int // the destination without angle brackets and title
}

// findLinks returns the inline links and images of a line, following the
// CommonMark rules for code spans, backslash escapes, link text and inline
// links: a code span takes precedence over a link that starts before it and
// ends inside it, and a link cannot contain another link. Reference links,
// autolinks, raw HTML, and links spanning several lines are not recognized,
// and a destination without angle brackets ends only at a space or a tab, so
// it may contain control characters.
func findLinks(s string) []inlineLink {
	type opener struct {
		pos           int
		image, active bool
	}
	var out []inlineLink
	var openers []opener
	for i := 0; i < len(s); {
		switch {
		case escaped(s, i):
			i += 2
		case s[i] == '`':
			n := backtickRun(s, i)
			i += n
			for j := i; j < len(s); {
				if m := backtickRun(s, j); m == 0 {
					j++
				} else if m == n {
					i = j + m
					break
				} else {
					j += m
				}
			}
		case s[i] == '[' || s[i] == '!' && i+1 < len(s) && s[i+1] == '[':
			image := s[i] == '!'
			openers = append(openers, opener{pos: i, image: image, active: true})
			i++
			if image {
				i++
			}
		case s[i] == ']' && len(openers) > 0:
			o := openers[len(openers)-1]
			openers = openers[:len(openers)-1]
			if o.active && i+1 < len(s) && s[i+1] == '(' {
				if start, end, next, ok := parseDest(s, i+2); ok && next < len(s) && s[next] == ')' {
					out = append(out, inlineLink{start: o.pos, end: next + 1, destStart: start, destEnd: end})
					if !o.image {
						for k := range openers {
							if !openers[k].image {
								openers[k].active = false
							}
						}
					}
					i = next + 1
					continue
				}
			}
			i++
		default:
			i++
		}
	}
	return out
}

// parseDest parses the destination and the optional title of an inline link,
// starting at byte i of s just after "(". It returns the byte range of the
// destination, without angle brackets, and the index of the first byte after
// the title and the spaces and tabs that follow it.
func parseDest(s string, i int) (start, end, next int, ok bool) {
	i = skipSpace(s, i)
	if i < len(s) && s[i] == '<' {
		j := i + 1
		for ; j < len(s) && s[j] != '>'; j++ {
			if s[j] == '<' {
				return 0, 0, 0, false
			}
			if escaped(s, j) {
				j++
			}
		}
		if j == len(s) {
			return 0, 0, 0, false
		}
		start, end, i = i+1, j, j+1
	} else {
		depth, j := 0, i
		for ; j < len(s) && s[j] != ' ' && s[j] != '\t'; j++ {
			if escaped(s, j) {
				j++
			} else if s[j] == '(' {
				depth++
			} else if s[j] == ')' {
				if depth == 0 {
					break
				}
				depth--
			}
		}
		if depth != 0 {
			return 0, 0, 0, false
		}
		start, end, i = i, j, j
	}
	t := skipSpace(s, i)
	if t > i && t < len(s) && strings.IndexByte("\"'(", s[t]) >= 0 {
		closer := s[t]
		if closer == '(' {
			closer = ')'
		}
		k := t + 1
		for ; k < len(s) && s[k] != closer; k++ {
			if s[t] == '(' && s[k] == '(' {
				return 0, 0, 0, false
			}
			if escaped(s, k) {
				k++
			}
		}
		if k == len(s) {
			return 0, 0, 0, false
		}
		t = skipSpace(s, k+1)
	}
	return start, end, t, true
}

// destPath returns the path of a link destination: the text before a query or
// a fragment.
func destPath(dest string) string {
	if i := strings.IndexAny(dest, "?#"); i >= 0 {
		return dest[:i]
	}
	return dest
}

// escaped reports whether byte i of s is a backslash that escapes the ASCII
// punctuation character after it.
func escaped(s string, i int) bool {
	return s[i] == '\\' && i+1 < len(s) && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", s[i+1]) >= 0
}

// backtickRun returns the length of the run of backticks starting at byte i of
// s.
func backtickRun(s string, i int) int {
	j := i
	for j < len(s) && s[j] == '`' {
		j++
	}
	return j - i
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}
