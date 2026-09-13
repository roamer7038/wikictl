package page

import (
	"bytes"
	"path"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// AddAlias appends alias to the aliases list of the frontmatter, creating the
// list if needed. The content is returned unchanged when the alias is already
// present or when there is no frontmatter. Other lines are not touched.
func AddAlias(content []byte, alias string) []byte {
	fm, rest, _, ok := SplitFrontmatter(content)
	if !ok {
		return content
	}
	lines := strings.Split(strings.TrimSuffix(string(fm), "\n"), "\n")
	idx := -1
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		if l == "aliases:" {
			idx = i
		}
		if t := strings.TrimSpace(l); t == "- "+alias || t == "- "+yamlString(alias) {
			return content
		}
	}
	entry := "  - " + yamlString(alias)
	if idx < 0 {
		lines = append(lines, "aliases:", entry)
	} else {
		end := idx + 1
		for end < len(lines) && strings.HasPrefix(lines[end], "  - ") {
			end++
		}
		lines = append(lines[:end], append([]string{entry}, lines[end:]...)...)
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n---\n")
	b.Write(rest)
	return b.Bytes()
}

// yamlString returns s as a YAML scalar that decodes to the string s: s itself
// when it already does, otherwise s in double quotes.
func yamlString(s string) string {
	var v []any
	if err := yaml.Unmarshal([]byte("- "+s), &v); err == nil && len(v) == 1 && v[0] == s {
		return s
	}
	return strconv.Quote(s)
}

// RelDest returns the relative link destination from fromPage to toPath.
func RelDest(fromPage, toPath string) string {
	from := strings.Split(path.Dir(fromPage), "/")
	to := strings.Split(toPath, "/")
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	return strings.Repeat("../", len(from)-i) + strings.Join(to[i:], "/")
}

// Relocate rewrites the relative links of a page written at fromPage so that
// they are correct when the page lives at toPage. When mapper maps a link
// target to a new path, the link points to that path instead. Links inside
// code fences are left alone. The second result is the number of links changed.
func Relocate(content []byte, fromPage, toPage string, mapper func(target string) (string, bool)) ([]byte, int) {
	return rewrite(content, fromPage, func(dest, target string) (string, bool) {
		if mapper != nil {
			if nt, ok := mapper(target); ok {
				target = nt
			}
		}
		nd := RelDest(toPage, target)
		if nd == stripSuffix(dest) {
			return "", false
		}
		return nd, true
	})
}

func stripSuffix(dest string) string {
	if i := strings.Index(dest, "#"); i >= 0 {
		return dest[:i]
	}
	return dest
}

// rewrite applies fn to every page link outside code fences. fn receives the
// destination as written and the resolved target, and returns the new
// destination; a "#fragment" of the original destination is preserved.
func rewrite(content []byte, pagePath string, fn func(dest, target string) (string, bool)) ([]byte, int) {
	fm, rest, n, ok := SplitFrontmatter(content)
	lines := ScanLines(rest, n+1)
	changed := 0
	var b bytes.Buffer
	if ok {
		b.WriteString("---\n")
		b.Write(fm)
		b.WriteString("---\n")
	}
	for _, l := range lines {
		text := l.Text
		if !l.InFence {
			text = reMDLink.ReplaceAllStringFunc(text, func(m string) string {
				dest := m[2 : len(m)-1]
				got, isURL, err := ResolveDest(pagePath, dest)
				if err != nil || isURL {
					return m
				}
				nd, ok := fn(dest, got)
				if !ok {
					return m
				}
				changed++
				suffix := ""
				if i := strings.Index(dest, "#"); i >= 0 {
					suffix = dest[i:]
				}
				return "](" + nd + suffix + ")"
			})
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.Bytes(), changed
}
