package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
	"github.com/roamer7038/wikictl/internal/wiki"
)

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

func hasTag(fm map[string]any, tag string) bool {
	ts, _ := fm["tags"].([]any)
	for _, t := range ts {
		if s, _ := t.(string); s == tag {
			return true
		}
	}
	return false
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
