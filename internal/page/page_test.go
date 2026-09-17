package page

import (
	"strings"
	"testing"
)

func TestCheckPath(t *testing.T) {
	for _, p := range []string{"x.md", "README.md", "global/x.md", "projects/my-app/x.md", "global/Foo_bar.md", "global/日本語.md", "global/-x.md", "global/a[b].md", "global/a<b.md", "global/a|b.md"} {
		if err := CheckPath(p); err != nil {
			t.Errorf("%q should be accepted: %v", p, err)
		}
	}
	for _, p := range []string{"", ".", ".md", "global/x", "global/x.md/", "/global/x.md", "global//x.md", "global/.md", ".hidden/x.md", "global/../x.md", "./global/x.md", "global/my page.md", "global/a\tb.md", "global/a\x00b.md", "global/a#b.md", "global/a)b.md", "global/a\"b.md", "global/a\\b.md", "global/a:b.md", "global/a?b.md", "global/a(b.md", "global/<x.md", "a:b/x.md", "global/a`b.md"} {
		if err := CheckPath(p); err == nil {
			t.Errorf("%q should be rejected", p)
		}
	}
	if err := CheckName("my page"); err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Errorf("CheckName: %v", err)
	}
	// A name over MaxNameLen bytes, counted with the .md suffix, cannot be
	// checked out by a clone. CheckName itself does not apply the limit.
	long := strings.Repeat("a", MaxNameLen)
	if err := CheckPath("global/" + long + ".md"); err == nil {
		t.Error("a page name over 255 bytes should be rejected")
	}
	if err := CheckFilePath("global/" + long + "b/x.png"); err == nil {
		t.Error("a directory name over 255 bytes should be rejected")
	}
	if err := CheckPath("global/" + strings.Repeat("a", MaxNameLen-len(".md")) + ".md"); err != nil {
		t.Errorf("a page name of 255 bytes should be accepted: %v", err)
	}
	if err := CheckName(long + "b"); err != nil {
		t.Errorf("CheckName must not apply the length limit: %v", err)
	}
}

