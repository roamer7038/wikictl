package cli

import (
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
	"github.com/roamer7038/wikictl/internal/wiki"
)

type hit struct {
	Path    string   `json:"path"`
	Summary string   `json:"summary"`
	Title   string   `json:"title"`
	Matched []string `json:"matched"`
	Updated string   `json:"updated"`
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

func (v *positiveInt) Type() string { return "int" }

func searchFlags(a *app, fs *pflag.FlagSet) {
	a.n = 20
	fs.BoolVar(&a.any, "any", false, "match pages containing any of the words instead of all of them")
	fs.VarP(&a.n, "number", "n", "show at most `N` results; N must be at least 1")
	fs.BoolVar(&a.all, "all", false, "include pages with status: deprecated")
}

func (a *app) cmdSearch(c *command, words []string) error {
	paths, err := a.repo.Grep(words, !a.any, nil)
	if err != nil {
		return &gitError{err}
	}
	if !a.all {
		dep, err := a.repo.GrepDeprecated(nil)
		if err != nil {
			return &gitError{err}
		}
		paths = filterOut(paths, dep)
	}
	updated, err := a.repo.Updated(nil)
	if err != nil {
		return &gitError{err}
	}
	pages, err := a.readPages(paths)
	if err != nil {
		return &gitError{err}
	}
	hits := []hit{}
	for _, p := range paths {
		pg := pages.parse(p)
		found, err := pages.containsFolded(a.repo, p, words)
		if err != nil {
			return &gitError{err}
		}
		matched := []string{}
		for i, w := range words {
			if found[i] {
				matched = append(matched, w)
			}
		}
		hits = append(hits, hit{Path: p, Summary: pg.Summary, Title: pg.Title, Matched: matched, Updated: fmtTime(updated[p])})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if a.any && len(hits[i].Matched) != len(hits[j].Matched) {
			return len(hits[i].Matched) > len(hits[j].Matched)
		}
		if hits[i].Updated != hits[j].Updated {
			return hits[i].Updated > hits[j].Updated
		}
		return hits[i].Path < hits[j].Path
	})
	if n := int(a.n); len(hits) > n {
		hits = hits[:n]
	}
	a.emit(map[string]any{"items": hits}, func(w io.Writer) {
		for _, h := range hits {
			fmt.Fprintf(w, "%s\t%s\n", h.Path, escapeControl(summaryOrTitle(h.Summary, h.Title)))
		}
	})
	return nil
}

type catItem struct {
	Path    string `json:"path"`
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

// cmdCat prints the files as stored, in the order given. As GNU cat does, a
// path that is not a file is reported on standard error, the other files are
// still printed, and the command exits with 1.
func (a *app) cmdCat(c *command, args []string) error {
	contents, shas, err := a.repo.CatSHA(args)
	if err != nil {
		return &gitError{err}
	}
	missing, err := a.missing(args, func(p string) bool { _, ok := contents[p]; return ok })
	if err != nil {
		return err
	}
	items := []catItem{}
	for _, p := range args {
		if b, ok := contents[p]; ok {
			items = append(items, catItem{p, shas[p], string(b)})
		}
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			io.WriteString(w, it.Content)
		}
	})
	return a.reportMissing(missing)
}

