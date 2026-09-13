package page

import "testing"

func TestResolveDest(t *testing.T) {
	cases := []struct {
		page, dest, want string
		url, bad         bool
	}{
		{"projects/a/x.md", "../../global/y.md", "global/y.md", false, false},
		{"projects/a/x.md", "y.md#sec", "projects/a/y.md", false, false},
		{"projects/a/x.md", "./y.md \"title\"", "projects/a/y.md", false, false},
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
	body := "# t\n## Links\n- uses:foo.md\n- cites:\n- see_also:foo.md\n- mailto:a@b.example\n- https://\n- p.md \"title\"\n- [t](p.md \"title\")\n- see_also: [t](q.md \"title\")\n- [u](https://x.example/a)\n"
	ls := ScanLines([]byte(body), 1)
	links, issues := ParseLinks(ls[LinksStart(ls):], "global/x.md")
	want := []Link{
		{Type: "see_also", Target: "global/p.md", Line: 9},
		{Type: "see_also", Target: "global/q.md", Line: 10},
		{Type: "see_also", Target: "https://x.example/a", Line: 11, IsURL: true},
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
