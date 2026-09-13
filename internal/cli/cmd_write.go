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

type conflictOut struct {
	Error   string `json:"error"`
	Reason  string `json:"reason"`
	Path    string `json:"path"`
	SHA     string `json:"sha"`
	Content string `json:"content"`
	Message string `json:"message"`
}

// commit writes changes as one commit and reports conflicts and git failures.
// The second result is the exit code; it is ExitOK when the first result is set.
func (a *app) commit(changes []repo.Change, msg string) (*repo.Result, int) {
	au, err := a.author()
	if err != nil {
		return nil, a.fail(ExitUsage, "usage", err.Error())
	}
	res, err := a.repo.Commit(changes, msg, au)
	if err != nil {
		var cf *repo.Conflict
		if errors.As(err, &cf) {
			if a.json {
				a.emit(conflictOut{"conflict", cf.Reason, cf.Path, cf.SHA, string(cf.Content), "the page changed since it was read; re-read the current content and reapply the change"}, nil)
			} else {
				fmt.Fprintf(a.stderr, "wikictl: conflict (%s): %s sha=%s\n", cf.Reason, cf.Path, cf.SHA)
				a.stdout.Write(cf.Content)
			}
			return nil, ExitConflict
		}
		return nil, a.fail(ExitGit, "git", err.Error())
	}
	return res, ExitOK
}

type putOpts struct {
	base string
	msg  string
}

func putFlags(fs *flag.FlagSet) *putOpts {
	o := &putOpts{}
	fs.StringVar(&o.base, "base", "", "blob `sha` of the existing page as printed by get; omit for a new page")
	fs.StringVar(&o.msg, "m", "", "commit `message`")
	return o
}

// msgFlag registers the -m flag shared by mv and rm.
func msgFlag(fs *flag.FlagSet) *string {
	return fs.String("m", "", "commit `message`")
}

func (a *app) cmdPut(c *command, args []string) int {
	fs := newFlagSet(c.name)
	o := putFlags(fs)
	rest, code, ok := a.parseFlags(c, fs, args)
	if !ok {
		return code
	}
	p := rest[0]
	content, err := io.ReadAll(a.stdin)
	if err != nil {
		return a.fail(ExitError, "error", err.Error())
	}
	pg := page.Parse(p, content)
	for _, is := range pg.Issues {
		switch is.Code {
		case "bad_path", "frontmatter_invalid":
			return a.fail(ExitInvalid, "invalid", is.Code+": "+is.Message)
		default:
			a.warn(is)
		}
	}
	a.warnBroken(pg)
	msg := o.msg
	if msg == "" {
		msg = "wikictl: put " + p
	}
	base := o.base
	res, code := a.commit([]repo.Change{{Path: p, Content: content, Base: &base}}, msg)
	if code != ExitOK {
		return code
	}
	out := map[string]string{"path": p, "sha": res.SHAs[p], "commit": res.Commit}
	a.emit(out, func(w io.Writer) { fmt.Fprintf(w, "%s\t%s\t%s\n", p, res.SHAs[p], res.Commit) })
	return ExitOK
}

// warn prints a non-blocking issue on stderr.
func (a *app) warn(is page.Issue) {
	fmt.Fprintf(a.stderr, "wikictl: warning: %s:%d: %s: %s\n", is.Path, is.Line, is.Code, is.Message)
}

// warnBroken reports links to missing pages on stderr. It never blocks the
// write, so that link targets can be created afterwards.
func (a *app) warnBroken(pg *page.Page) {
	var targets []string
	for _, l := range append(pg.Links, pg.Mentions...) {
		if !l.IsURL {
			targets = append(targets, l.Target)
		}
	}
	if len(targets) == 0 {
		return
	}
	found, _ := a.repo.Cat(targets)
	for _, l := range append(pg.Links, pg.Mentions...) {
		if !l.IsURL && found[l.Target] == nil {
			fmt.Fprintf(a.stderr, "wikictl: warning: %s:%d: broken_link: %s\n", pg.Path, l.Line, l.Target)
		}
	}
}

func (a *app) cmdRm(c *command, args []string) int {
	fs := newFlagSet(c.name)
	msg := msgFlag(fs)
	rest, code, ok := a.parseFlags(c, fs, args)
	if !ok {
		return code
	}
	p := rest[0]
	if contents, _ := a.repo.Cat([]string{p}); contents[p] == nil {
		return a.fail(ExitError, "error", "page not found: "+p)
	}
	if *msg == "" {
		*msg = "wikictl: rm " + p
	}
	res, code := a.commit([]repo.Change{{Path: p, Delete: true}}, *msg)
	if code != ExitOK {
		return code
	}
	a.emit(map[string]string{"path": p, "commit": res.Commit}, func(w io.Writer) { fmt.Fprintf(w, "%s\t%s\n", p, res.Commit) })
	return ExitOK
}

