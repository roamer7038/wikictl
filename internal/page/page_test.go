package page

import (
	"strings"
	"testing"
)

func TestCheckPath(t *testing.T) {
	for _, p := range []string{"global/x.md", "projects/my-app/x.md", "global/Foo_bar.md", "global/日本語.md", "global/-x.md", "global/a[b].md", "global/a<b.md", "global/a|b.md"} {
		if err := CheckPath(p); err != nil {
			t.Errorf("%q should be accepted: %v", p, err)
		}
	}
	for _, p := range []string{"", "x.md", "global/x", "global/x.md/", "/global/x.md", "global//x.md", "global/.md", ".hidden/x.md", "global/../x.md", "./global/x.md", "global/my page.md", "global/a\tb.md", "global/a\x00b.md", "global/a#b.md", "global/a)b.md", "global/a\"b.md", "global/a\\b.md", "global/a:b.md", "global/a?b.md", "global/a(b.md", "global/<x.md", "a:b/x.md", "global/a`b.md"} {
		if err := CheckPath(p); err == nil {
			t.Errorf("%q should be rejected", p)
		}
	}
	if err := CheckName("my page"); err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Errorf("CheckName: %v", err)
	}
}

func TestRecommended(t *testing.T) {
	for _, s := range []string{"a", "a-b1", "0x"} {
		if !Recommended(s) {
			t.Errorf("%q should be recommended", s)
		}
	}
	for _, s := range []string{"", "-a", "A", "a_b", "ü"} {
		if Recommended(s) {
			t.Errorf("%q should not be recommended", s)
		}
	}
}

func TestPathIssues(t *testing.T) {
	if is := PathIssues("global/x.md"); len(is) != 0 {
		t.Errorf("clean path: %+v", is)
	}
	if is := PathIssues("global/my page.md"); len(is) != 1 || is[0].Code != "bad_path" || is[0].Path != "global/my page.md" {
		t.Errorf("bad path: %+v", is)
	}
	if is := PathIssues("Global/x_y.md"); len(is) != 1 || is[0].Code != "name_style" {
		t.Errorf("style: %+v", is)
	}
}

func TestCaseCollisions(t *testing.T) {
	paths := []string{"global/a.md", "global/A.md", "projects/App/x.md", "projects/app/y.md", "global/b.md", "machines/h/c.md", "machines/h/C.md"}
	got := CaseCollisions(paths)
	want := map[string]string{
		"global/A.md":       "global/a.md",
		"global/a.md":       "global/A.md",
		"machines/h/C.md":   "machines/h/c.md",
		"machines/h/c.md":   "machines/h/C.md",
		"projects/App/x.md": "projects/app",
		"projects/app/y.md": "projects/App",
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, is := range got {
		if is.Code != "case_collision" || !strings.Contains(is.Message, want[is.Path]) {
			t.Errorf("%+v", is)
		}
	}
	if got := CaseCollisions([]string{"global/a.md", "global/b.md"}); len(got) != 0 {
		t.Errorf("no collision: %+v", got)
	}
}

func TestParse(t *testing.T) {
	src := "---\nsummary: the answer\ntype: policy\n---\n# The question\n\nbody [a](a.md)\n\n## Links\n- part_of: [p](../../global/p.md)\n"
	p := Parse("projects/x/q.md", []byte(src))
	if p.Summary != "the answer" || p.Title != "The question" || len(p.Links) != 1 || len(p.Mentions) != 1 || len(p.Issues) != 0 {
		t.Errorf("%+v", p)
	}
	if p.Body != "# The question\n\nbody [a](a.md)\n" {
		t.Errorf("body=%q", p.Body)
	}
	p2 := Parse("global/X.md", []byte("# no fm\n"))
	codes := map[string]bool{}
	for _, i := range p2.Issues {
		codes[i.Code] = true
	}
	if !codes["missing_summary"] || !codes["name_style"] || codes["bad_path"] {
		t.Errorf("issues=%+v", p2.Issues)
	}
	p3 := Parse("global/y.md", []byte("---\nsummary: a: b\n---\n# t\n"))
	if len(p3.Issues) == 0 || p3.Issues[0].Code != "frontmatter_invalid" {
		t.Errorf("%+v", p3.Issues)
	}
	if p := Parse("global/a b.md", []byte("---\nsummary: s\n---\n")); len(p.Issues) != 1 || p.Issues[0].Code != "bad_path" {
		t.Errorf("bad path issues=%+v", p.Issues)
	}
	p4 := Parse("global/z.md", []byte("---\nsummary: s\n---\n"))
	if p4.Title != "z" || p4.Body != "" {
		t.Errorf("title=%q body=%q", p4.Title, p4.Body)
	}
}
