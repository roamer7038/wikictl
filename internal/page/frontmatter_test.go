package page

import (
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
