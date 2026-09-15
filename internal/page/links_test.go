package page

import (
	"strings"
	"testing"
	"time"
)

func TestResolveDest(t *testing.T) {
	cases := []struct {
		page, dest, want string
		url, bad         bool
	}{
		{"projects/a/x.md", "../../global/y.md", "global/y.md", false, false},
		{"projects/a/x.md", "y.md#sec", "projects/a/y.md", false, false},
		{"projects/a/x.md", "./y.md?v=1#sec", "projects/a/y.md", false, false},
		{"projects/a/x.md", "https://example.com/p", "https://example.com/p", true, false},
		{"global/x.md", "../../out.md", "", false, true},
		{"global/x.md", "/abs.md", "", false, true},
		{"global/x.md", "img.png", "", false, true},
	}
	for _, c := range cases {
		got, url, err := ResolveDest(c.page, c.dest)
		if (err != nil) != c.bad || got != c.want || url != c.url {
			t.Errorf("%s %s: got %q url=%v err=%v", c.page, c.dest, got, url, err)
		}
	}
}

func TestParseLinks(t *testing.T) {
	body := "# t\n## Links\n- part_of: [parent](../../global/p.md) | upper\n- cites: https://x.example/ | evidence\n- bad line\n\n- see_also: q.md\n"
	ls := ScanLines([]byte(body), 4)
	links, issues := ParseLinks(ls[LinksStart(ls):], "projects/a/x.md")
	if len(links) != 3 {
		t.Fatalf("links=%+v issues=%+v", links, issues)
	}
	if links[0].Type != "part_of" || links[0].Target != "global/p.md" || links[0].Note != "upper" || links[0].Line != 6 {
		t.Errorf("%+v", links[0])
	}
	if !links[1].IsURL || links[2].Target != "projects/a/q.md" {
		t.Errorf("%+v %+v", links[1], links[2])
	}
	if len(issues) != 1 || issues[0].Code != "links_syntax" || issues[0].Line != 8 {
		t.Errorf("issues=%+v", issues)
	}
}

func TestBodyLinks(t *testing.T) {
	body := "# t\nsee [a](a.md) and [b](../../global/b.md#x) and [img](i.png) and [u](https://e/)\n```\n[c](c.md)\n```\n`[d](d.md)` text\n## Links\n- see_also: [a](a.md)\n"
	ls := ScanLines([]byte(body), 1)
	got := BodyLinks(ls[:LinksStart(ls)], "projects/a/x.md")
	if len(got) != 2 || got[0].Target != "projects/a/a.md" || got[1].Target != "global/b.md" || got[0].Type != "mentions" {
		t.Errorf("%+v", got)
	}
}

