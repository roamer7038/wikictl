// Package wiki implements what the commands do with the pages of a wiki,
// independent of how the files are stored: cleaning the paths given on the
// command line, finding the links between pages, broken links and deprecated
// pages, and building the changes that moving pages needs.
package wiki

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
)

// Store reads the files of one state of the wiki. *repo.Repo implements it.
type Store interface {
	// GrepRecords runs git grep with flags and patterns on the files under
	// dirs and returns one record per entry of the output.
	GrepRecords(flags, patterns, dirs []string) ([][]string, error)
	// Stat returns the sha and size of every path that is a file.
	Stat(paths []string) (map[string]repo.Object, error)
	// CatSHA returns the contents and blob shas of the paths that are files.
	CatSHA(paths []string) (map[string][]byte, map[string]string, error)
	// CatLimit returns the contents of the files of at most max bytes, and
	// the sha and size of every file.
	CatLimit(paths []string, max int64) (map[string][]byte, map[string]repo.Object, error)
	// CheckMissing returns an error for paths that Stat or CatSHA did not
	// return when one of them is a file that cannot be read, or when it
	// cannot be told whether they exist.
	CheckMissing(paths []string) error
}

// grepPages returns the pages that have a line matching pattern, given to
// "git grep -l" with flags.
func grepPages(s Store, flags []string, pattern string) ([]string, error) {
	records, err := s.GrepRecords(append(flags, "-l"), []string{pattern}, nil)
	var out []string
	for _, rec := range records {
		if page.IsPagePath(rec[0]) {
			out = append(out, rec[0])
		}
	}
	return out, err
}

// Pages is the files read by ReadPages.
type Pages struct {
	contents map[string][]byte      // files of at most page.MaxPageSize bytes
	Objects  map[string]repo.Object // sha and size of every file
}

// ReadPages reads paths with one lookup of their sizes and one read of the
// contents of the files that are not over page.MaxPageSize. A path that is a
// file the store cannot read is an error.
func ReadPages(s Store, paths []string) (Pages, error) {
	contents, objs, err := s.CatLimit(paths, page.MaxPageSize)
	return Pages{contents, objs}, err
}

// Exists reports whether p is a file.
func (ps Pages) Exists(p string) bool {
	_, ok := ps.Objects[p]
	return ok
}

// Parse returns page.Parse of p, and page.TooLarge for a file over the limit.
func (ps Pages) Parse(p string) *page.Page {
	if ps.Objects[p].Size > page.MaxPageSize {
		return page.TooLarge(p)
	}
	return page.Parse(p, ps.contents[p])
}

// Frontmatter returns the top-level keys of the frontmatter of p as
// page.Frontmatter does; ok is false when the file has no frontmatter. A file
// over page.MaxPageSize is not parsed, and a frontmatter that does not parse
// is reported as the issue lint reports for it.
func (ps Pages) Frontmatter(p string) (keys []page.FrontmatterKey, ok bool, iss *page.Issue) {
	if ps.Objects[p].Size > page.MaxPageSize {
		too := page.TooLargeIssue(p)
		return nil, false, &too
	}
	keys, ok, err := page.Frontmatter(ps.contents[p])
	if err != nil {
		return nil, false, &page.Issue{Path: p, Line: 1, Code: "frontmatter_invalid", Message: err.Error()}
	}
	return keys, ok, nil
}

// Deprecated returns the set of pages whose frontmatter has status:
// deprecated. The candidates are the pages that contain the word, which any
// way of writing status: deprecated in YAML does; the frontmatter of each
// decides, so that such a line in the body does not count and a quoted value
// does.
func Deprecated(s Store) (map[string]bool, error) {
	paths, err := grepPages(s, []string{"-F"}, "deprecated")
	if err != nil {
		return nil, err
	}
	pages, err := ReadPages(s, paths)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, p := range paths {
		if status, _ := pages.Parse(p).Frontmatter["status"].(string); status == "deprecated" {
			out[p] = true
		}
	}
	return out, nil
}

// Clean turns a path given on the command line into a path relative to the
// wiki root. A leading "/" or "./" and a trailing "/" make no difference, and
// the root itself is ".". Wildcards are not interpreted. An empty path is
// kept as it is: it names no file, and path.Clean would turn it into the root.
func Clean(p string) (string, error) {
	if strings.ContainsFunc(p, unicode.IsControl) {
		return "", fmt.Errorf("path contains a control character: %q", p)
	}
	if p == "" {
		return "", nil
	}
	c := path.Clean(strings.TrimLeft(p, "/"))
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("path is outside the wiki: %s", p)
	}
	return c, nil
}

