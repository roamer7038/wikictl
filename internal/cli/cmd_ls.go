package cli

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/wiki"
)

// fileTree is the files of the wiki arranged by directory.
type fileTree struct {
	children map[string]map[string]bool // directory ("." for the root) -> entry name -> whether the entry is a directory
	hidden   map[string]bool            // pages with status: deprecated
}

// join returns the path of the entry name in dir.
func join(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

// readTree lists every file of the wiki and, unless -a is given, finds the
// pages with status: deprecated, which ls and tree hide.
func (a *app) readTree() (*fileTree, error) {
	files, err := a.repo.Files(nil)
	if err != nil {
		return nil, &gitError{err}
	}
	t := &fileTree{children: map[string]map[string]bool{".": {}}, hidden: map[string]bool{}}
	for _, f := range files {
		dir := "."
		parts := strings.Split(f, "/")
		for i, name := range parts {
			if t.children[dir] == nil {
				t.children[dir] = map[string]bool{}
			}
			t.children[dir][name] = i < len(parts)-1
			dir = join(dir, name)
		}
	}
	if !a.all {
		if t.hidden, err = wiki.Deprecated(a.repo); err != nil {
			return nil, &gitError{err}
		}
	}
	return t, nil
}

func (t *fileTree) isDir(p string) bool { return t.children[p] != nil }

// isFile reports whether p is a file in the wiki.
func (t *fileTree) isFile(p string) bool {
	isDir, ok := t.children[path.Dir(p)][path.Base(p)]
	return ok && !isDir
}

// entries returns the paths of the entries of dir sorted by name. Unless all
// is set, names starting with a dot and hidden pages are left out.
func (t *fileTree) entries(dir string, all bool) []string {
	var out []string
	for name := range t.children[dir] {
		p := join(dir, name)
		if all || (!strings.HasPrefix(name, ".") && !t.hidden[p]) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

type lsItem struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Title   string `json:"title"`
	Updated string `json:"updated"`
}

func lsFlags(a *app, fs *pflag.FlagSet) {
	fs.BoolVarP(&a.long, "long", "l", false, "show the type, last update and summary of each entry")
	fs.BoolVarP(&a.recursive, "recursive", "R", false, "list subdirectories recursively")
	fs.BoolVarP(&a.all, "all", "a", false, "include names starting with a dot and pages with status: deprecated")
	fs.BoolVarP(&a.byTime, "time", "t", false, "sort by last update, newest first")
	fs.StringVar(&a.typ, "type", "", "list only the pages with this `type` in the frontmatter")
	fs.StringVar(&a.tag, "tag", "", "list only the pages tagged `tag`")
}

// lsSection is the entries listed under the heading of dir; dir is "" for
// the files given as arguments.
type lsSection struct {
	dir   string
	items []lsItem
}

func (a *app) cmdLs(c *command, args []string) error {
	t, err := a.readTree()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		args = []string{"."}
	}
	var files, dirs, missing []string
	for _, p := range args {
		switch {
		case t.isDir(p):
			dirs = append(dirs, p)
		case t.isFile(p):
			files = append(files, p)
		default:
			missing = append(missing, p)
		}
	}
	sort.Strings(files)
	sort.Strings(dirs)
	var sections []lsSection
	if len(files) > 0 {
		s := lsSection{}
		for _, p := range files {
			s.items = append(s.items, lsItem{Path: p, Kind: "file"})
		}
		sections = append(sections, s)
	}
	var walk func(dir string)
	walk = func(dir string) {
		s := lsSection{dir: dir}
		var sub []string
		for _, p := range t.entries(dir, a.all) {
			if t.isDir(p) {
				s.items = append(s.items, lsItem{Path: p, Kind: "dir"})
				sub = append(sub, p)
			} else {
				s.items = append(s.items, lsItem{Path: p, Kind: "file"})
			}
		}
		sections = append(sections, s)
		if a.recursive {
			for _, p := range sub {
				walk(p)
			}
		}
	}
	for _, d := range dirs {
		walk(d)
	}
	if err := a.lsDetails(sections); err != nil {
		return err
	}
	items := []lsItem{}
	for _, s := range sections {
		items = append(items, s.items...)
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		headings := len(args) > 1 || a.recursive
		for i, s := range sections {
			if i > 0 {
				fmt.Fprintln(w)
			}
			if headings && s.dir != "" {
				fmt.Fprintf(w, "%s:\n", escapeControl(s.dir))
			}
			var rows [][4]string
			for _, it := range s.items {
				name := it.Path
				if s.dir != "" {
					name = path.Base(it.Path)
				}
				if it.Kind == "dir" {
					name += "/"
				}
				rows = append(rows, [4]string{orDash(escapeControl(it.Type)), orDash(it.Updated), escapeControl(name), escapeControl(summaryOrTitle(it.Summary, it.Title))})
			}
			writeLs(w, rows, a.long)
		}
	})
	return a.reportMissing(missing)
}

