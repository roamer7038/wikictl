package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
)

// commit writes changes as one commit. A conflict is returned as a
// conflictError. rerun names the command to run again on a conflict; it is
// empty for put, whose conflicts are resolved by reapplying the change.
func (a *app) commit(changes []repo.Change, msg, rerun string) (*repo.Result, error) {
	au, err := a.author()
	if err != nil {
		return nil, &usageError{msg: err.Error()}
	}
	res, err := a.repo.Commit(changes, msg, au)
	if err != nil {
		var cf *repo.Conflict
		if errors.As(err, &cf) {
			return nil, &conflictError{cf, rerun}
		}
		return nil, &gitError{err}
	}
	return res, nil
}

type putOpts struct {
	base string
	msg  string
}

func putFlags(fs *flag.FlagSet) *putOpts {
	o := &putOpts{}
	fs.StringVar(&o.base, "base", "", "blob `sha` of the existing page, printed by get")
	fs.StringVar(&o.msg, "m", "", "commit `message`")
	return o
}

// msgFlag registers the -m flag shared by mv and rm.
func msgFlag(fs *flag.FlagSet) *string {
	return fs.String("m", "", "commit `message`")
}

func (a *app) cmdPut(c *command, args []string) error {
	fs := newFlagSet(c.name)
	o := putFlags(fs)
	rest, err := parseFlags(c, fs, args)
	if err != nil {
		return err
	}
	p := rest[0]
	content, err := io.ReadAll(a.stdin)
	if err != nil {
		return err
	}
	pg := page.Parse(p, content)
	for _, is := range pg.Issues {
		switch is.Code {
		case "bad_path", "frontmatter_invalid", "page_too_large":
			return &invalidError{is.Code + ": " + is.Message}
		default:
			a.warn(is)
		}
	}
	// Broken links never block the write, so that link targets can be created afterwards.
	broken, err := a.brokenLinks([]*page.Page{pg})
	if err != nil {
		return &gitError{err}
	}
	for _, is := range broken {
		a.warn(is)
	}
	msg := o.msg
	if msg == "" {
		msg = "wikictl: put " + p
	}
	base := o.base
	res, err := a.commit([]repo.Change{{Path: p, Content: content, Base: &base}}, msg, "")
	if err != nil {
		return err
	}
	out := map[string]string{"path": p, "sha": res.SHAs[p], "commit": res.Commit}
	a.emit(out, func(w io.Writer) { fmt.Fprintf(w, "%s\t%s\t%s\n", p, res.SHAs[p], res.Commit) })
	return nil
}

// warn prints a non-blocking issue on stderr.
func (a *app) warn(is page.Issue) {
	fmt.Fprintf(a.stderr, "wikictl: warning: %s:%d: %s: %s\n", is.Path, is.Line, is.Code, escapeControl(is.Message))
}

func (a *app) cmdRm(c *command, args []string) error {
	fs := newFlagSet(c.name)
	msg := msgFlag(fs)
	rest, err := parseFlags(c, fs, args)
	if err != nil {
		return err
	}
	p := rest[0]
	if err := page.CheckPath(p); err != nil {
		return &invalidError{"bad_path: " + err.Error()}
	}
	contents, shas, err := a.repo.CatSHA([]string{p})
	if err != nil {
		return &gitError{err}
	}
	if contents[p] == nil {
		return a.notFound(p)
	}
	if *msg == "" {
		*msg = "wikictl: rm " + p
	}
	base := shas[p]
	res, err := a.commit([]repo.Change{{Path: p, Delete: true, Base: &base}}, *msg, "rm")
	if err != nil {
		return err
	}
	a.emit(map[string]string{"path": p, "commit": res.Commit}, func(w io.Writer) { fmt.Fprintf(w, "%s\t%s\n", p, res.Commit) })
	return nil
}

func (a *app) cmdMv(c *command, args []string) error {
	fs := newFlagSet(c.name)
	msg := msgFlag(fs)
	rest, err := parseFlags(c, fs, args)
	if err != nil {
		return err
	}
	from, to := rest[0], rest[1]
	fromDir, toDir := strings.HasSuffix(from, "/"), strings.HasSuffix(to, "/")
	if fromDir && toDir {
		return a.mvDir(strings.TrimSuffix(from, "/"), strings.TrimSuffix(to, "/"), *msg)
	}
	if fromDir || toDir {
		return &usageError{c, "to move a directory, end both arguments with /"}
	}
	if err := page.CheckPath(from); err != nil {
		return &invalidError{"bad_path: " + err.Error()}
	}
	for _, is := range page.PathIssues(to) {
		if is.Code == "bad_path" {
			return &invalidError{is.Code + ": " + is.Message}
		}
		a.warn(is)
	}
	contents, err := a.repo.Cat([]string{from, to})
	if err != nil {
		return &gitError{err}
	}
	if _, ok := contents[from]; !ok {
		return a.notFound(from)
	}
	if _, exists := contents[to]; exists {
		return errors.New("page already exists: " + to)
	}
	if err := a.repo.CheckMissing([]string{to}); err != nil {
		return &gitError{err}
	}
	changes, err := a.relocate(map[string]string{from: to})
	if err != nil {
		return &gitError{err}
	}
	// Record the old file name in aliases when it changes.
	oldName := strings.TrimSuffix(path.Base(from), ".md")
	if oldName != strings.TrimSuffix(path.Base(to), ".md") {
		for i := range changes {
			if changes[i].Path == to {
				changes[i].Content = page.AddAlias(changes[i].Content, oldName)
			}
		}
	}
	if *msg == "" {
		*msg = "wikictl: mv " + from + " " + to
	}
	res, err := a.commit(changes, *msg, "mv")
	if err != nil {
		return err
	}
	a.emit(map[string]any{"path": to, "commit": res.Commit, "rewritten": len(changes) - 2},
		func(w io.Writer) { fmt.Fprintf(w, "%s -> %s\t%s\n", from, to, res.Commit) })
	return nil
}

