package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"
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
	contents, shas, err := a.repo.CatSHA(notEmpty(args))
	if err != nil {
		return &gitError{err}
	}
	missing, err := a.missing(args, func(p string) bool { _, ok := contents[p]; return ok })
	if err != nil {
		return err
	}
	msg, err := a.fileMessage(missing)
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
	return a.reportMissing(missing, msg)
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
// of each file. Paths that are not files are reported as cat reports them. A
// file that is not a page is still shown, since the sha and the update time
// are not an interpretation of a page; only the attributes are, and they stay
// empty for such a file. links and lint, whose whole output is that
// interpretation, refuse it instead.
func (a *app) cmdStat(c *command, args []string) error {
	pages, err := wiki.ReadPages(a.repo, notEmpty(args))
	if err != nil {
		return &gitError{err}
	}
	var found, missing []string
	for _, p := range args {
		if pages.Exists(p) {
			found = append(found, p)
		} else {
			missing = append(missing, p)
		}
	}
	msg, err := a.fileMessage(missing)
	if err != nil {
		return err
	}
	updated, err := a.repo.Updated(found)
	if err != nil {
		return &gitError{err}
	}
	items := []statItem{}
	for _, p := range found {
		it := statItem{Path: p, SHA: pages.Objects[p].SHA, Updated: fmtTime(updated[p]), Tags: []string{}, Aliases: []string{}}
		if page.IsPagePath(p) {
			pg := pages.Parse(p)
			it.Title, it.Summary = pg.Title, pg.Summary
			it.Type, _ = pg.Frontmatter["type"].(string)
			it.Status, _ = pg.Frontmatter["status"].(string)
			it.Tags, it.Aliases = stringList(pg.Frontmatter["tags"]), stringList(pg.Frontmatter["aliases"])
		}
		items = append(items, it)
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
	return a.reportMissing(missing, msg)
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

// noSuchFile is the message for a path that does not exist, isSubmodule the
// one for a submodule, which the read commands do not read; put and edit
// refuse to write over one with the same message. notAPage is the one for a
// file that the commands reading the attributes of a page cannot read.
const (
	noSuchFile  = "no such file or directory"
	isSubmodule = "is a submodule"
	notAPage    = "is not a page"
)

// notEmpty returns the paths that are not empty. An empty path names no file
// and git rejects it as a pathspec, so it never reaches git; the commands
// report it as noSuchFile.
func notEmpty(paths []string) []string {
	return slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return p == "" })
}

// displayPath returns p as a message names it. An empty path is shown as two
// quotation marks, since the message would otherwise start with its colon.
func displayPath(p string) string {
	if p == "" {
		return "''"
	}
	return escapeControl(p)
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
	if err := a.repo.CheckMissing(notEmpty(out)); err != nil {
		return nil, &gitError{err}
	}
	return out, nil
}

// fileMessage returns the message for reportMissing of paths that are not
// files where a file is required: "is a directory" for a directory, "is a
// submodule" for a submodule, else noSuchFile.
func (a *app) fileMessage(paths []string) (func(string) string, error) {
	return a.entryMessage(paths, false)
}

// pageMessage is fileMessage for the commands that read the attributes of a
// page: a file that is not a page is reported as notAPage.
func (a *app) pageMessage(paths []string) (func(string) string, error) {
	return a.entryMessage(paths, true)
}

// entryMessage returns the message for reportMissing of paths that cannot be
// read where, with page, a page is required and otherwise any file is.
func (a *app) entryMessage(paths []string, page bool) (func(string) string, error) {
	var ents []repo.Entry
	if ps := notEmpty(paths); len(ps) > 0 {
		var err error
		if ents, err = a.repo.Entries(ps); err != nil {
			return nil, &gitError{err}
		}
	}
	return func(p string) string {
		switch {
		case p == "." || slices.ContainsFunc(ents, func(e repo.Entry) bool { return strings.HasPrefix(e.Path, p+"/") }):
			return "is a directory"
		case slices.ContainsFunc(ents, func(e repo.Entry) bool { return e.Path == p && e.Type == "commit" }):
			return isSubmodule
		case page && slices.ContainsFunc(ents, func(e repo.Entry) bool { return e.Path == p }):
			return notAPage
		}
		return noSuchFile
	}, nil
}