// lsDetails fills in the attributes of the items that the output needs,
// removes the pages that --type or --tag leave out, and sorts by time for -t.
func (a *app) lsDetails(sections []lsSection) error {
	var paths []string
	for _, s := range sections {
		for _, it := range s.items {
			if it.Kind == "file" {
				paths = append(paths, it.Path)
			}
		}
	}
	if a.long || a.json || a.typ != "" || a.tag != "" {
		pages, err := a.readPages(paths)
		if err != nil {
			return &gitError{err}
		}
		for i := range sections {
			kept := sections[i].items[:0]
			for _, it := range sections[i].items {
				if it.Kind == "file" {
					pg := pages.parse(it.Path)
					it.Type, _ = pg.Frontmatter["type"].(string)
					it.Summary, it.Title = pg.Summary, pg.Title
					if (a.typ != "" && it.Type != a.typ) || (a.tag != "" && !hasTag(pg.Frontmatter, a.tag)) {
						continue
					}
				}
				kept = append(kept, it)
			}
			sections[i].items = kept
		}
	}
	if a.long || a.json || a.byTime {
		updated, err := a.repo.Updated(nil)
		if err != nil {
			return &gitError{err}
		}
		// A directory was last updated when any file under it was.
		latest := map[string]string{}
		for p, tm := range updated {
			s := fmtTime(tm)
			for d := p; d != "."; d = path.Dir(d) {
				latest[d] = max(latest[d], s)
			}
		}
		for _, s := range sections {
			for i := range s.items {
				s.items[i].Updated = latest[s.items[i].Path]
			}
			if a.byTime {
				sort.SliceStable(s.items, func(i, j int) bool { return s.items[i].Updated > s.items[j].Updated })
			}
		}
	}
	return nil
}

// writeLs writes the names of rows, or with long every column, aligned.
func writeLs(w io.Writer, rows [][4]string, long bool) {
	if !long {
		for _, r := range rows {
			fmt.Fprintln(w, r[2])
		}
		return
	}
	var width [3]int
	for _, r := range rows {
		for i := range width {
			width[i] = max(width[i], len([]rune(r[i])))
		}
	}
	for _, r := range rows {
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %s", width[0], r[0], width[1], r[1], width[2], r[2], r[3])
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

type treeItem struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

func treeFlags(a *app, fs *pflag.FlagSet) {
	fs.BoolVarP(&a.all, "all", "a", false, "include names starting with a dot and pages with status: deprecated")
	fs.BoolVarP(&a.dirsOnly, "dirs", "d", false, "list directories only")
	fs.VarP(&a.level, "level", "L", "descend at most `N` levels of directories")
}

func (a *app) cmdTree(c *command, args []string) error {
	t, err := a.readTree()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		args = []string{"."}
	}
	items := []treeItem{}
	var dirs, files int
	var b strings.Builder
	var bad []string
	for _, root := range args {
		if !t.isDir(root) {
			bad = append(bad, root)
			continue
		}
		fmt.Fprintln(&b, escapeControl(root))
		var walk func(dir, prefix string, depth int)
		walk = func(dir, prefix string, depth int) {
			var es []string
			for _, p := range t.entries(dir, a.all) {
				if !a.dirsOnly || t.isDir(p) {
					es = append(es, p)
				}
			}
			for i, p := range es {
				branch, indent := "├── ", "│   "
				if i == len(es)-1 {
					branch, indent = "└── ", "    "
				}
				fmt.Fprintf(&b, "%s%s%s\n", prefix, branch, escapeControl(path.Base(p)))
				if !t.isDir(p) {
					files++
					items = append(items, treeItem{p, "file"})
					continue
				}
				dirs++
				items = append(items, treeItem{p, "dir"})
				if a.level == 0 || depth < int(a.level) {
					walk(p, prefix+indent, depth+1)
				}
			}
		}
		walk(root, "", 1)
	}
	fmt.Fprintf(&b, "\n%s", count(dirs, "directory", "directories"))
	if !a.dirsOnly {
		fmt.Fprintf(&b, ", %s", count(files, "file", "files"))
	}
	b.WriteString("\n")
	a.emit(map[string]any{"items": items, "directories": dirs, "files": files}, func(w io.Writer) { io.WriteString(w, b.String()) })
	for _, p := range bad {
		fmt.Fprintf(a.stderr, "wikictl: %s: not a directory\n", escapeControl(p))
	}
	if len(bad) > 0 {
		return exitStatus(ExitError)
	}
	return nil
}

// count returns n followed by one or many.
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