// TestBodyLinkSyntax checks that lint, backlinks and links (BodyLinks) find
// the same links as mv rewrites (Relocate), when d/p.md moves to e/p.md.
func TestBodyLinkSyntax(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		target   string // the page the link refers to, or "" when there is no link
		out      string // the rewritten line, when there is a link
	}{
		{"angle brackets", "[a](<x.md>)", "d/x.md", "[a](<../d/x.md>)"},
		{"double-quoted title", `[a](x.md "t")`, "d/x.md", `[a](../d/x.md "t")`},
		{"single-quoted title", "[a](x.md 'title')", "d/x.md", "[a](../d/x.md 'title')"},
		{"parenthesized title", "[a](x.md (title))", "d/x.md", "[a](../d/x.md (title))"},
		{"spaces around destination", "[a]( x.md \t\"t\" )", "d/x.md", "[a]( ../d/x.md \t\"t\" )"},
		{"angle brackets, fragment and title", `[a](<./x.md#f> "t")`, "d/x.md", `[a](<../d/x.md#f> "t")`},
		{"query and fragment", "[a](x.md?v=1#f)", "d/x.md", "[a](../d/x.md?v=1#f)"},
		{"balanced parentheses", "[a](x(1).md)", "d/x(1).md", "[a](../d/x(1).md)"},
		{"image", "![a](x.md)", "d/x.md", "![a](../d/x.md)"},
		{"code span between text and destination", "[a]`c`(x.md)", "", ""},
		{"backticks in destination", "[a](x`c`.md)", "d/x`c`.md", "[a](../d/x`c`.md)"},
		{"code span over the destination", "[a `b](x.md)`", "", ""},
		{"code span over the link", "`[a](x.md)`", "", ""},
		{"code span of two backticks", "`` `[a](x.md)` ``", "", ""},
		{"unmatched backtick in text", "[a`b](x.md)", "d/x.md", "[a`b](../d/x.md)"},
		{"backtick runs of different lengths", "``[a](x.md)`", "d/x.md", "``[a](../d/x.md)`"},
		{"escaped backtick", "\\`[a](x.md)`", "d/x.md", "\\`[a](../d/x.md)`"},
		{"escaped backtick before a code span", "\\``[a](x.md)`", "", ""},
		{"escaped quote in title", `[a](x.md "t\")")`, "d/x.md", `[a](../d/x.md "t\")")`},
		{"escaped opening bracket", `\[a](x.md)`, "", ""},
		{"escaped closing bracket", `[a\](x.md)`, "", ""},
		{"no opening bracket", "a](x.md)", "", ""},
		{"link in link text", "[a [b](x.md) c](y.md)", "d/x.md", "[a [b](../d/x.md) c](y.md)"},
		{"unterminated title", `[a](x.md "t)`, "", ""},
		{"unterminated angle brackets", "[a](<x.md)", "", ""},
		{"title without space", `[a](x.md"t")`, "", ""},
		{"deeply nested parentheses", "[a](x" + strings.Repeat("(", 33) + strings.Repeat(")", 33) + ".md)", "", ""},
		{"link text over two lines", "See [the long link\ntext](x.md) here.", "d/x.md", "See [the long link\ntext](../d/x.md) here."},
		{"title on the next line", "[a](x.md\n\"title\")", "d/x.md", "[a](../d/x.md\n\"title\")"},
		{"destination on the next line", "[a](\n  x.md\n  \"t\"\n)", "d/x.md", "[a](\n  ../d/x.md\n  \"t\"\n)"},
		{"emphasis over two lines", "*[emph\nlink](x.md)*", "d/x.md", "*[emph\nlink](../d/x.md)*"},
		{"code span over two lines", "`[a\nb](x.md)`", "", ""},
		{"blank line before title", "[a](x.md\n\n\"t\")", "", ""},
		{"blank line before destination", "[a](\n\nx.md)", "", ""},
		{"heading in link text", "[a\n## h\n](x.md)", "", ""},
		{"code fence in link text", "[a\n```\n```\n](x.md)", "", ""},
		{"line break before parenthesis", "[a]\n(x.md)", "", ""},
		{"line break in angle brackets", "[a](<x\n.md>)", "", ""},
		{"link text over two Links lines", "# t\n## Links\n- see_also: [a\n- b](x.md)", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := ScanLines([]byte(tc.in), 1)
			if ls := LinksStart(lines); ls >= 0 {
				lines = lines[:ls]
			}
			got := BodyLinks(lines, "d/p.md")
			if tc.target == "" && len(got) != 0 || tc.target != "" && (len(got) != 1 || got[0].Target != tc.target) {
				t.Errorf("BodyLinks = %+v, want target %q", got, tc.target)
			}
			want, wantN := tc.out, 1
			if tc.target == "" {
				want, wantN = tc.in, 0
			} else {
				// The link is reported on the line of its destination, which is the
				// line that mv changes.
				in, out := strings.Split(tc.in, "\n"), strings.Split(tc.out, "\n")
				line := 1
				for in[line-1] == out[line-1] {
					line++
				}
				if len(got) == 1 && got[0].Line != line {
					t.Errorf("BodyLinks line = %d, want %d", got[0].Line, line)
				}
			}
			out, n := Relocate([]byte(tc.in+"\n"), "d/p.md", "e/p.md", nil)
			if string(out) != want+"\n" || n != wantN {
				t.Errorf("Relocate = %q, %d, want %q, %d", out, n, want+"\n", wantN)
			}
			crlf := func(s string) string { return utf8BOM + strings.ReplaceAll(s+"\n", "\n", "\r\n") }
			out, n = Relocate([]byte(crlf(tc.in)), "d/p.md", "e/p.md", nil)
			if string(out) != crlf(want) || n != wantN {
				t.Errorf("Relocate with BOM and CRLF = %q, %d, want %q, %d", out, n, crlf(want), wantN)
			}
		})
	}
}

// TestFindLinksLinearTime checks that pathological pages of 1 MiB, the page
// size limit, are read in time proportional to their size. A quadratic scan
// takes tens of seconds for each of them.
func TestFindLinksLinearTime(t *testing.T) {
	const size = 1 << 20
	fill := func(unit string) string { return strings.Repeat(unit, size/len(unit)) }
	for name, s := range map[string]string{
		"openers before links":   strings.Repeat("[", size/2) + strings.Repeat("[a](x.md)", size/2/9),
		"unclosed parentheses":   fill("[]("),
		"unclosed angle bracket": fill("[](<"),
		"unclosed titles":        fill("[](x.md '"),
		"backtick runs":          fill("`a``a```a"),
		"growing backtick runs": func() string {
			var b strings.Builder
			for n := 1; b.Len() < size; n++ {
				b.WriteString(strings.Repeat("`", n) + "a")
			}
			return b.String()
		}(),
		"lines of openers": fill("[\n"),
	} {
		start := time.Now()
		lines := ScanLines([]byte(s), 1)
		BodyLinks(lines, "d/p.md")
		Relocate([]byte(s), "d/p.md", "e/p.md", nil)
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("%s: %v", name, d)
		}
	}
}

