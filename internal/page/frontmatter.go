package page

import (
	"bytes"
	"fmt"
	"math"
	"slices"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// Limits on the input given to the YAML parser, whose memory use grows
// steeply with the nesting depth.
const (
	maxFrontmatterSize  = 64 << 10 // bytes between the delimiter lines
	maxFrontmatterDepth = 100      // nested block and flow collections
)

// splitFrontmatter splits content into the frontmatter body (without the
// delimiter lines) and the rest. The frontmatter starts with a first line of
// "---" and ends at the next line that is exactly "---"; a leading BOM and
// trailing CRs are ignored. lines is the number of lines consumed, including
// both delimiters. ok is false when there is no well-formed frontmatter.
func splitFrontmatter(content []byte) (fm, rest []byte, lines int, ok bool) {
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

// parseFrontmatter decodes the YAML frontmatter into a map. Frontmatter over
// maxFrontmatterSize or maxFrontmatterDepth is an error.
func parseFrontmatter(fm []byte) (map[string]any, error) {
	if err := checkFrontmatter(fm); err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(fm, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// FrontmatterKey is one top-level key of a frontmatter, with the line it is
// written on, counted from the start of the file, and its value.
type FrontmatterKey struct {
	Key   string `json:"key"`
	Line  int    `json:"line"`
	Value any    `json:"value"`
}

// frontmatterFirstLine is the line of the frontmatter body within the file:
// the opening delimiter takes the first line.
const frontmatterFirstLine = 2

// Frontmatter returns the top-level keys of the frontmatter of content, in the
// order they are written, each with the line it is written on and its value.
// ok is false when content has no frontmatter; a frontmatter that has no key
// gives an empty list. The values come from decoding the whole frontmatter, as
// parseFrontmatter does, so that anchors and merge keys are read the way the
// rest of wikictl reads them; the syntax tree only gives the order and the
// lines. A value that JSON cannot hold is converted by jsonValue.
func Frontmatter(content []byte) ([]FrontmatterKey, bool, error) {
	fm, _, _, ok := splitFrontmatter(content)
	if !ok {
		return nil, false, nil
	}
	m, err := parseFrontmatter(fm)
	if err != nil {
		return nil, false, err
	}
	f, err := parser.ParseBytes(fm, 0)
	if err != nil {
		return nil, false, err
	}
	return orderedKeys(f, m), true, nil
}

// orderedKeys lists the keys of m in the order the pairs of f write them. A
// key brought in by a merge key takes the line of the "<<", which is not
// listed itself, while a key written at the top level keeps its own line and
// wins over the merge, as YAML resolves it.
func orderedKeys(f *ast.File, m map[string]any) []FrontmatterKey {
	anchors := map[string]ast.Node{}
	for _, doc := range f.Docs {
		ast.Walk(anchorCollector(anchors), doc.Body)
	}
	pairs := topLevelPairs(f)
	explicit := map[string]bool{}
	for _, kv := range pairs {
		if !kv.Key.IsMergeKey() {
			explicit[kv.Key.GetToken().Value] = true
		}
	}
	keys, listed := []FrontmatterKey{}, map[string]bool{}
	add := func(name string, line int) {
		v, held := m[name]
		if !held || listed[name] {
			return
		}
		listed[name] = true
		keys = append(keys, FrontmatterKey{name, line, jsonValue(v)})
	}
	for _, kv := range pairs {
		line := kv.Key.GetToken().Position.Line + frontmatterFirstLine - 1
		if !kv.Key.IsMergeKey() {
			add(kv.Key.GetToken().Value, line)
			continue
		}
		for _, name := range mergedKeys(kv.Value, anchors, map[string]bool{}) {
			if !explicit[name] {
				add(name, line)
			}
		}
	}
	// A key that none of the pairs accounted for, such as one from an alias
	// that could not be followed, is still listed, at the opening delimiter.
	var rest []string
	for name := range m {
		if !listed[name] {
			rest = append(rest, name)
		}
	}
	slices.Sort(rest)
	for _, name := range rest {
		add(name, 1)
	}
	return keys
}

// topLevelPairs returns the pairs of the mapping that the frontmatter is, or
// nothing when it holds no pair.
func topLevelPairs(f *ast.File) []*ast.MappingValueNode {
	for _, doc := range f.Docs {
		switch b := doc.Body.(type) {
		case *ast.MappingNode:
			return b.Values
		case *ast.MappingValueNode:
			return []*ast.MappingValueNode{b}
		}
	}
	return nil
}

// anchorCollector records the node of every anchor of a document, so that the
// aliases of a merge key can be followed.
type anchorCollector map[string]ast.Node

func (c anchorCollector) Visit(n ast.Node) ast.Visitor {
	if a, ok := n.(*ast.AnchorNode); ok && a.Name != nil {
		c[a.Name.GetToken().Value] = a.Value
	}
	return c
}

// mergedKeys returns, in order, the keys that the value of a merge key brings
// in: the keys of the mapping it names, of each mapping of a sequence of them,
// or of a mapping written in place, following the merge keys they hold in
// turn. seen keeps an alias that refers to itself from looping.
func mergedKeys(n ast.Node, anchors map[string]ast.Node, seen map[string]bool) []string {
	switch v := n.(type) {
	case *ast.AliasNode:
		if v.Value == nil {
			return nil
		}
		name := v.Value.GetToken().Value
		if seen[name] {
			return nil
		}
		seen[name] = true
		return mergedKeys(anchors[name], anchors, seen)
	case *ast.AnchorNode:
		return mergedKeys(v.Value, anchors, seen)
	case *ast.SequenceNode:
		var out []string
		for _, e := range v.Values {
			out = append(out, mergedKeys(e, anchors, seen)...)
		}
		return out
	case *ast.MappingNode:
		var out []string
		for _, kv := range v.Values {
			out = append(out, pairKeys(kv, anchors, seen)...)
		}
		return out
	case *ast.MappingValueNode:
		return pairKeys(v, anchors, seen)
	}
	return nil
}

func pairKeys(kv *ast.MappingValueNode, anchors map[string]ast.Node, seen map[string]bool) []string {
	if kv.Key.IsMergeKey() {
		return mergedKeys(kv.Value, anchors, seen)
	}
	return []string{kv.Key.GetToken().Value}
}

// jsonValue returns v with the values that JSON cannot hold replaced: a number
// that is not finite becomes the string YAML writes it as, and the keys of a
// mapping that are not strings become their text. Everything else is kept as
// parseFrontmatter decoded it, so that the values agree with what -meta
// compares against.
func jsonValue(v any) any {
	switch t := v.(type) {
	case float64:
		if s, ok := nonFinite(t); ok {
			return s
		}
	case float32:
		if s, ok := nonFinite(float64(t)); ok {
			return s
		}
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = jsonValue(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = jsonValue(e)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[fmt.Sprint(k)] = jsonValue(e)
		}
		return out
	}
	return v
}

// nonFinite returns how YAML writes a number that JSON cannot hold.
func nonFinite(f float64) (string, bool) {
	switch {
	case math.IsInf(f, 1):
		return ".inf", true
	case math.IsInf(f, -1):
		return "-.inf", true
	case math.IsNaN(f):
		return ".nan", true
	}
	return "", false
}

// checkFrontmatter enforces the frontmatter limits before the YAML parser
// runs. The depth is counted on the lexer tokens, whose cost is linear in the
// input: an open flow collection adds one level, and so does each block
// sequence entry or mapping key indented further than the enclosing one.
func checkFrontmatter(fm []byte) error {
	if len(fm) > maxFrontmatterSize {
		return fmt.Errorf("frontmatter is larger than %d bytes", maxFrontmatterSize)
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
		if len(block)+flow > maxFrontmatterDepth {
			return fmt.Errorf("frontmatter nesting is deeper than %d levels", maxFrontmatterDepth)
		}
		if tk.Type != token.SpaceType && tk.Type != token.CommentType {
			prev = tk
		}
	}
	return nil
}
