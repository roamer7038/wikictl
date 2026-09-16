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
// of each file. Paths that are not files are reported as cat reports them.
// Every file has a sha, which put --base needs, but only a page has
// attributes: they stay empty for a file that is not one.
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
		if isPage(p) {
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

// isPage reports whether p names a page, the only kind of file whose content
// is interpreted: a path ending in .md, as put reads it.
func isPage(p string) bool { return strings.HasSuffix(p, ".md") }

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
	pages, err := wiki.ReadPages(a.repo, notEmpty(args))
	if err != nil {
		return &gitError{err}
	}
	if !pages.Exists(p) || !isPage(p) {
		msg, err := a.pageMessage(args)
		if err != nil {
			return err
		}
		a.emit(map[string]any{"items": []linkItem{}}, nil)
		return a.reportMissing(args, msg)
	}
	both := a.linksIn == a.linksOut
	items := []linkItem{}
	if both || a.linksOut {
		pg := pages.Parse(p)
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
