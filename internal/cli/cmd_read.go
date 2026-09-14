package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	ctx "github.com/roamer7038/wikictl/internal/context"
	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
)

type hit struct {
	Path    string   `json:"path"`
	Summary string   `json:"summary"`
	Title   string   `json:"title"`
	Matched []string `json:"matched"`
	Updated string   `json:"updated"`
}

type searchOpts struct {
	any bool
	n   positiveInt
	all bool
}

// positiveInt is an int flag value that rejects values below 1, so that the
// check runs wherever the flags are parsed.
type positiveInt int

func (v *positiveInt) String() string { return strconv.Itoa(int(*v)) }

func (v *positiveInt) Set(s string) error {
	n, err := strconv.ParseInt(s, 0, strconv.IntSize)
	if err != nil {
		return errors.New("parse error")
	}
	if n < 1 {
		return errors.New("must be at least 1")
	}
	*v = positiveInt(n)
	return nil
}

func searchFlags(fs *flag.FlagSet) *searchOpts {
	o := &searchOpts{n: 20}
	fs.BoolVar(&o.any, "any", false, "match pages containing any of the words instead of all of them")
	fs.Var(&o.n, "n", "show at most `N` results; N must be at least 1")
	fs.BoolVar(&o.all, "all", false, "include pages with status: deprecated")
	return o
}

func (a *app) cmdSearch(c *command, args []string) error {
	fs := newFlagSet(c.name)
	o := searchFlags(fs)
	words, err := parseFlags(c, fs, args)
	if err != nil {
		return err
	}
	paths, err := a.repo.Grep(words, !o.any, a.dirs)
	if err != nil {
		return &gitError{err}
	}
	if !o.all {
		dep, err := a.repo.GrepDeprecated(a.dirs)
		if err != nil {
			return &gitError{err}
		}
		paths = filterOut(paths, dep)
	}
	updated, err := a.repo.Updated(a.dirs)
	if err != nil {
		return &gitError{err}
	}
	contents, err := a.repo.Cat(paths)
	if err != nil {
		return &gitError{err}
	}
	hits := []hit{}
	for _, p := range paths {
		pg := page.Parse(p, contents[p])
		matched := []string{}
		folded := repo.Fold(string(contents[p]))
		for _, w := range words {
			if strings.Contains(folded, repo.Fold(w)) {
				matched = append(matched, w)
			}
		}
		hits = append(hits, hit{Path: p, Summary: pg.Summary, Title: pg.Title, Matched: matched, Updated: fmtTime(updated[p])})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if o.any && len(hits[i].Matched) != len(hits[j].Matched) {
			return len(hits[i].Matched) > len(hits[j].Matched)
		}
		if hits[i].Updated != hits[j].Updated {
			return hits[i].Updated > hits[j].Updated
		}
		return hits[i].Path < hits[j].Path
	})
	if n := int(o.n); len(hits) > n {
		hits = hits[:n]
	}
	a.emit(map[string]any{"items": hits}, func(w io.Writer) {
		for _, h := range hits {
			fmt.Fprintf(w, "%s\t%s\n", h.Path, escapeControl(summaryOrTitle(h.Summary, h.Title)))
		}
	})
	return nil
}