// Backlink is a page that links to another page, with the type of the link:
// the type from its Links section, or "mentions" for a link in its body.
type Backlink struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// Backlinks greps the wiki for the file name of target, ignoring case as
// repo.FoldPattern does, to find candidate pages, then parses each candidate
// and keeps those whose links resolve to target. Typed links win over body
// mentions.
func Backlinks(s Store, target string) ([]Backlink, error) {
	cands, err := grepPages(s, []string{"-E"}, repo.FoldPattern(path.Base(target)))
	if err != nil {
		return nil, err
	}
	pages, err := ReadPages(s, cands)
	if err != nil {
		return nil, err
	}
	out := []Backlink{}
	for _, cp := range cands {
		if cp == target {
			continue
		}
		pg := pages.Parse(cp)
		typed := false
		for _, l := range pg.Links {
			if !l.IsURL && l.Target == target {
				out = append(out, Backlink{cp, l.Type})
				typed = true
			}
		}
		if typed {
			continue
		}
		for _, m := range pg.Mentions {
			if m.Target == target {
				out = append(out, Backlink{cp, "mentions"})
				break
			}
		}
	}
	return out, nil
}

// BrokenLinks returns a broken_link issue for every link and mention of pages
// whose target is not a file in the wiki. lint and put share it so that both
// agree on which targets exist. A target that is a file the store cannot read
// is an error, found with one CheckMissing call for all the targets that Stat
// did not return.
func BrokenLinks(s Store, pages []*page.Page) ([]page.Issue, error) {
	var targets []string
	for _, pg := range pages {
		for _, l := range append(pg.Links, pg.Mentions...) {
			if !l.IsURL {
				targets = append(targets, l.Target)
			}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	found, err := s.Stat(targets)
	if err != nil {
		return nil, err
	}
	var absent []string
	for _, t := range targets {
		if _, ok := found[t]; !ok {
			absent = append(absent, t)
		}
	}
	if err := s.CheckMissing(slices.Compact(slices.Sorted(slices.Values(absent)))); err != nil {
		return nil, err
	}
	var items []page.Issue
	for _, pg := range pages {
		for _, l := range append(pg.Links, pg.Mentions...) {
			if _, ok := found[l.Target]; !l.IsURL && !ok {
				items = append(items, page.Issue{Path: pg.Path, Line: l.Line, Code: "broken_link", Message: "link target does not exist: " + l.Target})
			}
		}
	}
	return items, nil
}

// Relocate builds, from the files of the whole tree in entries, one change set
// for mapping (old path -> new path): moved pages get their own links re-based
// at the new location, and pages that refer to a moved page get those links
// rewritten. Every change carries the blob sha read here as its Base, and
// every new path requires that the path does not exist, so that a page
// changed or created since it was read makes the commit a conflict. A page
// that cannot be read is an error, so that no link to a moved page is left
// unchanged. Pages that are not blobs, such as submodules, have no links and
// are not read.
func Relocate(s Store, entries []repo.Entry, mapping map[string]string) ([]repo.Change, error) {
	var all []string
	for _, e := range entries {
		if e.Type == "blob" && page.IsPagePath(e.Path) {
			all = append(all, e.Path)
		}
	}
	contents, shas, err := s.CatSHA(all)
	if err != nil {
		return nil, err
	}
	var absent []string
	for _, p := range all {
		if _, ok := contents[p]; !ok {
			absent = append(absent, p)
		}
	}
	if err := s.CheckMissing(absent); err != nil {
		return nil, err
	}
	none := ""
	var changes []repo.Change
	for _, p := range all {
		content, base := contents[p], shas[p]
		if np, moved := mapping[p]; moved {
			nc, _ := page.Relocate(content, p, np, mapping)
			changes = append(changes, repo.Change{Path: np, Content: nc, Base: &none}, repo.Change{Path: p, Delete: true, Base: &base})
			continue
		}
		if nc, n := page.Relocate(content, p, p, mapping); n > 0 {
			changes = append(changes, repo.Change{Path: p, Content: nc, Base: &base})
		}
	}
	return changes, nil
}
