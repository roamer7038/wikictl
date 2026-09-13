package page

import (
	"bytes"
	"path"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// AddAlias appends alias to the aliases list of the frontmatter, creating the
// list if needed. The aliases node is located with the YAML parser and only
// the text around it is edited, so other keys and comments are kept as they
// are. The content is returned unchanged when the alias is already present,
// when there is no frontmatter, or when the frontmatter is not a mapping whose
// aliases value is a sequence, null, or absent.
func AddAlias(content []byte, alias string) []byte {
	fm, rest, _, ok := SplitFrontmatter(content)
	if !ok {
		return content
	}
	file, err := parser.ParseBytes(fm, 0)
	if err != nil || len(file.Docs) > 1 {
		return content
	}
	var body ast.Node
	if len(file.Docs) == 1 {
		body = file.Docs[0].Body
	}
	lines := strings.Split(strings.TrimSuffix(string(fm), "\n"), "\n")
	if len(fm) == 0 {
		lines = nil
	}
	entry := yamlString(alias)
	var key, value ast.Node
	switch b := body.(type) {
	case nil:
	case *ast.MappingNode:
		if b.IsFlowStyle {
			return content
		}
		for _, mv := range b.Values {
			if mv.Key.GetToken().Value == "aliases" {
				key, value = mv.Key, mv.Value
				break
			}
		}
	default:
		return content
	}
	switch v := value.(type) {
	case nil:
		lines = append(lines, "aliases:", "  - "+entry)
	case *ast.NullNode:
		// The value is read from the text after the key's colon because the
		// parser does not report a reliable column for a null scalar followed
		// by spaces or tabs.
		kl := key.GetToken().Position.Line - 1
		colon := mappingColon(lines[kl], byteIndex(lines[kl], key.GetToken().Position.Column))
		if colon < 0 {
			return content
		}
		l := kl
		start, end := scalarSpan(lines[l], colon+1)
		if start == end {
			indent := len(lines[kl]) - len(strings.TrimLeft(lines[kl], " "))
			for i := kl + 1; i < len(lines); i++ {
				t := strings.TrimRight(lines[i], " \t\r")
				s := strings.TrimLeft(t, " ")
				if s == "" || s[0] == '#' {
					continue
				}
				if len(t)-len(s) > indent {
					l = i
					start, end = scalarSpan(lines[i], 0)
				}
				break
			}
		}
		switch lines[l][start:end] {
		case "":
			lines = insertLine(lines, kl+1, "  - "+entry)
		case "~", "null", "Null", "NULL":
			lines[l] = lines[l][:start] + "[" + yamlFlowString(alias) + "]" + lines[l][end:]
		default:
			return content
		}
	case *ast.SequenceNode:
		for _, item := range v.Values {
			var s string
			if err := yaml.Unmarshal([]byte(item.String()), &s); err == nil && s == alias {
				return content
			}
		}
		if v.IsFlowStyle {
			l := v.End.Position.Line - 1
			i := byteIndex(lines[l], v.End.Position.Column)
			flow := yamlFlowString(alias)
			text := ", " + flow
			if prev := lastNonSpace(lines[:l+1], l, i); prev == '[' {
				text = flow
			} else if prev == ',' {
				text = " " + flow
			}
			lines[l] = lines[l][:i] + text + lines[l][i:]
		} else {
			indent := v.Start.Position.Column - 1
			last := v.Values[len(v.Values)-1].GetToken().Position.Line - 1
			at := last + 1
			for i := last + 1; i < len(lines); i++ {
				t := strings.TrimRight(lines[i], "\r")
				if strings.TrimSpace(t) == "" {
					continue
				}
				if len(t)-len(strings.TrimLeft(t, " ")) <= indent {
					break
				}
				at = i + 1
			}
			lines = insertLine(lines, at, strings.Repeat(" ", indent)+"- "+entry)
		}
	default:
		return content
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	b.Write(rest)
	return b.Bytes()
}

// byteIndex returns the byte index in line of the 1-based character column.
func byteIndex(line string, column int) int {
	n := 1
	for i := range line {
		if n == column {
			return i
		}
		n++
	}
	return len(line)
}

// mappingColon returns the byte index of the colon that ends the mapping key
// starting at byte from of line, or -1 when there is none.
func mappingColon(line string, from int) int {
	for i := from; i < len(line); i++ {
		if line[i] == ':' && (i+1 == len(line) || strings.IndexByte(" \t\r", line[i+1]) >= 0) {
			return i
		}
	}
	return -1
}

// scalarSpan returns the byte range of the text of line after byte from,
// without a trailing comment and surrounding spaces, tabs, and carriage
// returns. The range is empty when there is no such text.
func scalarSpan(line string, from int) (start, end int) {
	end = len(line)
	for i := from; i < len(line); i++ {
		if line[i] == '#' && (i == from || line[i-1] == ' ' || line[i-1] == '\t') {
			end = i
			break
		}
	}
	start = from
	for start < end && strings.IndexByte(" \t\r", line[start]) >= 0 {
		start++
	}
	for end > start && strings.IndexByte(" \t\r", line[end-1]) >= 0 {
		end--
	}
	return start, end
}

// lastNonSpace returns the last non-space character before byte i of line l.
func lastNonSpace(lines []string, l, i int) byte {
	s := strings.TrimRight(lines[l][:i], " \t\r")
	for s == "" && l > 0 {
		l--
		s = strings.TrimRight(lines[l], " \t\r")
	}
	if s == "" {
		return 0
	}
	return s[len(s)-1]
}

func insertLine(lines []string, at int, line string) []string {
	return append(lines[:at], append([]string{line}, lines[at:]...)...)
}

// yamlString returns s as an item of a block sequence that decodes to the
// string s: s itself when it already does, otherwise s in double quotes.
func yamlString(s string) string {
	return yamlScalar(s, "- ", "")
}

// yamlFlowString returns s as an item of a flow sequence that decodes to the
// string s: s itself when it already does, otherwise s in double quotes. A
// plain scalar in a flow collection must not contain flow indicators, so s is
// always quoted when it has one.
func yamlFlowString(s string) string {
	if strings.ContainsAny(s, ",[]{}") {
		return strconv.Quote(s)
	}
	return yamlScalar(s, "[", "]")
}

func yamlScalar(s, prefix, suffix string) string {
	var v []any
	if err := yaml.Unmarshal([]byte(prefix+s+suffix), &v); err == nil && len(v) == 1 && v[0] == s {
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
