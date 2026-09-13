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