type statItem struct {
	Path    string   `json:"path"`
	SHA     string   `json:"sha"`
	Updated string   `json:"updated"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Type    string   `json:"type"`
	Tags    []string `json:"tags"`
	Status  string   `json:"status"`
	Aliases []string `json:"aliases"`
}

// cmdStat shows the blob sha, the time of the last change and the attributes
// of each file. Paths that are not files are reported as cat reports them.
func (a *app) cmdStat(c *command, args []string) error {
	objs, err := a.repo.Stat(args)
	if err != nil {
		return &gitError{err}
	}
	missing, err := a.missing(args, func(p string) bool { _, ok := objs[p]; return ok })
	if err != nil {
		return err
	}
	var found []string
	for _, p := range args {
		if _, ok := objs[p]; ok {
			found = append(found, p)
		}
	}
	pages, err := a.readPages(found)
	if err != nil {
		return &gitError{err}
	}
	updated := map[string]time.Time{}
	if len(found) > 0 {
		if updated, err = a.repo.Updated(found); err != nil {
			return &gitError{err}
		}
	}
	items := []statItem{}
	for _, p := range found {
		pg := pages.parse(p)
		typ, _ := pg.Frontmatter["type"].(string)
		status, _ := pg.Frontmatter["status"].(string)
		items = append(items, statItem{Path: p, SHA: objs[p].SHA, Updated: fmtTime(updated[p]), Title: pg.Title, Summary: pg.Summary,
			Type: typ, Tags: stringList(pg.Frontmatter["tags"]), Status: status, Aliases: stringList(pg.Frontmatter["aliases"])})
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for i, it := range items {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "path: %s\nsha: %s\nupdated: %s\ntitle: %s\nsummary: %s\ntype: %s\ntags: %s\nstatus: %s\naliases: %s\n",
				escapeControl(it.Path), it.SHA, it.Updated, escapeControl(it.Title), escapeControl(it.Summary), escapeControl(it.Type),
				escapeControl(strings.Join(it.Tags, ", ")), escapeControl(it.Status), escapeControl(strings.Join(it.Aliases, ", ")))
		}
	})
	return a.reportMissing(missing)
}

// stringList returns the strings of a frontmatter value written as a YAML
// list, or the value itself when it is a single string.
func stringList(v any) []string {
	out := []string{}
	switch v := v.(type) {
	case string:
		out = append(out, v)
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// missing returns the paths for which found is false, after checking with
// git that each of them is absent rather than unreadable.
func (a *app) missing(paths []string, found func(string) bool) ([]string, error) {
	var out []string
	for _, p := range paths {
		if !found(p) {
			out = append(out, p)
		}
	}
	if err := a.repo.CheckMissing(out); err != nil {
		return nil, &gitError{err}
	}
	return out, nil
}

// reportMissing prints a line on standard error for each path that is not a
// file, and returns exit status 1 when there is any.
func (a *app) reportMissing(paths []string) error {
	for _, p := range paths {
		fmt.Fprintf(a.stderr, "wikictl: %s: no such file\n", escapeControl(p))
	}
	if len(paths) > 0 {
		return exitStatus(ExitError)
	}
	return nil
}

type linkItem struct {
	Direction string `json:"direction"`
	Type      string `json:"type"`
	Target    string `json:"target"`
	Note      string `json:"note"`
}

func linksFlags(a *app, fs *pflag.FlagSet) {
	fs.BoolVarP(&a.linksIn, "in", "i", false, "list only the links from other pages to this page")
	fs.BoolVarP(&a.linksOut, "out", "o", false, "list only the links in this page")
}

// cmdLinks lists the links in a page ("out") and the links to it from other
// pages ("in"). A body link to a page that the Links section also links to is
// not listed again.
func (a *app) cmdLinks(c *command, args []string) error {
	p := args[0]
	pages, err := a.readPages(args)
	if err != nil {
		return &gitError{err}
	}
	if !pages.exists(p) {
		return a.notFound(p)
	}
	both := a.linksIn == a.linksOut
	items := []linkItem{}
	if both || a.linksOut {
		pg := pages.parse(p)
		typed := map[string]bool{}
		for _, l := range pg.Links {
			items = append(items, linkItem{"out", l.Type, l.Target, l.Note})
			typed[l.Target] = true
		}
		for _, m := range pg.Mentions {
			if !typed[m.Target] {
				items = append(items, linkItem{"out", m.Type, m.Target, ""})
			}
		}
	}
	if both || a.linksIn {
		backlinks, err := wiki.Backlinks(a.repo, p)
		if err != nil {
			return &gitError{err}
		}
		for _, b := range backlinks {
			items = append(items, linkItem{"in", b.Type, b.Path, ""})
		}
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\n", it.Direction, it.Type, escapeControl(it.Target))
		}
	})
	return nil
}

// pageSet is the files read by readPages.
type pageSet struct {
	contents map[string][]byte
	large    map[string]repo.Object // files over page.MaxPageSize, not read
}

// readPages reads paths with one lookup of their sizes and one read of the
// contents of the files that are not over page.MaxPageSize.
func (a *app) readPages(paths []string) (pageSet, error) {
	contents, large, err := a.repo.CatLimit(paths, page.MaxPageSize)
	return pageSet{contents, large}, err
}

// exists reports whether p is a file.
func (s pageSet) exists(p string) bool {
	_, ok := s.contents[p]
	_, big := s.large[p]
	return ok || big
}

// parse returns page.Parse of p, and page.TooLarge for a file over the limit.
func (s pageSet) parse(p string) *page.Page {
	if _, big := s.large[p]; big {
		return page.TooLarge(p)
	}
	return page.Parse(p, s.contents[p])
}

// containsFolded reports, for each word, whether the content of p contains
// it ignoring case as repo.Fold defines. The content of a file over the limit
// is read as a stream with repo.ContainsFolded.
func (s pageSet) containsFolded(r *repo.Repo, p string, words []string) ([]bool, error) {
	o, big := s.large[p]
	if !big {
		folded := repo.Fold(string(s.contents[p]))
		found := make([]bool, len(words))
		for i, w := range words {
			found[i] = strings.Contains(folded, repo.Fold(w))
		}
		return found, nil
	}
	var found []bool
	err := r.ReadBlob(o.SHA, func(rd io.Reader) error {
		var err error
		found, err = repo.ContainsFolded(rd, words)
		return err
	})
	return found, err
}

type lsItem struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Updated string `json:"updated"`
}

func lsFlags(a *app, fs *pflag.FlagSet) {
	fs.StringVar(&a.typ, "type", "", "only pages with this `type` in the frontmatter")
	fs.StringVar(&a.tag, "tag", "", "only pages tagged `tag`")
	fs.BoolVar(&a.all, "all", false, "include pages with status: deprecated")
}

func (a *app) cmdLs(c *command, args []string) error {
	paths, err := a.repo.List(nil)
	if err != nil {
		return &gitError{err}
	}
	if !a.all {
		dep, err := a.repo.GrepDeprecated(nil)
		if err != nil {
			return &gitError{err}
		}
		paths = filterOut(paths, dep)
	}
	pages, err := a.readPages(paths)
	if err != nil {
		return &gitError{err}
	}
	updated, err := a.repo.Updated(nil)
	if err != nil {
		return &gitError{err}
	}
	items := []lsItem{}
	for _, p := range paths {
		pg := pages.parse(p)
		t, _ := pg.Frontmatter["type"].(string)
		if a.typ != "" && t != a.typ {
			continue
		}
		if a.tag != "" && !hasTag(pg.Frontmatter, a.tag) {
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

// cmdContext prints the configuration the other commands use, with the origin
// remote that profile selection compared against match.remotes.
func (a *app) cmdContext(c *command, args []string) error {
	au, _ := a.author()
	out := map[string]any{"config": a.cfg.Path, "profile": a.cfg.Profile, "profile_source": a.cfg.ProfileSource,
		"repo": repo.RedactURL(a.cfg.Repo), "mirror": a.repo.Dir, "branch": a.repo.Branch, "author": au.Name,
		"remote": repo.RedactURL(a.remote)}
	a.emit(out, func(w io.Writer) {
		for _, k := range []string{"config", "profile", "profile_source", "repo", "mirror", "branch", "author", "remote"} {
			fmt.Fprintf(w, "%s: %v\n", k, out[k])
		}
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
		if strings.HasSuffix(d, ".md") {
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
	pages, err := a.readPages(indexes)
	if err != nil {
		return &gitError{err}
	}
	items := []dirItem{}
	for _, d := range dirs {
		it := dirItem{Dir: d, Pages: counts[d]}
		if pages.exists(d + "index.md") {
			it.Summary = pages.parse(d + "index.md").Summary
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
			if !pages.exists(it.Dir + "index.md") {
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