// TestParseLinksTargets checks how a target in the Links section is read: a
// page path follows the destination rules of body links, and a target with a
// scheme is a URL as written.
func TestParseLinksTargets(t *testing.T) {
	for _, tc := range []struct {
		target         string
		typed, untyped string // the resolved target, or "" for links_syntax
	}{
		{"<y.md>", "d/y.md", "d/y.md"},
		{"y.md 't'", "d/y.md", ""},
		{"y.md (t)", "d/y.md", ""},
		{`y.md "t"`, "d/y.md", ""},
		{"y.md?v=1", "d/y.md", "d/y.md"},
		{"[t](<y.md> 't')", "d/y.md", "d/y.md"},
		{"my page.md", "", ""},
		{"y.md 't", "", ""},
		{"https://example.com/a)b", "https://example.com/a)b", "https://example.com/a)b"},
		{"https://example.com/(a", "https://example.com/(a", "https://example.com/(a"},
		{"https://example.com/a b", "https://example.com/a b", ""},
		{"mailto:a b", "mailto:a b", ""},
	} {
		for _, c := range []struct{ line, want string }{{"- see_also: " + tc.target, tc.typed}, {"- " + tc.target, tc.untyped}} {
			ls := ScanLines([]byte("# t\n## Links\n"+c.line+"\n"), 1)
			links, issues := ParseLinks(ls[LinksStart(ls):], "d/p.md")
			if c.want == "" && (len(links) != 0 || len(issues) != 1) || c.want != "" && (len(links) != 1 || links[0].Target != c.want || len(issues) != 0) {
				t.Errorf("%q: links=%+v issues=%+v, want %q", c.line, links, issues, c.want)
			}
		}
	}
}

func TestParseLinksBulletMarkers(t *testing.T) {
	body := "# t\n## Links\n* cites: https://x.example/\n+ see_also: q.md\n  - part_of: [p](p.md) | upper\n\t-\tuses: r.md\n-  spaced: s.md\n"
	ls := ScanLines([]byte(body), 1)
	links, issues := ParseLinks(ls[LinksStart(ls):], "global/x.md")
	if len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
	want := []Link{
		{Type: "cites", Target: "https://x.example/", Line: 3, IsURL: true},
		{Type: "see_also", Target: "global/q.md", Line: 4},
		{Type: "part_of", Target: "global/p.md", Note: "upper", Line: 5},
		{Type: "uses", Target: "global/r.md", Line: 6},
		{Type: "spaced", Target: "global/s.md", Line: 7},
	}
	if len(links) != len(want) {
		t.Fatalf("links=%+v", links)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("links[%d]=%+v want %+v", i, links[i], want[i])
		}
	}
}

func TestParseLinksUntyped(t *testing.T) {
	body := "# t\n## Links\n- [q](q.md)\n- r.md | why\n* https://x.example/\n- img.png\n- Bad: r.md\n"
	ls := ScanLines([]byte(body), 1)
	links, issues := ParseLinks(ls[LinksStart(ls):], "global/x.md")
	want := []Link{
		{Type: "see_also", Target: "global/q.md", Line: 3},
		{Type: "see_also", Target: "global/r.md", Note: "why", Line: 4},
		{Type: "see_also", Target: "https://x.example/", Line: 5, IsURL: true},
	}
	if len(links) != len(want) {
		t.Fatalf("links=%+v issues=%+v", links, issues)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("links[%d]=%+v want %+v", i, links[i], want[i])
		}
	}
	if len(issues) != 2 || issues[0].Code != "links_syntax" || issues[0].Line != 6 || issues[1].Code != "links_syntax" || issues[1].Line != 7 {
		t.Errorf("issues=%+v", issues)
	}
}

func TestParseLinksUntypedEdgeCases(t *testing.T) {
	body := "# t\n## Links\n- uses:foo.md\n- cites:\n- see_also:foo.md\n- mailto:a@b.example\n- https://\n- p.md \"title\"\n- [t](p.md \"title\")\n- see_also: [t](q.md \"title\")\n- [u](https://x.example/a)\n- see_also: [t](<r.md> 'title')\n- see_also: s.md \"title\"\n"
	ls := ScanLines([]byte(body), 1)
	links, issues := ParseLinks(ls[LinksStart(ls):], "global/x.md")
	want := []Link{
		{Type: "see_also", Target: "global/p.md", Line: 9},
		{Type: "see_also", Target: "global/q.md", Line: 10},
		{Type: "see_also", Target: "https://x.example/a", Line: 11, IsURL: true},
		{Type: "see_also", Target: "global/r.md", Line: 12},
		{Type: "see_also", Target: "global/s.md", Line: 13},
	}
	if len(links) != len(want) {
		t.Fatalf("links=%+v issues=%+v", links, issues)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("links[%d]=%+v want %+v", i, links[i], want[i])
		}
	}
	wantLines := []int{3, 4, 5, 6, 7, 8}
	if len(issues) != len(wantLines) {
		t.Fatalf("issues=%+v", issues)
	}
	for i, n := range wantLines {
		if issues[i].Code != "links_syntax" || issues[i].Line != n {
			t.Errorf("issues[%d]=%+v want links_syntax at line %d", i, issues[i], n)
		}
	}
}
