package wiki

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
)

// fakeStore is a Store over files kept in memory, keyed by path.
type fakeStore map[string]string

func (f fakeStore) List() ([]string, error) {
	var out []string
	for p := range f {
		if !strings.HasSuffix(p, ".md") || !strings.Contains(p, "/") || strings.HasPrefix(p, ".") || strings.Contains(p, "/.") {
			continue
		}
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
}

func (f fakeStore) Grep(words []string) ([]string, error) {
	paths, _ := f.List()
	var out []string
	for _, p := range paths {
		n := 0
		for _, w := range words {
			if strings.Contains(strings.ToLower(f[p]), strings.ToLower(w)) {
				n++
			}
		}
		if n == len(words) {
			out = append(out, p)
		}
	}
	return out, nil
}

func blobSHA(c string) string {
	h := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(c), c)))
	return hex.EncodeToString(h[:])
}

func (f fakeStore) Stat(paths []string) (map[string]repo.Object, error) {
	out := map[string]repo.Object{}
	for _, p := range paths {
		if c, ok := f[p]; ok {
			out[p] = repo.Object{SHA: blobSHA(c), Size: int64(len(c))}
		}
	}
	return out, nil
}

func (f fakeStore) CatSHA(paths []string) (map[string][]byte, map[string]string, error) {
	contents, shas := map[string][]byte{}, map[string]string{}
	for _, p := range paths {
		if c, ok := f[p]; ok {
			contents[p], shas[p] = []byte(c), blobSHA(c)
		}
	}
	return contents, shas, nil
}

func (f fakeStore) CatLimit(paths []string, max int64) (map[string][]byte, map[string]repo.Object, error) {
	contents, large := map[string][]byte{}, map[string]repo.Object{}
	for _, p := range paths {
		c, ok := f[p]
		switch {
		case !ok:
		case int64(len(c)) > max:
			large[p] = repo.Object{SHA: blobSHA(c), Size: int64(len(c))}
		default:
			contents[p] = []byte(c)
		}
	}
	return contents, large, nil
}

func (f fakeStore) GrepDeprecated() (map[string]bool, error) {
	paths, _ := f.List()
	out := map[string]bool{}
	for _, p := range paths {
		if strings.Contains(f[p], "deprecated") {
			out[p] = true
		}
	}
	return out, nil
}

func TestDeprecated(t *testing.T) {
	s := fakeStore{
		"global/plain.md":    "---\nstatus: deprecated\n---\n# p\n",
		"global/quoted.md":   "---\nsummary: q\nstatus: \"deprecated\"\n---\n# q\n",
		"global/body.md":     "---\nsummary: b\n---\n# b\n```\nstatus: deprecated\n```\n",
		"global/other.md":    "---\nstatus: deprecated-soon\n---\n# o\n",
		"global/nextline.md": "---\nstatus:\n  deprecated\n---\n# n\n",
		"global/flow.md":     "---\n{summary: f, status: deprecated}\n---\n# f\n",
		"global/spaced.md":   "---\nstatus : deprecated # old\n---\n# s\n",
		"global/invalid.md":  "---\nsummary: [unclosed\nstatus: deprecated\n---\n# i\n",
		"global/big.md":      "---\nstatus: deprecated\n---\n# big\n" + strings.Repeat("x", page.MaxPageSize),
	}
	got, err := Deprecated(s)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"global/plain.md": true, "global/quoted.md": true, "global/nextline.md": true, "global/flow.md": true, "global/spaced.md": true}
	if !maps.Equal(got, want) {
		t.Errorf("Deprecated = %v, want %v", got, want)
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"global/a.md":   "global/a.md",
		"/global/a.md":  "global/a.md",
		"//global/a.md": "global/a.md",
		"./global/":     "global",
		"global//a.md":  "global/a.md",
		"a/../b":        "b",
		"global/*.md":   "global/*.md",
		"":              ".",
		"/":             ".",
		"./":            ".",
	} {
		if got, err := Clean(in); err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"..", "../x", "/../x", "a/../../x", "./../"} {
		if got, err := Clean(in); !errors.Is(err, ErrOutside) {
			t.Errorf("Clean(%q) = %q, %v; want ErrOutside", in, got, err)
		}
	}
	for _, in := range []string{"global/push.md\nglobal/index.md", "global/a.md\r", "global/\ta.md", "global/a\x00.md", "global/a\u0085.md", "\n"} {
		if got, err := Clean(in); !errors.Is(err, ErrControl) {
			t.Errorf("Clean(%q) = %q, %v; want ErrControl", in, got, err)
		}
	}
}