type getOut struct {
	Path        string         `json:"path"`
	SHA         string         `json:"sha"`
	Frontmatter map[string]any `json:"frontmatter"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	Links       []linkOut      `json:"links"`
	Backlinks   []backlinkOut  `json:"backlinks"`
	Updated     string         `json:"updated"`
}

type linkOut struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Note   string `json:"note"`
}

type backlinkOut struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (a *app) cmdGet(c *command, args []string) error {
	p := args[0]
	contents, err := a.repo.Cat([]string{p})
	if err != nil {
		return &gitError{err}
	}
	content, ok := contents[p]
	if !ok {
		return a.notFound(p)
	}
	head, err := a.repo.Head()
	if err != nil {
		return &gitError{err}
	}
	sha, err := a.repo.BlobSHA(head, p)
	if err != nil {
		return &gitError{err}
	}
	pg := page.Parse(p, content)
	links := []linkOut{}
	for _, l := range pg.Links {
		links = append(links, linkOut{l.Type, l.Target, l.Note})
	}
	backlinks, err := a.backlinks(p)
	if err != nil {
		return &gitError{err}
	}
	updated, err := a.repo.Updated([]string{path.Dir(p)})
	if err != nil {
		return &gitError{err}
	}
	fm := pg.Frontmatter
	if fm == nil {
		fm = map[string]any{}
	}
	out := getOut{Path: p, SHA: sha, Frontmatter: fm, Title: pg.Title, Body: pg.Body, Links: links, Backlinks: backlinks, Updated: fmtTime(updated[p])}
	a.emit(out, func(w io.Writer) {
		fmt.Fprintf(w, "path: %s\nsha: %s\nupdated: %s\n\n%s", p, sha, out.Updated, pg.Body)
		if len(links) > 0 {
			fmt.Fprintln(w, "\nlinks:")
			for _, l := range links {
				fmt.Fprintf(w, "%s\n", strings.TrimRight("  "+l.Type+": "+escapeControl(l.Target)+" "+escapeControl(l.Note), " "))
			}
		}
		if len(backlinks) > 0 {
			fmt.Fprintln(w, "\nbacklinks:")
			for _, b := range backlinks {
				fmt.Fprintf(w, "  %s (%s)\n", b.Path, b.Type)
			}
		}
	})
	return nil
}

// backlinks greps the whole wiki for the file name of target to collect candidate
// pages, then parses each candidate and keeps those whose links resolve to
// target. Typed links win over body mentions.
func (a *app) backlinks(target string) ([]backlinkOut, error) {
	name := strings.TrimSuffix(path.Base(target), ".md")
	cands, err := a.repo.Grep([]string{name + ".md"}, true, nil)
	if err != nil {
		return nil, err
	}
	contents, err := a.repo.Cat(cands)
	if err != nil {
		return nil, err
	}
	out := []backlinkOut{}
	for _, cp := range cands {
		if cp == target {
			continue
		}
		pg := page.Parse(cp, contents[cp])
		typed := false
		for _, l := range pg.Links {
			if !l.IsURL && l.Target == target {
				out = append(out, backlinkOut{cp, l.Type})
				typed = true
			}
		}
		if !typed {
			for _, m := range pg.Mentions {
				if m.Target == target {
					out = append(out, backlinkOut{cp, "mentions"})
					break
				}
			}
		}
	}
	return out, nil
}

type lsItem struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Updated string `json:"updated"`
}

type lsOpts struct {
	typ, tag string
	all      bool
}

func lsFlags(fs *flag.FlagSet) *lsOpts {
	o := &lsOpts{}
	fs.StringVar(&o.typ, "type", "", "only pages with this `type` in the frontmatter")
	fs.StringVar(&o.tag, "tag", "", "only pages tagged `tag`")
	fs.BoolVar(&o.all, "all", false, "include pages with status: deprecated")
	return o
}

func (a *app) cmdLs(c *command, args []string) error {
	fs := newFlagSet(c.name)
	o := lsFlags(fs)
	if _, err := parseFlags(c, fs, args); err != nil {
		return err
	}
	paths, err := a.repo.List(a.dirs)
	if err != nil {
		return &gitError{err}
	}
	if !o.all {
		dep, err := a.repo.GrepDeprecated(a.dirs)
		if err != nil {
			return &gitError{err}
		}
		paths = filterOut(paths, dep)
	}
	contents, err := a.repo.Cat(paths)
	if err != nil {
		return &gitError{err}
	}
	updated, err := a.repo.Updated(a.dirs)
	if err != nil {
		return &gitError{err}
	}
	items := []lsItem{}
	for _, p := range paths {
		pg := page.Parse(p, contents[p])
		t, _ := pg.Frontmatter["type"].(string)
		if o.typ != "" && t != o.typ {
			continue
		}
		if o.tag != "" && !hasTag(pg.Frontmatter, o.tag) {
			continue
		}
		items = append(items, lsItem{p, pg.Summary, pg.Title, t, fmtTime(updated[p])})
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			fmt.Fprintf(w, "%s\t%s\n", it.Path, escapeControl(summaryOrTitle(it.Summary, it.Title)))
		}
	})
	return nil
}

// cmdContext prints the values other commands derive from the environment:
// machine and project are the resolved names used for machines/<name>/ and
// projects/<name>/, not the raw hostname and remote URL.
func (a *app) cmdContext(c *command, args []string) error {
	host, _ := osHostname()
	machine := a.cfg.Machine
	if machine == "" {
		machine = ctx.MachineName(host)
	}
	remote := a.remote
	project := ""
	if remote != "" {
		project = ctx.ProjectName(remote)
		if m, ok := a.cfg.Projects[project]; ok && m != "" {
			project = m
		}
	}
	au, _ := a.author()
	paths, err := a.repo.List(a.dirs)
	if err != nil {
		return &gitError{err}
	}
	pages := map[string]int{}
	for _, d := range a.dirs {
		pages[d] = countPagesUnder(paths, d)
	}
	out := map[string]any{"config": a.cfg.Path, "profile": a.cfg.Profile, "profile_source": a.cfg.ProfileSource,
		"repo": repo.RedactURL(a.cfg.Repo), "mirror": a.repo.Dir, "branch": a.repo.Branch, "author": au.Name,
		"machine": machine, "project": project, "remote": repo.RedactURL(remote), "dirs": a.dirs, "pages": pages}
	a.emit(out, func(w io.Writer) {
		for _, k := range []string{"config", "profile", "profile_source", "repo", "mirror", "branch", "author", "machine", "project", "remote"} {
			fmt.Fprintf(w, "%s: %v\n", k, out[k])
		}
		ds := make([]string, len(a.dirs))
		for i, d := range a.dirs {
			ds[i] = fmt.Sprintf("%s (%s)", d, plural(pages[d], "page"))
		}
		fmt.Fprintf(w, "dirs: %s\n", strings.Join(ds, ", "))
	})
	return nil
}

// summaryOrTitle returns the summary, or the title when the page has none,
// so that text output always shows something for a page.
func summaryOrTitle(summary, title string) string {
	if summary != "" {
		return summary
	}
	return title
}

type dirItem struct {
	Dir     string `json:"dir"`
	Pages   int    `json:"pages"`
	Summary string `json:"summary"`
}

// cmdDirs groups one listing of the tree by directory and reads only the
// index.md files, so the cost does not grow with the number of pages read.
func (a *app) cmdDirs(c *command, args []string) error {
	for _, d := range args {
		p := path.Clean(d)
		if path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") {
			return &usageError{c, "directory outside the wiki: " + d}
		}
		if strings.HasSuffix(p, ".md") {
			return &usageError{c, "not a directory: " + d}
		}
	}
	paths, err := a.repo.List(args)
	if err != nil {
		return &gitError{err}
	}
	counts := countPages(paths)
	dirs := make([]string, 0, len(counts))
	indexes := make([]string, 0, len(counts))
	for d := range counts {
		dirs = append(dirs, d)
		indexes = append(indexes, d+"index.md")
	}
	sort.Strings(dirs)
	contents, err := a.repo.Cat(indexes)
	if err != nil {
		return &gitError{err}
	}
	items := []dirItem{}
	for _, d := range dirs {
		it := dirItem{Dir: d, Pages: counts[d]}
		if content, ok := contents[d+"index.md"]; ok {
			it.Summary = page.Parse(d+"index.md", content).Summary
		}
		items = append(items, it)
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		dw, nw := 0, 0
		for _, it := range items {
			dw = max(dw, len(it.Dir))
			nw = max(nw, len(strconv.Itoa(it.Pages)))
		}
		for _, it := range items {
			s := escapeControl(it.Summary)
			if _, ok := contents[it.Dir+"index.md"]; !ok {
				s = "(no index)"
			}
			fmt.Fprintf(w, "%-*s  %*d  %s\n", dw, it.Dir, nw, it.Pages, s)
		}
	})
	return nil
}

// countPages returns the number of pages directly in each directory,
// keyed by the directory path with a trailing slash.
func countPages(paths []string) map[string]int {
	counts := map[string]int{}
	for _, p := range paths {
		counts[path.Dir(p)+"/"]++
	}
	return counts
}

// countPagesUnder returns the number of paths at any depth below dir;
// "." counts every path.
func countPagesUnder(paths []string, dir string) int {
	d := path.Clean(dir)
	if d == "." {
		return len(paths)
	}
	n := 0
	for _, p := range paths {
		if strings.HasPrefix(p, d+"/") {
			n++
		}
	}
	return n
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func hasTag(fm map[string]any, tag string) bool {
	ts, _ := fm["tags"].([]any)
	for _, t := range ts {
		if s, _ := t.(string); s == tag {
			return true
		}
	}
	return false
}

func filterOut(paths []string, drop map[string]bool) []string {
	var out []string
	for _, p := range paths {
		if !drop[p] {
			out = append(out, p)
		}
	}
	return out
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
