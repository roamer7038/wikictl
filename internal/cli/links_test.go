package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// setupLinks creates the wiki of setup and adds a page whose body holds links
// of every kind the links command tells apart. Its lines are numbered from 1,
// so that the tests can name the line of each link:
//
//	 5 the two URLs listed, on one line
//	 6 an image, 7 other schemes, 8 an autolink and a bare URL, none listed
//	 9 a URL the Links section links to as well, so not listed again
//	11 the first link to push.md, 13 the second, which does not add an item
//	16 the Links line of the URL, 17 the Links line of index.md
func setupLinks(t *testing.T) string {
	t.Helper()
	cfg := setup(t)
	pushFiles(t, cfg, map[string]string{
		"global/urls.md": "---\nsummary: urls\n---\n# urls\n" +
			"see [a](https://example.com/a) and [b](http://example.com/b)\n" +
			"![img](https://example.com/i.png)\n" +
			"[m](mailto:x@example.com) and [d](data:text/plain,x)\n" +
			"<https://example.com/auto> and https://example.com/bare\n" +
			"[c](https://example.com/c)\n" +
			"\nbody link to [p](push.md) here\n" +
			"\nand [p again](push.md)\n" +
			"\n## Links\n- cites: https://example.com/c | c\n- see_also: [i](index.md)\n",
	})
	return cfg
}

// TestLinksOut checks the items of one page: the Links section line by line,
// then the body, whose links to pages and to http and https URLs are listed
// once per destination, on the line where each first appears. An image,
// another scheme, an autolink, a bare URL and a URL the Links section already
// links to add no item.
func TestLinksOut(t *testing.T) {
	cfg := setupLinks(t)
	var res struct{ Items []linkItem }
	code, out, errs := runCLI(t, cfg, "", "links", "--json", "-o", "global/urls.md")
	mustUnmarshal(t, out, &res)
	want := []linkItem{
		{"global/urls.md", "out", "cites", "https://example.com/c", "c", 16, true},
		{"global/urls.md", "out", "see_also", "global/index.md", "", 17, false},
		{"global/urls.md", "out", "mentions", "global/push.md", "", 11, false},
		{"global/urls.md", "out", "mentions", "https://example.com/a", "", 5, true},
		{"global/urls.md", "out", "mentions", "http://example.com/b", "", 5, true},
	}
	if code != ExitOK || !slices.Equal(res.Items, want) {
		t.Errorf("links -o: code=%d items=%+v want=%+v errs=%q", code, res.Items, want, errs)
	}
}

// TestLinksInLine checks that the line of an "in" item is the line of the link
// in the page that holds it, the page named by target, not in the page read.
func TestLinksInLine(t *testing.T) {
	cfg := setupLinks(t)
	var res struct{ Items []linkItem }
	code, out, errs := runCLI(t, cfg, "", "links", "--json", "-i", "global/index.md")
	mustUnmarshal(t, out, &res)
	want := []linkItem{
		{"global/index.md", "in", "part_of", "global/push.md", "", 10, false},
		{"global/index.md", "in", "see_also", "global/urls.md", "", 17, false},
	}
	if code != ExitOK || !slices.Equal(res.Items, want) {
		t.Errorf("links -i: code=%d items=%+v want=%+v errs=%q", code, res.Items, want, errs)
	}
}

// columns returns the largest number of tab-separated fields of a line of out.
func columns(out string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if l != "" {
			n = max(n, strings.Count(l, "\t")+1)
		}
	}
	return n
}

// TestLinksPathColumn checks when the path is printed before each link: not
// for a single page, so that the text output of one page is the three columns
// it has always been, but for several paths, for a directory and for no path
// at all. -H always prints it and -h never does.
func TestLinksPathColumn(t *testing.T) {
	cfg := setupLinks(t)
	if _, out, errs := runCLI(t, cfg, "", "links", "-o", "global/push.md"); out != "out\tpart_of\tglobal/index.md\n" {
		t.Errorf("links of one page: %q %q", out, errs)
	}
	if _, out, errs := runCLI(t, cfg, "", "links", "-o", "-H", "global/push.md"); out != "global/push.md\tout\tpart_of\tglobal/index.md\n" {
		t.Errorf("links -H of one page: %q %q", out, errs)
	}
	for _, c := range []struct {
		args []string
		cols int
	}{
		{[]string{"links", "-o", "global/push.md"}, 3},
		{[]string{"links", "-o", "-h", "global/push.md"}, 3},
		{[]string{"links", "-o", "-H", "global/push.md"}, 4},
		{[]string{"links", "-o", "global/push.md", "global/urls.md"}, 4},
		{[]string{"links", "-o", "-h", "global/push.md", "global/urls.md"}, 3},
		{[]string{"links", "-o", "global"}, 4},
		{[]string{"links", "-o", "-h", "global"}, 3},
		{[]string{"links", "-o"}, 4},
		{[]string{"links", "-o", "-h"}, 3},
	} {
		code, out, errs := runCLI(t, cfg, "", c.args...)
		if code != ExitOK || out == "" || columns(out) != c.cols {
			t.Errorf("%v: code=%d columns=%d, want %d: out=%q errs=%q", c.args, code, columns(out), c.cols, out, errs)
		}
	}
}

