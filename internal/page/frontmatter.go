package page

import (
	"bytes"

	"github.com/goccy/go-yaml"
)

// SplitFrontmatter splits content into the frontmatter body (without the
// delimiter lines) and the rest. The frontmatter starts with a first line of
// "---" and ends at the next line that is exactly "---"; a leading BOM and
// trailing CRs are ignored. lines is the number of lines consumed, including
// both delimiters. ok is false when there is no well-formed frontmatter.
func SplitFrontmatter(content []byte) (fm, rest []byte, lines int, ok bool) {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	ls := bytes.SplitAfter(content, []byte("\n"))
	if len(ls) == 0 || !isDashLine(ls[0]) {
		return nil, content, 0, false
	}
	for i := 1; i < len(ls); i++ {
		if isDashLine(ls[i]) {
			return bytes.Join(ls[1:i], nil), bytes.Join(ls[i+1:], nil), i + 1, true
		}
	}
	return nil, content, 0, false
}

func isDashLine(l []byte) bool {
	return string(bytes.TrimRight(l, "\r\n \t")) == "---"
}

// ParseFrontmatter decodes the YAML frontmatter into a map.
func ParseFrontmatter(fm []byte) (map[string]any, error) {
	m := map[string]any{}
	if err := yaml.Unmarshal(fm, &m); err != nil {
		return nil, err
	}
	return m, nil
}
