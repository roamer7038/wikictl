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
	exists := map[string]bool{}
	for _, p := range all {
		exists[p] = true
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
	for _, p := range paths {
		pg := page.Parse(p, contents[p])
		items = append(items, pg.Issues...)
		for _, l := range append(pg.Links, pg.Mentions...) {
			if !l.IsURL && !exists[l.Target] {
				items = append(items, page.Issue{Path: p, Line: l.Line, Code: "broken_link", Message: "link target does not exist: " + l.Target})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].Line < items[j].Line
	})
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			fmt.Fprintf(w, "%s:%d: %s: %s\n", it.Path, it.Line, it.Code, it.Message)
		}
	})
	if len(items) > 0 {
		return ExitInvalid
	}
	return ExitOK
}