// TestIsPagePath checks the one definition of a page that every command
// applies: a name ending in .md with no component starting with a dot, at the
// wiki root or in a directory.
func TestIsPagePath(t *testing.T) {
	for _, p := range []string{"README.md", "global/x.md", "projects/app/x.md", "global/日本語.md", "global/A b.md"} {
		if !IsPagePath(p) {
			t.Errorf("%q should be a page", p)
		}
	}
	for _, p := range []string{"", ".", ".md", "x", "logo.png", "global/x.txt", ".github/x.md", "global/.x.md", "global/x.md/y.txt"} {
		if IsPagePath(p) {
			t.Errorf("%q should not be a page", p)
		}
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

// TestUnicodeCollisions checks the names that differ only by normalisation:
// "ä" as one rune (NFC) and as "a" with a combining diaeresis (NFD).
func TestUnicodeCollisions(t *testing.T) {
	nfc, nfd := "global/ä.md", "global/ä.md"
	got := UnicodeCollisions([]string{nfc, nfd, "global/b.md"})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	for _, is := range got {
		if is.Code != "unicode_collision" || !strings.Contains(is.Message, "differs only by Unicode normalisation") {
			t.Errorf("%+v", is)
		}
	}
	// Both names are reported, each message naming the other spelling.
	msgs := map[string]string{}
	for _, is := range got {
		msgs[is.Path] = is.Message
	}
	if !strings.Contains(msgs[nfc], nfd) || !strings.Contains(msgs[nfd], nfc) {
		t.Errorf("%+v", got)
	}
	// A directory name collides as a file name does.
	if got := UnicodeCollisions([]string{"ä/a.md", "ä/b.md"}); len(got) != 2 {
		t.Errorf("directory: %+v", got)
	}
	// A pair that differs by case alone is a case_collision and nothing else.
	caseOnly := []string{"global/a.md", "global/A.md"}
	if got := UnicodeCollisions(caseOnly); len(got) != 0 {
		t.Errorf("case only: %+v", got)
	}
	if got := CaseCollisions(caseOnly); len(got) != 2 {
		t.Errorf("case only: %+v", got)
	}
	// A pair that differs by case and normalisation at once collides on a file
	// system that normalises names, and no case_collision covers it: "Ö" as
	// "O" with a combining diaeresis (NFD) and "ö" as one rune (NFC).
	both := []string{"global/Ö.md", "global/ö.md"}
	got = UnicodeCollisions(both)
	if len(got) != 2 {
		t.Fatalf("case and normalisation: %+v", got)
	}
	for _, is := range got {
		if is.Code != "unicode_collision" || !strings.Contains(is.Message, "Unicode normalisation and case") {
			t.Errorf("case and normalisation: %+v", is)
		}
	}
	if !strings.Contains(got[0].Message, both[1]) || !strings.Contains(got[1].Message, both[0]) {
		t.Errorf("case and normalisation: %+v", got)
	}
	if got := CaseCollisions(both); len(got) != 0 {
		t.Errorf("case and normalisation is not a case collision: %+v", got)
	}
	// "İ" (U+0130) lowercases to "i" but does not fold to it. The pair differs
	// only by case, so case_collision reports it and unicode_collision stays
	// quiet, although the two names are not equal under strings.EqualFold.
	dotted := []string{"global/İ.md", "global/i.md"}
	if got := UnicodeCollisions(dotted); len(got) != 0 {
		t.Errorf("dotted capital I: %+v", got)
	}
	if got := CaseCollisions(dotted); len(got) != 2 {
		t.Errorf("dotted capital I: %+v", got)
	}
	// Names that do not collide are not reported.
	if got := UnicodeCollisions([]string{nfc, "global/b.md"}); len(got) != 0 {
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

func TestParseDescriptionFallback(t *testing.T) {
	p := Parse("global/d.md", []byte("---\ndescription: from description\n---\n# t\n"))
	if p.Summary != "from description" || len(p.Issues) != 0 {
		t.Errorf("summary=%q issues=%+v", p.Summary, p.Issues)
	}
	p2 := Parse("global/d.md", []byte("---\nsummary: from summary\ndescription: from description\n---\n# t\n"))
	if p2.Summary != "from summary" {
		t.Errorf("summary must win: %q", p2.Summary)
	}
	p3 := Parse("global/d.md", []byte("---\nsummary: \"\"\ndescription: from description\n---\n# t\n"))
	if p3.Summary != "from description" || len(p3.Issues) != 0 {
		t.Errorf("blank summary must fall back: summary=%q issues=%+v", p3.Summary, p3.Issues)
	}
}

func TestParseLinksNotLast(t *testing.T) {
	p := Parse("global/l.md", []byte("---\nsummary: s\n---\n# t\n\n## Links\n- part_of: [i](index.md)\n\n### later\n"))
	if len(p.Issues) != 1 || p.Issues[0].Code != "links_syntax" || p.Issues[0].Line != 6 || len(p.Links) != 0 ||
		p.Issues[0].Message != `"## Links" is not the last heading, so the lines after it are not read as links` {
		t.Errorf("issues=%+v links=%+v", p.Issues, p.Links)
	}
	p = Parse("global/l.md", []byte("---\nsummary: s\n---\n# t\n## Links\n## Links\n- part_of: [i](index.md)\n"))
	if len(p.Issues) != 1 || p.Issues[0].Code != "links_syntax" || p.Issues[0].Line != 5 || len(p.Links) != 1 {
		t.Errorf("earlier Links heading: issues=%+v links=%+v", p.Issues, p.Links)
	}
	p = Parse("global/l.md", []byte("---\nsummary: s\n---\n# t\n```\n## Links\n```\n## Links\n- part_of: [i](index.md)\n"))
	if len(p.Issues) != 0 || len(p.Links) != 1 {
		t.Errorf("fenced Links heading: issues=%+v links=%+v", p.Issues, p.Links)
	}
}

func TestParseLimits(t *testing.T) {
	big := "---\nsummary: s\n---\n# t\n" + strings.Repeat("a", MaxPageSize)
	p := Parse("global/big.md", []byte(big))
	if len(p.Issues) != 1 || p.Issues[0].Code != "page_too_large" || p.Issues[0].Line != 0 || p.Summary != "" || p.Frontmatter != nil || p.Body != "" || p.Title != "big" {
		t.Errorf("big: issues=%+v summary=%q title=%q", p.Issues, p.Summary, p.Title)
	}
	deep := "---\nx: " + strings.Repeat("[", 30000) + strings.Repeat("]", 30000) + "\n---\n# t\n"
	p = Parse("global/deep.md", []byte(deep))
	if len(p.Issues) != 1 || p.Issues[0].Code != "frontmatter_invalid" || p.Title != "t" {
		t.Errorf("deep: issues=%+v title=%q", p.Issues, p.Title)
	}
}
