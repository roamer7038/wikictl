package cli

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/wiki"
)

// lintItem is one finding of lint in JSON output. It wraps page.Issue so that
// a path that is not valid UTF-8 is printed in base64, which is a concern of
// the output rather than of the check.
type lintItem struct{ page.Issue }

// MarshalJSON writes every key of the finding; a path that is not valid
// UTF-8 goes to path_base64; see jsonout.go. The message is prose rather than
// a value to pass back to another command, so it keeps its key; see the help
// of lint.
func (it lintItem) MarshalJSON() ([]byte, error) {
	return jsonObject(nil).text("path", it.Path).add("line", it.Line).
		add("code", it.Code).add("message", it.Message).MarshalJSON()
}

func (a *app) cmdLint(c *command, args []string) error {
	t, err := a.readTree(false)
	if err != nil {
		return err
	}
	all := slices.DeleteFunc(slices.Clone(t.files), func(p string) bool { return !page.IsPagePath(p) })
	// Names are the paths collisions are found against: the pages, and the
	// submodules named like one, whose name breaks a clone on a
	// case-insensitive file system as any other colliding name does.
	names := slices.Clone(all)
	for p := range t.subs {
		if page.IsPagePath(p) {
			names = append(names, p)
		}
	}
	slices.Sort(names)
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
	read, err := wiki.ReadPages(a.repo, notEmpty(paths))
	if err != nil {
		return &gitError{err}
	}
	// An argument that is a file but not a page is reported, not checked: the
	// rules lint applies are the rules of a page. page.IsPagePath decides what
	// a page is here as it does above, where it selects the pages to check
	// when no path is given.
	missing := slices.DeleteFunc(slices.Clone(files), func(p string) bool { return read.Exists(p) && page.IsPagePath(p) })
	paths = slices.DeleteFunc(paths, func(p string) bool { return slices.Contains(missing, p) })
	checked := map[string]bool{}
	for _, p := range paths {
		checked[p] = true
	}
	items := []page.Issue{}
	// Collisions are found against the whole tree, since the colliding name
	// may lie outside the checked directories. Only the checked paths are
	// reported, so a submodule contributes its name but gets no line of its
	// own; the message of the page it collides with names it.
	for _, is := range slices.Concat(page.CaseCollisions(names), page.UnicodeCollisions(names)) {
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
	// lint.ignore removes items before they are counted, sorted or printed, so
	// that an ignored rule affects neither the exit code nor --json.
	items = slices.DeleteFunc(items, func(is page.Issue) bool { return a.cfg.LintIgnore[is.Code] })
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].Line < items[j].Line
	})
	out := make([]lintItem, len(items))
	for i, is := range items {
		out[i] = lintItem{is}
	}
	a.emit(map[string]any{"items": out}, func(w io.Writer) {
		for _, it := range items {
			fmt.Fprintf(w, "%s:%d: %s: %s\n", escapeControl(it.Path), it.Line, it.Code, escapeControl(it.Message))
		}
	})
	if len(missing) > 0 {
		msg, err := a.pageMessage(missing)
		if err != nil {
			return err
		}
		return a.reportMissing(missing, msg)
	}
	if len(items) > 0 {
		return exitStatus(ExitInvalid)
	}
	return nil
}
