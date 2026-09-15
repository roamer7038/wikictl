package page

import (
	"strings"
	"testing"
)

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
		fm, _, _, _ := splitFrontmatter(out)
		m, err := parseFrontmatter(fm)
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
		{"flow sequence with comma", "---\naliases: [x]\n---\n", "a,b",
			"---\naliases: [x, \"a,b\"]\n---\n"},
		{"flow sequence with bracket", "---\naliases: [x]\n---\n", "a]b",
			"---\naliases: [x, \"a]b\"]\n---\n"},
		{"flow sequence with brace", "---\naliases: []\n---\n", "a{b",
			"---\naliases: [\"a{b\"]\n---\n"},
		{"null value with trailing space", "---\naliases: ~ \n---\n", "old",
			"---\naliases: [old] \n---\n"},
		{"null value after tab", "---\naliases:\t~\n---\n", "old",
			"---\naliases:\t[old]\n---\n"},
		{"null value with trailing tab", "---\naliases: ~\t\n---\n", "old",
			"---\naliases: [old]\t\n---\n"},
		{"null word with trailing space", "---\naliases: null \n---\n", "old",
			"---\naliases: [old] \n---\n"},
		{"null word after tab with trailing tab", "---\naliases:\tnull\t\n---\n", "old",
			"---\naliases:\t[old]\t\n---\n"},
		{"null word with comment", "---\naliases: NULL # none\nsummary: s\n---\n", "old",
			"---\naliases: [old] # none\nsummary: s\n---\n"},
		{"null value on the next line", "---\naliases:\n  ~ \nsummary: s\n---\n", "old",
			"---\naliases:\n  [old] \nsummary: s\n---\n"},
		{"null value with comma", "---\naliases: ~\n---\n", "a,b",
			"---\naliases: [\"a,b\"]\n---\n"},
		{"null value with brace", "---\naliases: ~\n---\n", "a}b",
			"---\naliases: [\"a}b\"]\n---\n"},
		{"block sequence with comma", "---\naliases:\n  - x\n---\n", "a,b",
			"---\naliases:\n  - x\n  - a,b\n---\n"},
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
	if relDest("projects/a/x.md", "global/y.md") != "../../global/y.md" {
		t.Error(relDest("projects/a/x.md", "global/y.md"))
	}
	if relDest("global/x.md", "global/y.md") != "y.md" {
		t.Error(relDest("global/x.md", "global/y.md"))
	}
	if relDest("global/x.md", "projects/a/y.md") != "../projects/a/y.md" {
		t.Error(relDest("global/x.md", "projects/a/y.md"))
	}
}

func TestRelocateKeepsLineEndingsAndBOM(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"CRLF with BOM",
			"\xef\xbb\xbf---\r\nsummary: s\r\n---\r\n# t\r\n[a](a.md)\r\n```\r\n[b](b.md)\r\n```\r\n",
			"\xef\xbb\xbf---\r\nsummary: s\r\n---\r\n# t\r\n[a](../x/a.md)\r\n```\r\n[b](b.md)\r\n```\r\n"},
		{"CRLF without frontmatter", "# t\r\n[a](a.md)",
			"# t\r\n[a](../x/a.md)\r\n"},
		{"LF with BOM", "\xef\xbb\xbf---\nsummary: s\n---\n[a](a.md)\n",
			"\xef\xbb\xbf---\nsummary: s\n---\n[a](../x/a.md)\n"},
		{"mixed takes the first line ending", "---\r\nsummary: s\n---\r\n[a](a.md)\n",
			"---\r\nsummary: s\r\n---\r\n[a](../x/a.md)\r\n"},
	} {
		out, n := Relocate([]byte(tc.in), "x/p.md", "y/p.md", nil)
		if n != 1 || string(out) != tc.want {
			t.Errorf("%s: n=%d %q", tc.name, n, out)
		}
	}
}

