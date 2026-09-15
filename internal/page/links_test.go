package page

import "testing"

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
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := BodyLinks(ScanLines([]byte(tc.in), 1), "d/p.md")
			if tc.target == "" && len(got) != 0 || tc.target != "" && (len(got) != 1 || got[0].Target != tc.target) {
				t.Errorf("BodyLinks = %+v, want target %q", got, tc.target)
			}
			want, wantN := tc.out, 1
			if tc.target == "" {
				want, wantN = tc.in, 0
			}
			out, n := Relocate([]byte(tc.in+"\n"), "d/p.md", "e/p.md", nil)
			if string(out) != want+"\n" || n != wantN {
				t.Errorf("Relocate = %q, %d, want %q, %d", out, n, want+"\n", wantN)
			}
		})
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