// mvDir moves every page under from to the same relative position under to.
func (a *app) mvDir(from, to, msg string) error {
	for _, seg := range strings.Split(from, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return &invalidError{fmt.Sprintf("bad_path: %q is not a directory of the wiki", from+"/")}
		}
	}
	for _, seg := range strings.Split(to, "/") {
		if err := page.CheckName(seg); err != nil {
			return &invalidError{"bad_path: " + err.Error()}
		}
		if !page.Recommended(seg) {
			a.warn(page.Issue{Path: to + "/", Code: "name_style", Message: fmt.Sprintf("name %q: lowercase ASCII letters, digits and hyphens are recommended", seg)})
		}
	}
	listed, err := a.repo.List([]string{from})
	if err != nil {
		return &gitError{err}
	}
	// List also matches from itself when from is a file.
	var src []string
	for _, p := range listed {
		if strings.HasPrefix(p, from+"/") {
			src = append(src, p)
		}
	}
	if len(src) == 0 {
		return errors.New("no pages under " + from + "/")
	}
	dst, err := a.repo.List([]string{to})
	if err != nil {
		return &gitError{err}
	}
	if len(dst) > 0 {
		return errors.New("pages already exist under " + to + "/")
	}
	mapping := map[string]string{}
	for _, p := range src {
		np := to + strings.TrimPrefix(p, from)
		if err := page.CheckPath(np); err != nil {
			return &invalidError{"bad_path: " + np + ": " + err.Error()}
		}
		mapping[p] = np
	}
	changes, err := a.relocate(mapping)
	if err != nil {
		return &gitError{err}
	}
	if msg == "" {
		msg = "wikictl: mv " + from + "/ " + to + "/"
	}
	res, err := a.commit(changes, msg, "mv")
	if err != nil {
		return err
	}
	a.emit(map[string]any{"path": to + "/", "commit": res.Commit, "moved": len(src), "rewritten": len(changes) - 2*len(src)},
		func(w io.Writer) { fmt.Fprintf(w, "%s/ -> %s/ (%d pages)\t%s\n", from, to, len(src), res.Commit) })
	return nil
}

// relocate walks every page of the wiki and builds one change set for
// mapping (old path -> new path): moved pages get their own links re-based
// at the new location, and pages that refer to a moved page get those links
// rewritten. Every change carries the blob sha read here as its Base, and
// every new path requires that the path does not exist, so that a page
// changed or created since it was read makes the commit a conflict.
func (a *app) relocate(mapping map[string]string) ([]repo.Change, error) {
	all, err := a.repo.List(nil)
	if err != nil {
		return nil, err
	}
	contents, shas, err := a.repo.CatSHA(all)
	if err != nil {
		return nil, err
	}
	none := ""
	mapper := func(target string) (string, bool) { nt, ok := mapping[target]; return nt, ok }
	var changes []repo.Change
	for _, p := range all {
		content, base := contents[p], shas[p]
		if np, moved := mapping[p]; moved {
			nc, _ := page.Relocate(content, p, np, mapper)
			changes = append(changes, repo.Change{Path: np, Content: nc, Base: &none}, repo.Change{Path: p, Delete: true, Base: &base})
			continue
		}
		if nc, n := page.Relocate(content, p, p, mapper); n > 0 {
			changes = append(changes, repo.Change{Path: p, Content: nc, Base: &base})
		}
	}
	return changes, nil
}

const initReadme = `# wiki

A knowledge base shared by AI agents and people. A page is a Markdown file
whose frontmatter should have a one-line summary. Relations go in a "## Links"
section at the end; links are relative paths. Directories are scopes:
global/ for everything, personal/ for one user, projects/<name>/ and
machines/<name>/ for the rest.
`

func (a *app) cmdInit(c *command, args []string) error {
	head, err := a.repo.Head()
	if err != nil {
		return &gitError{err}
	}
	if head != "" {
		return errors.New("branch " + a.repo.Branch + " already exists on the remote; init only works on an empty repository")
	}
	empty := ""
	changes := []repo.Change{
		{Path: "README.md", Content: []byte(initReadme), Base: &empty},
		{Path: "global/index.md", Content: []byte("---\nsummary: Entry point for knowledge that does not depend on any project, machine or user\n---\n# global\n"), Base: &empty},
	}
	res, err := a.commit(changes, "wikictl: init", "")
	if err != nil {
		return err
	}
	a.emit(map[string]string{"commit": res.Commit}, func(w io.Writer) { fmt.Fprintf(w, "initialized: %s\n", res.Commit) })
	return nil
}
