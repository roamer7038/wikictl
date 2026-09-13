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
