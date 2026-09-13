package page

import (
	"bytes"
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// Limits on the input given to the YAML parser, whose memory use grows
// steeply with the nesting depth.
const (
	MaxFrontmatterSize  = 64 << 10 // bytes between the delimiter lines
	MaxFrontmatterDepth = 100      // nested block and flow collections
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

// ParseFrontmatter decodes the YAML frontmatter into a map. Frontmatter over
// MaxFrontmatterSize or MaxFrontmatterDepth is an error.
func ParseFrontmatter(fm []byte) (map[string]any, error) {
	if err := checkFrontmatter(fm); err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(fm, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// checkFrontmatter enforces the frontmatter limits before the YAML parser
// runs. The depth is counted on the lexer tokens, whose cost is linear in the
// input: an open flow collection adds one level, and so does each block
// sequence entry or mapping key indented further than the enclosing one.
func checkFrontmatter(fm []byte) error {
	if len(fm) > MaxFrontmatterSize {
		return fmt.Errorf("frontmatter is larger than %d bytes", MaxFrontmatterSize)
	}
	type level struct {
		col int
		seq bool
	}
	var block []level
	flow := 0
	var prev *token.Token
	for _, tk := range lexer.Tokenize(string(fm)) {
		switch tk.Type {
		case token.SequenceStartType, token.MappingStartType:
			flow++
		case token.SequenceEndType, token.MappingEndType:
			if flow > 0 {
				flow--
			}
		case token.SequenceEntryType, token.MappingValueType:
			if flow > 0 {
				break
			}
			cur := level{tk.Position.Column, tk.Type == token.SequenceEntryType}
			if !cur.seq && prev != nil {
				cur.col = prev.Position.Column
			}
			// A block sequence may sit at the same column as its parent key.
			for len(block) > 0 {
				top := block[len(block)-1]
				if top.col < cur.col || (top.col == cur.col && cur.seq && !top.seq) {
					break
				}
				block = block[:len(block)-1]
			}
			block = append(block, cur)
		}
		if len(block)+flow > MaxFrontmatterDepth {
			return fmt.Errorf("frontmatter nesting is deeper than %d levels", MaxFrontmatterDepth)
		}
		if tk.Type != token.SpaceType && tk.Type != token.CommentType {
			prev = tk
		}
	}
	return nil
}
