package page

import "testing"

const sample = "# Title\n\nbody [x](a.md)\n\n```\n## Links\n# not heading\n```\n\n## Section\n\n## Links\n- part_of: [parent](../global/p.md)\n"

func TestScanFence(t *testing.T) {
	ls := scanLines([]byte(sample), 4)
	if ls[0].N != 4 || ls[0].InFence {
		t.Errorf("first line: %+v", ls[0])
	}
	if !ls[5].InFence || ls[5].Text != "## Links" {
		t.Errorf("fenced Links must be InFence: %+v", ls[5])
	}
}

func TestHeadings(t *testing.T) {
	ls := scanLines([]byte(sample), 4)
	i, notLast, title := headings(ls)
	if i < 0 || ls[i].Text != "## Links" || ls[i].InFence || len(notLast) != 0 || title != 0 {
		t.Fatalf("headings = %d, %v, %d", i, notLast, title)
	}
	if i, notLast, _ := headings(scanLines([]byte("# t\n## Links\n- a: [b](b.md)\n## after\n"), 1)); i != -1 || len(notLast) != 1 {
		t.Error("Links followed by a heading must not be a Links section")
	}
	if i, _, _ := headings(scanLines([]byte("# t\nbody\n"), 1)); i != -1 {
		t.Error("no Links section")
	}
	if _, _, title := headings(scanLines([]byte("## Links\n- a: [b](b.md)\n"), 1)); title != -1 {
		t.Error("the Links heading is not a title")
	}
}

func TestTitleClosingSequence(t *testing.T) {
	for in, want := range map[string]string{
		"# C#":          "C#",
		"# C #":         "C",
		"## a ##   ":    "a",
		"# a\t###":      "a",
		"# C##":         "C##",
		"# x # y":       "x # y",
		"# F# and C# #": "F# and C#",
	} {
		if got := Parse("d/p.md", []byte(in+"\n")).Title; got != want {
			t.Errorf("%q: title=%q, want %q", in, got, want)
		}
	}
}

func TestScanFenceRules(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		in         []bool
	}{
		{"info string does not close", "```\n```bash\n[x](a.md)\n```\n[y](b.md)\n", []bool{true, true, true, true, false}},
		{"other character does not close", "~~~\n```\n~~~\nafter\n", []bool{true, true, true, false}},
		{"shorter fence does not close", "````\n```\nx\n````\nafter\n", []bool{true, true, true, true, false}},
		{"closing fence with trailing spaces", "```\nx\n```  \nafter\n", []bool{true, true, true, false}},
		{"indented up to three spaces", "   ```\nx\n   ```\nafter\n", []bool{true, true, true, false}},
		{"four spaces is not a fence", "    ```\nafter\n", []bool{false, false}},
		{"closing fence indented four spaces", "```\nx\n    ```\nafter\n", []bool{true, true, true, true}},
		{"backtick info string with a backtick", "``` a`b\nafter\n", []bool{false, false}},
		{"tilde info string with a backtick", "~~~ a`b\nx\n~~~\nafter\n", []bool{true, true, true, false}},
		{"two backticks is not a fence", "``\nafter\n", []bool{false, false}},
		{"unclosed fence runs to the end", "```\n~~~\nafter\n", []bool{true, true, true}},
	} {
		ls := scanLines([]byte(tc.body), 1)
		if len(ls) != len(tc.in) {
			t.Fatalf("%s: %d lines", tc.name, len(ls))
		}
		for i, l := range ls {
			if l.InFence != tc.in[i] {
				t.Errorf("%s: line %d %q InFence=%v", tc.name, l.N, l.Text, l.InFence)
			}
		}
	}
}