// TestLinksPaths checks the paths links accepts: several pages, a directory,
// which stands for the pages under it, and none at all, which reads every
// page of the wiki. A path that cannot be read is reported as before, the
// other paths are still listed, and the command exits with 1.
func TestLinksPaths(t *testing.T) {
	cfg := setupLinks(t)
	var res struct{ Items []linkItem }
	pathsOf := func() []string {
		var out []string
		for _, it := range res.Items {
			if !slices.Contains(out, it.Path) {
				out = append(out, it.Path)
			}
		}
		return out
	}
	// Without a path, every page of the wiki, the deprecated ones included.
	code, out, errs := runCLI(t, cfg, "", "links", "--json", "-o")
	mustUnmarshal(t, out, &res)
	if want := []string{"global/push.md", "global/urls.md"}; code != ExitOK || !slices.Equal(pathsOf(), want) {
		t.Errorf("links -o: code=%d paths=%v want=%v errs=%q", code, pathsOf(), want, errs)
	}
	// A directory is the pages under it, and is no longer an error.
	code, out, errs = runCLI(t, cfg, "", "links", "--json", "-o", "global")
	mustUnmarshal(t, out, &res)
	if want := []string{"global/push.md", "global/urls.md"}; code != ExitOK || !slices.Equal(pathsOf(), want) {
		t.Errorf("links -o global: code=%d paths=%v want=%v errs=%q", code, pathsOf(), want, errs)
	}
	// A directory holding no page lists nothing and still succeeds.
	if code, out, errs := runCLI(t, cfg, "", "links", "--json", "-o", "projects"); code != ExitOK || out != `{"items":[]}`+"\n" {
		t.Errorf("links -o projects: code=%d out=%q errs=%q", code, out, errs)
	}
	// Several pages are read in path order, whatever order they are given in.
	code, out, errs = runCLI(t, cfg, "", "links", "--json", "-o", "global/urls.md", "global/push.md")
	mustUnmarshal(t, out, &res)
	if want := []string{"global/push.md", "global/urls.md"}; code != ExitOK || !slices.Equal(pathsOf(), want) {
		t.Errorf("links -o of two pages: code=%d paths=%v want=%v errs=%q", code, pathsOf(), want, errs)
	}
	// A path that cannot be read is reported as cat reports it, the pages of
	// the other paths are still listed, and the command exits with 1.
	if code, _, errs := runCLI(t, cfg, "data\n", "put", "global/data.txt"); code != ExitOK {
		t.Fatalf("put: code=%d errs=%q", code, errs)
	}
	for _, c := range []struct{ path, msg string }{
		{"global/none.md", noSuchFile},
		{"global/data.txt", notAPage},
	} {
		code, out, errs := runCLI(t, cfg, "", "links", "--json", "-o", c.path, "global/push.md")
		mustUnmarshal(t, out, &res)
		want := []linkItem{{"global/push.md", "out", "part_of", "global/index.md", "", 10, false}}
		if code != ExitError || !slices.Equal(res.Items, want) || errs != "wikictl: "+c.path+": "+c.msg+"\n" {
			t.Errorf("links -o %s: code=%d out=%q errs=%q", c.path, code, out, errs)
		}
	}
}

// recordGrep puts a git wrapper first on PATH that appends a line to the
// returned file for every git grep it runs.
func recordGrep(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "greps")
	wrapGit(t, func(real string) string {
		return "*' grep -z --no-column '*)\n" +
			"  echo run >> " + shQuote(log) + "\n" +
			"  exec " + real + " \"$@\"\n" +
			"  ;;\n"
	})
	return log
}

// greps returns the number of git greps recordGrep logged, and empties the log.
func greps(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

// TestLinksOneGrep checks that the backlinks of several pages cost one search,
// not one per page, and that reading every page of the wiki costs none at all,
// since every page is then already a candidate.
func TestLinksOneGrep(t *testing.T) {
	cfg := setupLinks(t)
	if code, _, errs := runCLI(t, cfg, "", "links", "-o", "global/push.md"); code != ExitOK {
		t.Fatalf("filling the mirror: code=%d errs=%q", code, errs)
	}
	log := recordGrep(t)
	args := []string{"--no-fetch", "links", "-i", "global/index.md", "global/push.md", "global/urls.md"}
	if code, _, errs := runCLI(t, cfg, "", args...); code != ExitOK {
		t.Fatalf("links -i of three pages: code=%d errs=%q", code, errs)
	}
	if n := greps(t, log); n != 1 {
		t.Errorf("git grep ran %d times for three pages, want 1", n)
	}
	if code, _, errs := runCLI(t, cfg, "", "--no-fetch", "links"); code != ExitOK {
		t.Fatalf("links: code=%d errs=%q", code, errs)
	}
	if n := greps(t, log); n != 0 {
		t.Errorf("git grep ran %d times for the whole wiki, want 0", n)
	}
}

// TestLinksHelp checks that the help says which page the line of an item is
// in, and recommends -o for a graph of the whole wiki.
func TestLinksHelp(t *testing.T) {
	_, out, _ := runNoConfig(t, "help", "links")
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		`the page of path for "out", and the page of target for "in"`,
		"of the whole wiki use -o",
		"items[] {path, direction, type, target, note, line, url}",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("help links must say %q: %q", want, flat)
		}
	}
}
