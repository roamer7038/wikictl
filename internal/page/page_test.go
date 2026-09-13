package page

import "testing"

func TestValid(t *testing.T) {
	for _, s := range []string{"a", "a-b1", "0x"} {
		if !ValidSlug(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "-a", "A", "a_b", "ü"} {
		if ValidSlug(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
	if !ValidPagePath("global/x.md") || !ValidPagePath("projects/my-app/x.md") {
		t.Error("valid path rejected")
	}
	for _, p := range []string{"x.md", "global/x", "Global/x.md", ".hidden/x.md", "global/X.md"} {
		if ValidPagePath(p) {
			t.Errorf("%q should be invalid", p)
		}
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
	if !codes["missing_summary"] || !codes["bad_slug"] {
		t.Errorf("issues=%+v", p2.Issues)
	}
	p3 := Parse("global/y.md", []byte("---\nsummary: a: b\n---\n# t\n"))
	if len(p3.Issues) == 0 || p3.Issues[0].Code != "frontmatter_invalid" {
		t.Errorf("%+v", p3.Issues)
	}
	p4 := Parse("global/z.md", []byte("---\nsummary: s\n---\n"))
	if p4.Title != "z" || p4.Body != "" {
		t.Errorf("title=%q body=%q", p4.Title, p4.Body)
	}
}
