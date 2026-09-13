package page

import "testing"

func TestAddAlias(t *testing.T) {
	in := "---\nsummary: a\naliases:\n  - old\n---\n# t\n"
	out := string(AddAlias([]byte(in), "older"))
	if out != "---\nsummary: a\naliases:\n  - old\n  - older\n---\n# t\n" {
		t.Errorf("%q", out)
	}
	in2 := "---\nsummary: a\n---\n# t\n"
	if got := string(AddAlias([]byte(in2), "x")); got != "---\nsummary: a\naliases:\n  - x\n---\n# t\n" {
		t.Errorf("%q", got)
	}
	if got := string(AddAlias([]byte(in), "old")); got != in {
		t.Error("duplicate alias must not change")
	}
	// Any name that put and mv accept must stay a string alias in valid YAML.
	for _, a := range []string{"[x]", "'q", "a:b", "*star", "&x", "!x", "%x", "@x", "{x}", "日本語", "Foo_bar", "true", "12", "-x", "?x", "a'b"} {
		out := AddAlias([]byte(in2), a)
		fm, _, _, _ := SplitFrontmatter(out)
		m, err := ParseFrontmatter(fm)
		if err != nil {
			t.Errorf("alias %q: %v in %q", a, err, out)
			continue
		}
		if l, ok := m["aliases"].([]any); !ok || len(l) != 1 || l[0] != a {
			t.Errorf("alias %q decoded as %#v", a, m["aliases"])
		}
		if again := AddAlias(out, a); string(again) != string(out) {
			t.Errorf("duplicate alias %q must not change: %q", a, again)
		}
	}
}

func TestAddAliasFrontmatterForms(t *testing.T) {
	for _, tc := range []struct {
		name, in, alias, want string
	}{
		{"flow sequence", "---\naliases: [x]\n---\n# t\n", "old",
			"---\naliases: [x, old]\n---\n# t\n"},
		{"empty flow sequence", "---\naliases: []\n---\n", "old",
			"---\naliases: [old]\n---\n"},
		{"multi-line flow sequence", "---\naliases: [x,\n  y,\n]\ntags: [a]\n---\n", "old",
			"---\naliases: [x,\n  y,\n old]\ntags: [a]\n---\n"},
		{"flow sequence with duplicate", "---\naliases: [\"old\"]\n---\n", "old",
			"---\naliases: [\"old\"]\n---\n"},
		{"non-indented block sequence", "---\naliases:\n- y\ntags:\n  - a\n---\n", "old",
			"---\naliases:\n- y\n- old\ntags:\n  - a\n---\n"},
		{"same item in another key", "---\ntags:\n  - d\n---\n", "d",
			"---\ntags:\n  - d\naliases:\n  - d\n---\n"},
		{"same item in another key with aliases", "---\ntags:\n  - d\naliases:\n  - x\n---\n", "d",
			"---\ntags:\n  - d\naliases:\n  - x\n  - d\n---\n"},
		{"comments and other keys", "---\n# head\nsummary: a # s\naliases:\n  - x # note\n\n# about tags\ntags: [a, b]\n---\n# t\n", "old",
			"---\n# head\nsummary: a # s\naliases:\n  - x # note\n  - old\n\n# about tags\ntags: [a, b]\n---\n# t\n"},
		{"multi-line entry", "---\naliases:\n  - |\n    a\n\n    b\nsummary: s\n---\n", "old",
			"---\naliases:\n  - |\n    a\n\n    b\n  - old\nsummary: s\n---\n"},
		{"empty value", "---\naliases: # none\nsummary: s\n---\n", "old",
			"---\naliases: # none\n  - old\nsummary: s\n---\n"},
		{"empty value before another key", "---\naliases:\nsummary: s\n---\n", "old",
			"---\naliases:\n  - old\nsummary: s\n---\n"},
		{"empty value at the end", "---\nsummary: s\naliases:\n---\n", "old",
			"---\nsummary: s\naliases:\n  - old\n---\n"},
		{"null value", "---\naliases: ~\n---\n", "old",
			"---\naliases: [old]\n---\n"},
		{"multibyte flow sequence", "---\nsummary: 日本語\naliases: [日本]\n---\n", "old",
			"---\nsummary: 日本語\naliases: [日本, old]\n---\n"},
		{"empty frontmatter", "---\n---\n# t\n", "old",
			"---\naliases:\n  - old\n---\n# t\n"},
		{"scalar value", "---\naliases: x\n---\n", "old",
			"---\naliases: x\n---\n"},
		{"invalid yaml", "---\naliases: [x\n---\n", "old",
			"---\naliases: [x\n---\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(AddAlias([]byte(tc.in), tc.alias))
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRelDest(t *testing.T) {
	if RelDest("projects/a/x.md", "global/y.md") != "../../global/y.md" {
		t.Error(RelDest("projects/a/x.md", "global/y.md"))
	}
	if RelDest("global/x.md", "global/y.md") != "y.md" {
		t.Error(RelDest("global/x.md", "global/y.md"))
	}
	if RelDest("global/x.md", "projects/a/y.md") != "../projects/a/y.md" {
		t.Error(RelDest("global/x.md", "projects/a/y.md"))
	}
}

func TestRelocateRewritesReferences(t *testing.T) {
	// A page that stays in place: only links to mapped targets change, and
	// links inside code fences are left alone.
	in := "---\nsummary: a\n---\n# t\nsee [y](y.md) and [z](../../global/z.md)\n```\n[y](y.md)\n```\n## Links\n- part_of: [y](y.md) | n\n"
	out, n := Relocate([]byte(in), "projects/a/x.md", "projects/a/x.md", func(target string) (string, bool) {
		if target == "projects/a/y.md" {
			return "projects/b/y2.md", true
		}
		return "", false
	})
	want := "---\nsummary: a\n---\n# t\nsee [y](../b/y2.md) and [z](../../global/z.md)\n```\n[y](y.md)\n```\n## Links\n- part_of: [y](../b/y2.md) | n\n"
	if n != 2 || string(out) != want {
		t.Errorf("n=%d\n%s", n, out)
	}
}

func TestRelocateMovedPage(t *testing.T) {
	// A page moved from global/ to projects/a/: its own links are re-based
	// and fragments are preserved.
	in := "---\nsummary: a\n---\n# t\n[i](index.md) [s](../projects/a/x.md#h)\n"
	out, n := Relocate([]byte(in), "global/p.md", "projects/a/p.md", nil)
	want := "---\nsummary: a\n---\n# t\n[i](../../global/index.md) [s](x.md#h)\n"
	if n != 2 || string(out) != want {
		t.Errorf("n=%d\n%s", n, out)
	}
}

func TestRelocateWithMapper(t *testing.T) {
	// global/p.md moves to projects/a/p.md while global/q.md moves along with it.
	in := "---\nsummary: a\n---\n# t\n[q](q.md) [i](index.md)\n"
	out, n := Relocate([]byte(in), "global/p.md", "projects/a/p.md", func(target string) (string, bool) {
		if target == "global/q.md" {
			return "projects/a/q.md", true
		}
		return "", false
	})
	want := "---\nsummary: a\n---\n# t\n[q](q.md) [i](../../global/index.md)\n"
	if n != 1 || string(out) != want {
		t.Errorf("n=%d\n%s", n, out)
	}
}
