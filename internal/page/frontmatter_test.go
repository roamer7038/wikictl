package page

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantFM  string
		wantOK  bool
		wantLns int
	}{
		{"basic", "---\nsummary: a\n---\n# T\n", "summary: a\n", true, 3},
		{"crlf", "---\r\nsummary: a\r\n---\r\nbody\r\n", "summary: a\r\n", true, 3},
		{"bom", "\xef\xbb\xbf---\nsummary: a\n---\n", "summary: a\n", true, 3},
		{"none", "# T\n", "", false, 0},
		{"unclosed", "---\nsummary: a\n", "", false, 0},
	}
	for _, c := range cases {
		fm, _, n, ok := splitFrontmatter([]byte(c.in))
		if ok != c.wantOK || string(fm) != c.wantFM || n != c.wantLns {
			t.Errorf("%s: got fm=%q ok=%v lines=%d", c.name, fm, ok, n)
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	m, err := parseFrontmatter([]byte("summary: \"draft: x\"\ntype: policy\ntags: [a, b]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m["summary"] != "draft: x" || m["type"] != "policy" {
		t.Errorf("got %#v", m)
	}
	if _, err := parseFrontmatter([]byte("summary: a: b\n")); err == nil {
		t.Error("expected error for invalid yaml")
	}
}

// TestFrontmatter checks the keys, their lines counted from the start of the
// file, and their values, which come from decoding the whole frontmatter so
// that anchors and merge keys are resolved as parseFrontmatter resolves them.
func TestFrontmatter(t *testing.T) {
	keys, ok, err := Frontmatter([]byte("---\nsummary: s\ntype: adr\n---\n# t\n"))
	if err != nil || !ok || len(keys) != 2 ||
		keys[0] != (FrontmatterKey{"summary", 2, "s"}) || keys[1] != (FrontmatterKey{"type", 3, "adr"}) {
		t.Errorf("plain: keys=%+v ok=%v err=%v", keys, ok, err)
	}
	// A BOM is not a line of its own.
	if keys, _, _ := Frontmatter([]byte("\xef\xbb\xbf---\nsummary: s\n---\n")); len(keys) != 1 || keys[0].Line != 2 {
		t.Errorf("bom: %+v", keys)
	}
	// A merge key gives its line to every key it brings in and is not listed
	// itself; a key written at the top level keeps its own line.
	keys, _, err = Frontmatter([]byte("---\nd: &d\n  a: 1\n  b: 2\n<<: *d\nb: 9\n---\n"))
	if err != nil || len(keys) != 3 || keys[0].Key != "d" ||
		keys[1] != (FrontmatterKey{"a", 5, uint64(1)}) || keys[2] != (FrontmatterKey{"b", 6, uint64(9)}) {
		t.Errorf("merge: keys=%+v err=%v", keys, err)
	}
	// A merge of a sequence of aliases is resolved in order.
	keys, _, err = Frontmatter([]byte("---\nx: &x {a: 1}\ny: &y {b: 2}\n<<: [*x, *y]\n---\n"))
	if err != nil || len(keys) != 4 || keys[2] != (FrontmatterKey{"a", 4, uint64(1)}) ||
		keys[3] != (FrontmatterKey{"b", 4, uint64(2)}) {
		t.Errorf("merge list: keys=%+v err=%v", keys, err)
	}
	// So is a mapping written in place of an alias.
	keys, _, err = Frontmatter([]byte("---\n<<: {c: 3}\nd: 4\n---\n"))
	if err != nil || len(keys) != 2 || keys[0] != (FrontmatterKey{"c", 2, uint64(3)}) ||
		keys[1] != (FrontmatterKey{"d", 3, uint64(4)}) {
		t.Errorf("merge inline: keys=%+v err=%v", keys, err)
	}
	// Numbers that JSON cannot hold become the strings YAML writes them as,
	// nested values included.
	keys, _, err = Frontmatter([]byte("---\nn: [.inf, -.inf, {k: .nan}]\n---\n"))
	if err != nil || len(keys) != 1 {
		t.Fatalf("infinity: keys=%+v err=%v", keys, err)
	}
	if b, err := json.Marshal(keys[0].Value); err != nil || string(b) != `[".inf","-.inf",{"k":".nan"}]` {
		t.Errorf("infinity: %s %v", b, err)
	}
	for _, c := range []struct {
		name, in string
		keys     int
		ok       bool
		bad      bool
	}{
		{"empty", "---\n---\n", 0, true, false},
		{"blank", "---\n\n---\n", 0, true, false},
		{"none", "# t\n", 0, false, false},
		{"unclosed", "---\nsummary: s\n", 0, false, false},
		{"invalid", "---\nsummary: [\n---\n", 0, false, true},
		{"sequence", "---\n- a\n---\n", 0, false, true},
		{"duplicate", "---\na: 1\na: 2\n---\n", 0, false, true},
		// An alias that refers to itself must not loop.
		{"cycle", "---\na: &a\n  <<: *a\n---\n", 0, false, true},
	} {
		keys, ok, err := Frontmatter([]byte(c.in))
		if ok != c.ok || (err != nil) != c.bad || len(keys) != c.keys {
			t.Errorf("%s: keys=%+v ok=%v err=%v", c.name, keys, ok, err)
		}
		if c.ok && c.keys == 0 && keys == nil {
			t.Errorf("%s: an empty frontmatter must give an empty list, not nil", c.name)
		}
	}
}

func TestFrontmatterLimits(t *testing.T) {
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	var block, blockDeep strings.Builder
	for i := 0; i < maxFrontmatterDepth; i++ {
		fmt.Fprintf(&block, "%sk%d:\n", strings.Repeat(" ", i), i)
	}
	blockDeep.WriteString(block.String())
	fmt.Fprintf(&blockDeep, "%sk:\n", strings.Repeat(" ", maxFrontmatterDepth))
	var mixed strings.Builder
	for i := 0; i < maxFrontmatterDepth/2+1; i++ {
		fmt.Fprintf(&mixed, "%s- k%d:\n", strings.Repeat("  ", i), i)
	}
	var siblings strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&siblings, "a%d: 1\nb%d:\n- x\n- y\n", i, i)
	}
	ok := []struct{ name, in string }{
		{"size at limit", "summary: " + strings.Repeat("a", maxFrontmatterSize-len("summary: \n")) + "\n"},
		{"flow at limit", "x: " + nest(maxFrontmatterDepth-1) + "\n"},
		{"block at limit", block.String()},
		{"brackets in strings", "summary: \"" + strings.Repeat("[", 500) + "\"\nnote: |\n  " + strings.Repeat("{", 500) + "\ntags: [a, b]\n"},
		{"sibling keys", siblings.String()},
	}
	for _, c := range ok {
		if _, err := parseFrontmatter([]byte(c.in)); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	bad := []struct{ name, in, msg string }{
		{"size", "summary: " + strings.Repeat("a", maxFrontmatterSize) + "\n", "larger than"},
		{"flow", "x: " + nest(maxFrontmatterDepth) + "\n", "deeper than"},
		{"deep flow", "x: " + nest(30000) + "\n", "deeper than"},
		{"block", blockDeep.String(), "deeper than"},
		{"compact sequence", strings.Repeat("- ", maxFrontmatterDepth+1) + "x\n", "deeper than"},
		{"sequence under key", mixed.String(), "deeper than"},
	}
	for _, c := range bad {
		if _, err := parseFrontmatter([]byte(c.in)); err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: err=%v", c.name, err)
		}
	}
}