func (a *app) cmdMv(c *command, args []string) int {
	fs := newFlagSet(c.name)
	msg := msgFlag(fs)
	rest, code, ok := a.parseFlags(c, fs, args)
	if !ok {
		return code
	}
	from, to := rest[0], rest[1]
	fromDir, toDir := strings.HasSuffix(from, "/"), strings.HasSuffix(to, "/")
	if fromDir && toDir {
		return a.mvDir(strings.TrimSuffix(from, "/"), strings.TrimSuffix(to, "/"), *msg)
	}
	if fromDir || toDir {
		return a.usageError(c, "to move a directory, end both arguments with /")
	}
	for _, is := range page.PathIssues(to) {
		if is.Code == "bad_path" {
			return a.fail(ExitInvalid, "invalid", is.Code+": "+is.Message)
		}
		a.warn(is)
	}
	contents, err := a.repo.Cat([]string{from, to})
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	if _, ok := contents[from]; !ok {
		return a.fail(ExitError, "error", "page not found: "+from)
	}
	if _, exists := contents[to]; exists {
		return a.fail(ExitError, "error", "page already exists: "+to)
	}
	changes, err := a.relocate(map[string]string{from: to})
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
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
	res, code := a.commit(changes, *msg)
	if code != ExitOK {
		return code
	}
	a.emit(map[string]any{"path": to, "commit": res.Commit, "rewritten": len(changes) - 2},
		func(w io.Writer) { fmt.Fprintf(w, "%s -> %s\t%s\n", from, to, res.Commit) })
	return ExitOK
}

// mvDir moves every page under from to the same relative position under to.
func (a *app) mvDir(from, to, msg string) int {
	for _, seg := range strings.Split(from, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return a.fail(ExitInvalid, "invalid", fmt.Sprintf("bad_path: %q is not a directory of the wiki", from+"/"))
		}
	}
	for _, seg := range strings.Split(to, "/") {
		if err := page.CheckName(seg); err != nil {
			return a.fail(ExitInvalid, "invalid", "bad_path: "+err.Error())
		}
		if !page.Recommended(seg) {
			a.warn(page.Issue{Path: to + "/", Code: "name_style", Message: fmt.Sprintf("name %q: lowercase ASCII letters, digits and hyphens are recommended", seg)})
		}
	}
	src, err := a.repo.List([]string{from})
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	if len(src) == 0 {
		return a.fail(ExitError, "error", "no pages under "+from+"/")
	}
	if dst, _ := a.repo.List([]string{to}); len(dst) > 0 {
		return a.fail(ExitError, "error", "pages already exist under "+to+"/")
	}
	mapping := map[string]string{}
	for _, p := range src {
		mapping[p] = to + strings.TrimPrefix(p, from)
	}
	changes, err := a.relocate(mapping)
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	if msg == "" {
		msg = "wikictl: mv " + from + "/ " + to + "/"
	}
	res, code := a.commit(changes, msg)
	if code != ExitOK {
		return code
	}
	a.emit(map[string]any{"path": to + "/", "commit": res.Commit, "moved": len(src), "rewritten": len(changes) - 2*len(src)},
		func(w io.Writer) { fmt.Fprintf(w, "%s/ -> %s/ (%d pages)\t%s\n", from, to, len(src), res.Commit) })
	return ExitOK
}

// relocate walks every page of the wiki and builds one change set for
// mapping (old path -> new path): moved pages get their own links re-based
// at the new location, and pages that refer to a moved page get those links
// rewritten.
func (a *app) relocate(mapping map[string]string) ([]repo.Change, error) {
	all, err := a.repo.List(nil)
	if err != nil {
		return nil, err
	}
	contents, err := a.repo.Cat(all)
	if err != nil {
		return nil, err
	}
	mapper := func(target string) (string, bool) { nt, ok := mapping[target]; return nt, ok }
	var changes []repo.Change
	for _, p := range all {
		content := contents[p]
		if np, moved := mapping[p]; moved {
			nc, _ := page.Relocate(content, p, np, mapper)
			changes = append(changes, repo.Change{Path: np, Content: nc}, repo.Change{Path: p, Delete: true})
			continue
		}
		if nc, n := page.Relocate(content, p, p, mapper); n > 0 {
			changes = append(changes, repo.Change{Path: p, Content: nc})
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

func (a *app) cmdInit(c *command, args []string) int {
	if head, _ := a.repo.Head(); head != "" {
		return a.fail(ExitError, "error", "branch "+a.repo.Branch+" already exists on the remote; init only works on an empty repository")
	}
	empty := ""
	changes := []repo.Change{
		{Path: "README.md", Content: []byte(initReadme), Base: &empty},
		{Path: "global/index.md", Content: []byte("---\nsummary: Entry point for knowledge that does not depend on any project, machine or user\n---\n# global\n"), Base: &empty},
	}
	res, code := a.commit(changes, "wikictl: init")
	if code != ExitOK {
		return code
	}
	a.emit(map[string]string{"commit": res.Commit}, func(w io.Writer) { fmt.Fprintf(w, "initialized: %s\n", res.Commit) })
	return ExitOK
}
