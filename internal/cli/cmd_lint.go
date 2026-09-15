package cli

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
	"github.com/roamer7038/wikictl/internal/wiki"
)

func (a *app) cmdLint(c *command, args []string) error {
	t, err := a.readTree(false)
	if err != nil {
		return err
	}
	all := slices.DeleteFunc(slices.Clone(t.files), func(p string) bool { return !repo.IsPagePath(p) })
	paths := all
	var files []string // the arguments that are not directories
	if len(args) > 0 {
		var dirs []string
		for _, p := range args {
			if t.isDir(p) {
				dirs = append(dirs, p)
			} else {
				files = append(files, p)
			}
		}
		paths = slices.Clone(files)
		for _, p := range all {
			if slices.ContainsFunc(dirs, func(d string) bool { return d == "." || strings.HasPrefix(p, d+"/") }) {
				paths = append(paths, p)
			}
		}
		slices.Sort(paths)
		paths = slices.Compact(paths)
	}
	read, err := wiki.ReadPages(a.repo, paths)
	if err != nil {
		return &gitError{err}
	}
	missing := slices.DeleteFunc(slices.Clone(files), read.Exists)
	paths = slices.DeleteFunc(paths, func(p string) bool { return slices.Contains(missing, p) })
	checked := map[string]bool{}
	for _, p := range paths {
		checked[p] = true
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
		pg := read.Parse(p)
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
			fmt.Fprintf(w, "%s:%d: %s: %s\n", escapeControl(it.Path), it.Line, it.Code, escapeControl(it.Message))
		}
	})
	if len(missing) > 0 {
		return a.reportMissing(missing, nil)
	}
	if len(items) > 0 {
		return exitStatus(ExitInvalid)
	}
	return nil
}