func TestBacklinks(t *testing.T) {
	s := fakeStore{
		"global/index.md": "# i\n",
		"global/a.md":     "# a\n[i](index.md)\n\n## Links\n- part_of: [i](index.md)\n",
		"global/c.md":     "# c\n`[i](index.md)`\nhttps://example.com/index.md\n",
		"projects/p/b.md": "# b\nsee [i](../../global/index.md)\n",
		"projects/p/x.md": "# x\n[i](index.md)\n",
	}
	got, err := Backlinks(s, "global/index.md")
	if err != nil {
		t.Fatal(err)
	}
	want := []Backlink{{"global/a.md", "part_of"}, {"projects/p/b.md", "mentions"}}
	if !slices.Equal(got, want) {
		t.Errorf("Backlinks = %v, want %v", got, want)
	}
}

func TestBacklinksOfLargePage(t *testing.T) {
	s := fakeStore{
		"global/index.md": "# i\n",
		"global/big.md":   "# big\n[i](index.md)\n" + strings.Repeat("x", page.MaxPageSize),
	}
	if got, err := Backlinks(s, "global/index.md"); err != nil || len(got) != 0 {
		t.Errorf("a page over the size limit is not parsed: %v %v", got, err)
	}
}

func TestBrokenLinks(t *testing.T) {
	s := fakeStore{
		"README.md":          "# wiki\n",
		"global/exists.md":   "# e\n",
		"global/sub.md/a.md": "# a\n",
	}
	pg := page.Parse("global/a.md", []byte("# a\n[r](../README.md) [e](exists.md)\n[m](missing.md)\n[d](sub.md)\n\n## Links\n- cites: https://example.com/x.md\n"))
	got, err := BrokenLinks(s, []*page.Page{pg})
	if err != nil {
		t.Fatal(err)
	}
	var targets []string
	for _, is := range got {
		if is.Code != "broken_link" || is.Path != "global/a.md" || is.Line != 3 && is.Line != 4 {
			t.Errorf("issue: %+v", is)
		}
		targets = append(targets, strings.TrimPrefix(is.Message, "link target does not exist: "))
	}
	if !slices.Equal(targets, []string{"global/missing.md", "global/sub.md"}) {
		t.Errorf("broken targets: %v", targets)
	}
}

func TestRelocate(t *testing.T) {
	s := fakeStore{
		"global/a.md":     "# a\n[b](b.md)\n",
		"global/b.md":     "# b\n",
		"global/d.md":     "# d\n[b](b.md)\n",
		"projects/p/c.md": "# c\n[a](../../global/a.md)\n",
	}
	changes, err := Relocate(s, map[string]string{"global/a.md": "projects/p/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]repo.Change{}
	for _, c := range changes {
		byPath[c.Path] = c
	}
	if len(changes) != 3 {
		t.Fatalf("changes: %+v", changes)
	}
	if c := byPath["projects/p/a.md"]; c.Base == nil || *c.Base != "" || c.Delete || !strings.Contains(string(c.Content), "[b](../../global/b.md)") {
		t.Errorf("moved page: %+v %q", c, c.Content)
	}
	if c := byPath["global/a.md"]; c.Base == nil || *c.Base != blobSHA(s["global/a.md"]) || !c.Delete {
		t.Errorf("deleted page: %+v", c)
	}
	if c := byPath["projects/p/c.md"]; c.Base == nil || *c.Base != blobSHA(s["projects/p/c.md"]) || !strings.Contains(string(c.Content), "[a](a.md)") {
		t.Errorf("referrer: %+v %q", c, c.Content)
	}
}
