package page

import "testing"

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
		fm, _, n, ok := SplitFrontmatter([]byte(c.in))
		if ok != c.wantOK || string(fm) != c.wantFM || n != c.wantLns {
			t.Errorf("%s: got fm=%q ok=%v lines=%d", c.name, fm, ok, n)
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	m, err := ParseFrontmatter([]byte("summary: \"draft: x\"\ntype: policy\ntags: [a, b]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m["summary"] != "draft: x" || m["type"] != "policy" {
		t.Errorf("got %#v", m)
	}
	if _, err := ParseFrontmatter([]byte("summary: a: b\n")); err == nil {
		t.Error("expected error for invalid yaml")
	}
}
