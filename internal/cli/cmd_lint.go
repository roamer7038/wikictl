package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/roamer7038/wikictl/internal/page"
)

func (a *app) cmdLint(c *command, args []string) int {
	paths := args
	if len(paths) == 0 {
		var err error
		if paths, err = a.repo.List(a.dirs); err != nil {
			return a.fail(ExitGit, "git", err.Error())
		}
	}
	all, err := a.repo.List(nil)
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	checked := map[string]bool{}
	for _, p := range paths {
		checked[p] = true
	}
	contents, _ := a.repo.Cat(paths)
	for _, p := range args {
		if contents[p] == nil {
			return a.fail(ExitError, "error", "page not found: "+p)
		}
	}
	items := []page.Issue{}
	// Collisions are found against the whole tree, since the colliding name
	// may lie outside the checked directories.
	for _, is := range page.CaseCollisions(all) {
		if checked[is.Path] {
			items = append(items, is)
		}
	}
	var pages []*page.Page
	for _, p := range paths {
		pg := page.Parse(p, contents[p])
		items = append(items, pg.Issues...)
		pages = append(pages, pg)
	}
	broken, err := a.brokenLinks(pages)
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	items = append(items, broken...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].Line < items[j].Line
	})
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			fmt.Fprintf(w, "%s:%d: %s: %s\n", it.Path, it.Line, it.Code, escapeControl(it.Message))
		}
	})
	if len(items) > 0 {
		return ExitInvalid
	}
	return ExitOK
}

// brokenLinks returns a broken_link issue for every link and mention whose
// target is not a file in the wiki. lint and put share it so that both agree
// on which targets exist.
func (a *app) brokenLinks(pages []*page.Page) ([]page.Issue, error) {
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
	found, err := a.repo.Cat(targets)
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
