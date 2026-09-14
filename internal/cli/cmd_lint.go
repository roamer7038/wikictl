package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/wiki"
)

func (a *app) cmdLint(c *command, args []string) error {
	paths := args
	if len(paths) == 0 {
		var err error
		if paths, err = a.repo.List(nil); err != nil {
			return &gitError{err}
		}
	}
	all, err := a.repo.List(nil)
	if err != nil {
		return &gitError{err}
	}
	checked := map[string]bool{}
	for _, p := range paths {
		checked[p] = true
	}
	read, err := a.readPages(paths)
	if err != nil {
		return &gitError{err}
	}
	for _, p := range args {
		if !read.exists(p) {
			return a.notFound(p)
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
		pg := read.parse(p)
		items = append(items, pg.Issues...)
		pages = append(pages, pg)
	}
	broken, err := wiki.BrokenLinks(a.repo, pages)
	if err != nil {
		return &gitError{err}
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
		return exitStatus(ExitInvalid)
	}
	return nil
}