func TestAddAliasKeepsLineEndingsAndBOM(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"block sequence", "\xef\xbb\xbf---\r\nsummary: a\r\naliases:\r\n  - old\r\n---\r\n# t\r\n",
			"\xef\xbb\xbf---\r\nsummary: a\r\naliases:\r\n  - old\r\n  - x\r\n---\r\n# t\r\n"},
		{"absent", "---\r\nsummary: a\r\n---\r\n# t\r\n",
			"---\r\nsummary: a\r\naliases:\r\n  - x\r\n---\r\n# t\r\n"},
		{"null", "---\r\naliases:\r\n---\r\n",
			"---\r\naliases:\r\n  - x\r\n---\r\n"},
		{"flow sequence", "\xef\xbb\xbf---\r\naliases: [a]\r\n---\r\n",
			"\xef\xbb\xbf---\r\naliases: [a, x]\r\n---\r\n"},
		{"duplicate", "\xef\xbb\xbf---\r\naliases: [x]\r\n---\r\n",
			"\xef\xbb\xbf---\r\naliases: [x]\r\n---\r\n"},
	} {
		if got := string(AddAlias([]byte(tc.in), "x")); got != tc.want {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}

func TestRelocateKeepsForm(t *testing.T) {
	const fm = "---\nsummary: s\n---\n"
	rename := map[string]string{"global/a.md": "global/a2.md"}
	for _, tc := range []struct {
		name, from, to string
		mapping        map[string]string
		in, want       string
		n              int
	}{
		{"unrelated links are kept", "global/unrelated.md", "global/unrelated.md", rename,
			"see [t](other.md \"title\") and [d](./index.md) and `[c](a.md)`\n",
			"see [t](other.md \"title\") and [d](./index.md) and `[c](a.md)`\n", 0},
		{"title", "global/r.md", "global/r.md", rename,
			"[t](a.md \"title\")\n", "[t](a2.md \"title\")\n", 1},
		{"dot slash", "global/r.md", "global/r.md", rename,
			"[d](./a.md)\n", "[d](./a2.md)\n", 1},
		{"fragment and query", "global/r.md", "global/r.md", rename,
			"[f](a.md#x) [q](a.md?v=1#x)\n", "[f](a2.md#x) [q](a2.md?v=1#x)\n", 2},
		{"angle brackets", "global/r.md", "global/r.md", rename,
			"[b](<a.md> \"t\") [c](<./a.md#x>)\n", "[b](<a2.md> \"t\") [c](<./a2.md#x>)\n", 2},
		{"code span", "global/r.md", "global/r.md", rename,
			"`[c](a.md)` and [c](a.md) and `x](a.md)`\n", "`[c](a.md)` and [c](a2.md) and `x](a.md)`\n", 1},
		{"code fence", "global/r.md", "global/r.md", rename,
			"```\n[c](a.md)\n```\n", "```\n[c](a.md)\n```\n", 0},
		{"other directory", "projects/p/r.md", "projects/p/r.md", rename,
			"[a](../../global/a.md \"t\") [i](../../global/./index.md)\n", "[a](../../global/a2.md \"t\") [i](../../global/./index.md)\n", 1},
		{"links section", "global/r.md", "global/r.md", rename,
			"# r\n\n## Links\n- part_of: [a](./a.md \"t\") | n\n- see_also: [o](./other.md)\n- `[a](a.md)`\n",
			"# r\n\n## Links\n- part_of: [a](./a2.md \"t\") | n\n- see_also: [o](./other.md)\n- `[a](a.md)`\n", 1},
		{"urls and fragments only", "global/r.md", "global/r.md", rename,
			"[u](https://e.example/a.md) [h](#a.md)\n", "[u](https://e.example/a.md) [h](#a.md)\n", 0},
		{"moved page", "global/p.md", "projects/a/p.md", map[string]string{"global/p.md": "projects/a/p.md"},
			"[i](./index.md \"t\") [s](../projects/a/x.md#h) [me](p.md#top) `[c](index.md)`\n",
			"[i](../../global/index.md \"t\") [s](x.md#h) [me](p.md#top) `[c](index.md)`\n", 2},
		{"renamed page", "global/p.md", "global/p2.md", map[string]string{"global/p.md": "global/p2.md"},
			"[i](./index.md) [me](./p.md#top)\n", "[i](./index.md) [me](./p2.md#top)\n", 1},
		{"moved along", "global/p.md", "projects/a/p.md", map[string]string{"global/p.md": "projects/a/p.md", "global/q.md": "projects/a/q.md"},
			"[q](./q.md \"t\") [i](index.md)\n", "[q](./q.md \"t\") [i](../../global/index.md)\n", 1},
		{"target moved to another directory", "projects/a/x.md", "projects/a/x.md", map[string]string{"projects/a/y.md": "projects/b/y2.md"},
			"# t\nsee [y](y.md) and [z](../../global/z.md)\n```\n[y](y.md)\n```\n## Links\n- part_of: [y](y.md) | n\n",
			"# t\nsee [y](../b/y2.md) and [z](../../global/z.md)\n```\n[y](y.md)\n```\n## Links\n- part_of: [y](../b/y2.md) | n\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, n := Relocate([]byte(fm+tc.in), tc.from, tc.to, tc.mapping)
			if string(out) != fm+tc.want || n != tc.n {
				t.Errorf("n=%d (want %d)\ngot  %q\nwant %q", n, tc.n, out, fm+tc.want)
			}
		})
	}
}

func TestAddAliasDeepFrontmatter(t *testing.T) {
	in := "---\nx: " + strings.Repeat("[", 30000) + strings.Repeat("]", 30000) + "\n---\n# t\n"
	if got := string(AddAlias([]byte(in), "old")); got != in {
		t.Errorf("deep frontmatter must be left unchanged")
	}
}
