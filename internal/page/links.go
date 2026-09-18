package page

import (
	"fmt"
	"path"
	"regexp"
	"sort"
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

// resolveDest normalizes a link destination written in pagePath to a path
// relative to the wiki root. dest is the destination without angle brackets
// and title. URLs are returned unchanged with isURL set. A query and a
// fragment are dropped. ok is false for destinations that cannot refer to a
// page: absolute paths, paths outside the wiki root, and files other than .md.
// Every .md destination resolves, whether IsPagePath holds for it or not, so
// that a link to a .md file below a directory starting with a dot is a link
// that mv keeps correct.
func resolveDest(pagePath, dest string) (target string, isURL, ok bool) {
	if reScheme.MatchString(dest) {
		return dest, true, true
	}
	dest = destPath(dest)
	if dest == "" || strings.HasPrefix(dest, "/") || !strings.HasSuffix(dest, ".md") {
		return "", false, false
	}
	p := path.Join(path.Dir(pagePath), dest)
	if strings.HasPrefix(p, "../") || p == ".." {
		return "", false, false
	}
	return p, false, true
}

// parseLinks interprets the Links section. lines starts with the heading line.
// A line is a bullet ("-", "*" or "+", possibly indented) followed by
// "<type>: <target> | <note>"; a bullet holding only "<target> | <note>" is
// an untyped relation of type "see_also". In an untyped line, a target that
// starts with "<word>:" must be a URL of the form "<scheme>://...", so that a
// mistyped "<type>:<target>" is reported instead of being taken as a URL.
// A target that starts with a scheme is taken as a URL as it is written.
func parseLinks(lines []Line, pagePath string) ([]Link, []Issue) {
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
		} else if reScheme.MatchString(target) {
			dest = target
		} else if start, end, next, ok := parseDest(target, 0); ok && next == len(target) {
			dest = target[start:end]
		}
		if typ == "" && ((!bracket && strings.ContainsAny(target, " \t")) ||
			(rePrefix.MatchString(target) && !reURL.MatchString(target))) {
			issues = append(issues, Issue{Path: pagePath, Line: l.N, Code: "links_syntax", Message: syntaxMsg})
			continue
		}
		got, isURL, ok := resolveDest(pagePath, dest)
		if !ok {
			msg := syntaxMsg
			if typ != "" {
				// The target is quoted, so that a byte of a target that is
				// not valid UTF-8 is written as \xNN rather than lost to
				// U+FFFD when the message is printed as JSON.
				msg = fmt.Sprintf("invalid link destination: %q", target)
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

// bodyLinks returns the links in the body (outside code fences and code
// spans), all of type "mentions": mentions holds the page references, one per
// distinct target, and urls the destinations that are URLs of the http or
// https scheme, one per distinct URL and with IsURL set. An image is a
// reference to a page but not a link to a URL, and a URL of another scheme,
// an autolink and a bare URL are no link to a URL either. The line of a link
// is the line where its destination starts.
func bodyLinks(lines []Line, pagePath string) (mentions, urls []Link) {
	seen := map[string]bool{}
	eachParagraph(lines, -1, func(from, _ int, text string, offsets []int) {
		for _, m := range findLinks(text) {
			dest := text[m.destStart:m.destEnd]
			got, isURL, ok := resolveDest(pagePath, dest)
			if !ok || seen[got] || isURL && (m.image || !webURL(dest)) {
				continue
			}
			seen[got] = true
			n := lines[from+sort.SearchInts(offsets, m.destStart+1)-1].N
			l := Link{Type: "mentions", Target: got, Line: n, IsURL: isURL}
			if isURL {
				urls = append(urls, l)
			} else {
				mentions = append(mentions, l)
			}
		}
	})
	return mentions, urls
}

// webURL reports whether dest is a URL of the http or https scheme, the only
// URLs that bodyLinks lists: a link to another scheme, such as mailto: or
// data:, is not a reference a reader can follow to a page of the web.
func webURL(dest string) bool {
	return strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://")
}

// indent returns the index of the first byte of t after up to three spaces, or
// -1 when t has no such byte.
func indent(t string) int {
	for i := 0; i < len(t) && i <= 3; i++ {
		if t[i] != ' ' {
			return i
		}
	}
	return -1
}

// blockStart reports whether c is the first byte of t after up to three
// spaces.
func blockStart(t string, c byte) bool {
	i := indent(t)
	return i >= 0 && t[i] == c
}

// listItem reports whether t starts a list item: "-", "*", "+", or up to nine
// digits followed by "." or ")", indented by up to three spaces and followed by
// a space or a tab. num is the number of an ordered item, and "" otherwise.
func listItem(t string) (num string, ok bool) {
	i := indent(t)
	if i < 0 {
		return "", false
	}
	j := i
	if c := t[i]; c == '-' || c == '*' || c == '+' {
		j++
	} else {
		for j < len(t) && j-i < 9 && '0' <= t[j] && t[j] <= '9' {
			j++
		}
		if j == i || j == len(t) || t[j] != '.' && t[j] != ')' {
			return "", false
		}
		num = t[i:j]
		j++
	}
	if j == len(t) || t[j] != ' ' && t[j] != '\t' {
		return "", false
	}
	return num, true
}

// underlineOrBreak reports whether t, indented by up to three spaces, is a
// setext underline ("=" or "-" repeated, then spaces and tabs) or a thematic
// break (three or more "*", "-" or "_", with spaces and tabs between them).
func underlineOrBreak(t string) bool {
	i := indent(t)
	if i < 0 {
		return false
	}
	c := t[i]
	if c != '=' && c != '-' && c != '*' && c != '_' {
		return false
	}
	// n counts c. gap is set after a space or a tab, and inner when c follows
	// such a gap, that is, when the run of c is interrupted by white space.
	n, gap, inner := 0, false, false
	for k := i; k < len(t); k++ {
		switch t[k] {
		case c:
			inner = inner || gap
			n++
		case ' ', '\t':
			gap = true
		default:
			return false
		}
	}
	switch c {
	case '=':
		return !inner
	case '-':
		return !inner || n >= 3
	}
	return n >= 3
}

// eachParagraph calls fn for each paragraph of lines: a run of lines outside
// code fences without blank lines and headings. A heading, a setext underline,
// a thematic break, and each line from linksStart on (the Links section is
// read line by line) is a paragraph of its own; linksStart is -1 when there is
// no Links section. A new paragraph also starts at a list item (an ordered
// one only after another list item or when it is numbered 1), at a table row
// after another table row, and at a block quote line after a line outside a
// block quote. fn receives the range of the lines, their text joined with
// "\n", and the byte offset of each line in that text; fn must not change the
// offsets.
func eachParagraph(lines []Line, linksStart int, fn func(from, to int, text string, offsets []int)) {
	oneLine := []int{0} // the offsets of every paragraph of one line
	blank := func(l Line) bool { return strings.Trim(l.Text, " \t") == "" }
	joinable := func(i int) bool {
		l := lines[i]
		return (linksStart < 0 || i < linksStart) && !l.InFence && !blank(l) && !isHeading(l) && !underlineOrBreak(l.Text)
	}
	starts := func(i int) bool {
		t, prev := lines[i].Text, lines[i-1].Text
		if num, ok := listItem(t); ok {
			if _, after := listItem(prev); num == "" || num == "1" || after {
				return true
			}
		}
		return blockStart(t, '|') && blockStart(prev, '|') || blockStart(t, '>') && !blockStart(prev, '>')
	}
	for i := 0; i < len(lines); {
		if lines[i].InFence || blank(lines[i]) {
			i++
			continue
		}
		j := i + 1
		if joinable(i) {
			for j < len(lines) && joinable(j) && !starts(j) {
				j++
			}
		}
		if j == i+1 {
			fn(i, j, lines[i].Text, oneLine)
			i = j
			continue
		}
		var b strings.Builder
		offsets := make([]int, 0, j-i)
		for k := i; k < j; k++ {
			if k > i {
				b.WriteByte('\n')
			}
			offsets = append(offsets, b.Len())
			b.WriteString(lines[k].Text)
		}
		fn(i, j, b.String(), offsets)
		i = j
	}
}

// inlineLink is the position of an inline link or image in a paragraph.
type inlineLink struct {
	start, end         int  // from "[" or "![" to the closing ")"
	destStart, destEnd int  // the destination without angle brackets and title
	image              bool // the link is an image, written "![text](dest)"
}

// findLinks returns the inline links and images of a paragraph, whose lines
// are joined with "\n", following the CommonMark rules for code spans,
// backslash escapes, link text and inline links: a code span takes precedence
// over a link that starts before it and ends inside it, and a link cannot
// contain another link. Reference links, autolinks, raw HTML and entity
// references are not recognized, backslash escapes are kept in the
// destination, and a destination without angle brackets ends only at a space,
// a tab or a line break, so it may contain other control characters. The time
// is linear in the length of s.
func findLinks(s string) []inlineLink {
	type opener struct {
		pos   int
		image bool
	}
	var out []inlineLink
	var openers []opener
	// Openers of links (not images) below index inactive may not start a link,
	// because a link was found after them.
	inactive := 0
	// runs holds the start of every backtick run of s by its length, and
	// resume[n] the index in runs[n] where the search for a closing run resumes.
	var runs map[int][]int
	var resume map[int]int
	for i := 0; i < len(s); {
		switch {
		case escaped(s, i):
			i += 2
		case s[i] == '`':
			if runs == nil {
				runs, resume = map[int][]int{}, map[int]int{}
				for j := i; j < len(s); {
					n := backtickRun(s, j)
					if n == 0 {
						j++
						continue
					}
					runs[n] = append(runs[n], j)
					j += n
				}
			}
			n := backtickRun(s, i)
			i += n
			ps, c := runs[n], resume[n]
			for c < len(ps) && ps[c] < i {
				c++
			}
			resume[n] = c
			if c < len(ps) {
				i = ps[c] + n
			}
		case s[i] == '[' || s[i] == '!' && i+1 < len(s) && s[i+1] == '[':
			image := s[i] == '!'
			openers = append(openers, opener{pos: i, image: image})
			i++
			if image {
				i++
			}
		case s[i] == ']' && len(openers) > 0:
			k := len(openers) - 1
			o := openers[k]
			openers = openers[:k]
			active := o.image || k >= inactive
			inactive = min(inactive, k)
			if active && i+1 < len(s) && s[i+1] == '(' {
				if start, end, next, ok := parseDest(s, i+2); ok && next < len(s) && s[next] == ')' {
					out = append(out, inlineLink{start: o.pos, end: next + 1, destStart: start, destEnd: end, image: o.image})
					if !o.image {
						inactive = k
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

// maxParenDepth is the deepest nesting of parentheses accepted in a
// destination without angle brackets, as in cmark.
const maxParenDepth = 32

// parseDest parses the destination and the optional title of an inline link,
// starting at byte i of s just after "(". It returns the byte range of the
// destination, without angle brackets, and the index of the first byte after
// the title and the white space that follows it.
func parseDest(s string, i int) (start, end, next int, ok bool) {
	i = skipSpace(s, i)
	if i < len(s) && s[i] == '<' {
		j := i + 1
		for ; j < len(s) && s[j] != '>'; j++ {
			if s[j] == '<' || s[j] == '\n' {
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
		for ; j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '\n'; j++ {
			if escaped(s, j) {
				j++
			} else if s[j] == '(' {
				if depth++; depth > maxParenDepth {
					return 0, 0, 0, false
				}
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

// skipSpace returns the index of the first byte at or after byte i of s that
// is not a space or a tab, allowing one line break among them.
func skipSpace(s string, i int) int {
	nl := false
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' && !nl) {
		nl = nl || s[i] == '\n'
		i++
	}
	return i
}