// reportMissing prints "wikictl: <path>: <message>" on standard error for each
// path, with the message that msg returns for it, or noSuchFile when msg is
// nil, and returns exit status 1 when there is any path.
func (a *app) reportMissing(paths []string, msg func(string) string) error {
	for _, p := range paths {
		m := noSuchFile
		if msg != nil {
			m = msg(p)
		}
		fmt.Fprintf(a.stderr, "wikictl: %s: %s\n", displayPath(p), m)
	}
	if len(paths) > 0 {
		return exitStatus(ExitError)
	}
	return nil
}

type linkItem struct {
	Path      string `json:"path"`
	Direction string `json:"direction"`
	Type      string `json:"type"`
	Target    string `json:"target"`
	Note      string `json:"note"`
	Line      int    `json:"line"`
	URL       bool   `json:"url"`
}

func linksFlags(a *app, fs *pflag.FlagSet) {
	fs.BoolVarP(&a.linksIn, "in", "i", false, "list only the links from other pages to the pages")
	fs.BoolVarP(&a.linksOut, "out", "o", false, "list only the links in the pages")
	fs.BoolVarP(&a.withFilename, "with-filename", "H", false, "print the path before every link")
	fs.BoolVarP(&a.noFilename, "no-filename", "h", false, "print the links without the path")
}

// cmdLinks lists the links in each page ("out") and the links to it from other
// pages ("in"). A body link to a page or a URL that the Links section also
// links to is not listed again. The pages that hold the backlinks of every
// page read are found with one search and read together with those pages, so
// that the links of the whole wiki cost one search and one read rather than
// one of each per page; reading every page needs no search at all, since then
// every page is already a candidate.
func (a *app) cmdLinks(c *command, args []string) error {
	t, err := a.readTree(false)
	if err != nil {
		return err
	}
	all := slices.DeleteFunc(slices.Clone(t.files), func(p string) bool { return !page.IsPagePath(p) })
	slices.Sort(all)
	paths, files := slices.Clone(all), []string(nil)
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
	both := a.linksIn == a.linksOut
	var cands []string
	if both || a.linksIn {
		cands = all
		if len(paths) != len(all) {
			if cands, err = wiki.BacklinkCandidates(a.repo, notEmpty(paths)); err != nil {
				return &gitError{err}
			}
		}
	}
	read, err := wiki.ReadPages(a.repo, notEmpty(union(paths, cands)))
	if err != nil {
		return &gitError{err}
	}
	// An argument that is a file but not a page is reported, not read, as lint
	// reports it: the links of a page are an interpretation of the page format.
	missing := slices.DeleteFunc(slices.Clone(files), func(p string) bool { return read.Exists(p) && page.IsPagePath(p) })
	paths = slices.DeleteFunc(paths, func(p string) bool { return slices.Contains(missing, p) })
	var back map[string][]wiki.Backlink
	if both || a.linksIn {
		back = wiki.Backlinks(read, cands, paths)
	}
	// The path is printed where grep prints it: for more than one path, for
	// none, and for a directory, which stands for the pages under it.
	header := a.withFilename || !a.noFilename && (len(args) != 1 || t.isDir(args[0]))
	items := []linkItem{}
	for _, p := range paths {
		if both || a.linksOut {
			pg := read.Parse(p)
			typed := map[string]bool{}
			for _, l := range pg.Links {
				items = append(items, linkItem{p, "out", l.Type, l.Target, l.Note, l.Line, l.IsURL})
				typed[l.Target] = true
			}
			for _, m := range slices.Concat(pg.Mentions, pg.URLs) {
				if !typed[m.Target] {
					items = append(items, linkItem{p, "out", m.Type, m.Target, "", m.Line, m.IsURL})
				}
			}
		}
		for _, b := range back[p] {
			items = append(items, linkItem{p, "in", b.Type, b.Path, "", b.Line, false})
		}
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			if header {
				fmt.Fprintf(w, "%s\t", escapeControl(it.Path))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", it.Direction, it.Type, escapeControl(it.Target))
		}
	})
	if len(missing) > 0 {
		msg, err := a.pageMessage(missing)
		if err != nil {
			return err
		}
		return a.reportMissing(missing, msg)
	}
	return nil
}

// union returns the paths of a and b, sorted and without repetition.
func union(a, b []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(slices.Concat(a, b))))
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
			fmt.Fprintf(w, "%s: %s\n", k, escapeControl(fmt.Sprint(out[k])))
		}
	})
	return nil
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
