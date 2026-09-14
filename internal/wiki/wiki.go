// Package wiki implements what the commands do with the pages of a wiki,
// independent of how the files are stored: cleaning the paths given on the
// command line, finding the links between pages, and building the changes
// that moving pages needs.
package wiki

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
)

// Store reads the files of one state of the wiki. *repo.Repo implements it.
type Store interface {
	// List returns the page paths under dirs, or of the whole wiki when dirs
	// is nil.
	List(dirs []string) ([]string, error)
	// Grep returns the pages under dirs that contain the words as fixed
	// strings ignoring case; with all set, every one of them.
	Grep(words []string, all bool, dirs []string) ([]string, error)
	// Stat returns the sha and size of every path that is a file.
	Stat(paths []string) (map[string]repo.Object, error)
	// CatSHA returns the contents and blob shas of the paths that are files.
	CatSHA(paths []string) (map[string][]byte, map[string]string, error)
	// CatLimit returns the contents of the files of at most max bytes, and
	// the sha and size of the larger ones.
	CatLimit(paths []string, max int64) (map[string][]byte, map[string]repo.Object, error)
	// GrepDeprecated returns the pages under dirs that may have status:
	// deprecated in their frontmatter.
	GrepDeprecated(dirs []string) (map[string]bool, error)
}

// Deprecated returns the set of pages whose frontmatter has status:
// deprecated. The frontmatter of each candidate found by GrepDeprecated
// decides, so that such a line in the body does not count and a quoted value
// does.
func Deprecated(s Store) (map[string]bool, error) {
	cands, err := s.GrepDeprecated(nil)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(cands))
	for p := range cands {
		paths = append(paths, p)
	}
	contents, large, err := s.CatLimit(paths, page.MaxPageSize)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, p := range paths {
		if status, _ := parse(p, contents, large).Frontmatter["status"].(string); status == "deprecated" {
			out[p] = true
		}
	}
	return out, nil
}

// ErrOutside is the error of Clean for a path that leaves the wiki.
var ErrOutside = errors.New("path is outside the wiki")

// Clean turns a path given on the command line into a path relative to the
// wiki root. A leading "/" or "./" and a trailing "/" make no difference, and
// the root itself is ".". Wildcards are not interpreted.
func Clean(p string) (string, error) {
	c := path.Clean(strings.TrimLeft(p, "/"))
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%w: %s", ErrOutside, p)
	}
	return c, nil
}

// Backlink is a page that links to another page, with the type of the link:
// the type from its Links section, or "mentions" for a link in its body.
type Backlink struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// Backlinks greps the wiki for the file name of target to find candidate
// pages, then parses each candidate and keeps those whose links resolve to
// target. Typed links win over body mentions.
func Backlinks(s Store, target string) ([]Backlink, error) {
	cands, err := s.Grep([]string{path.Base(target)}, true, nil)
	if err != nil {
		return nil, err
	}
	contents, large, err := s.CatLimit(cands, page.MaxPageSize)
	if err != nil {
		return nil, err
	}
	out := []Backlink{}
	for _, cp := range cands {
		if cp == target {
			continue
		}
		pg := parse(cp, contents, large)
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
// agree on which targets exist.
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

// Relocate walks every page of the wiki and builds one change set for
// mapping (old path -> new path): moved pages get their own links re-based
// at the new location, and pages that refer to a moved page get those links
// rewritten. Every change carries the blob sha read here as its Base, and
// every new path requires that the path does not exist, so that a page
// changed or created since it was read makes the commit a conflict.
func Relocate(s Store, mapping map[string]string) ([]repo.Change, error) {
	all, err := s.List(nil)
	if err != nil {
		return nil, err
	}
	contents, shas, err := s.CatSHA(all)
	if err != nil {
		return nil, err
	}
	none := ""
	mapper := func(target string) (string, bool) { nt, ok := mapping[target]; return nt, ok }
	var changes []repo.Change
	for _, p := range all {
		content, base := contents[p], shas[p]
		if np, moved := mapping[p]; moved {
			nc, _ := page.Relocate(content, p, np, mapper)
			changes = append(changes, repo.Change{Path: np, Content: nc, Base: &none}, repo.Change{Path: p, Delete: true, Base: &base})
			continue
		}
		if nc, n := page.Relocate(content, p, p, mapper); n > 0 {
			changes = append(changes, repo.Change{Path: p, Content: nc, Base: &base})
		}
	}
	return changes, nil
}

// parse returns page.Parse of p, or page.TooLarge when p is over the size
// limit and its content was not read.
func parse(p string, contents map[string][]byte, large map[string]repo.Object) *page.Page {
	if _, big := large[p]; big {
		return page.TooLarge(p)
	}
	return page.Parse(p, contents[p])
}
