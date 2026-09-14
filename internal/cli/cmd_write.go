package cli

import (
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
	"github.com/roamer7038/wikictl/internal/wiki"
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

func putFlags(a *app, fs *pflag.FlagSet) {
	fs.StringVar(&a.base, "base", "", "blob `sha` of the existing file as printed by stat; omit for a new file")
	msgFlag(a, fs)
	fs.BoolVarP(&a.verbose, "verbose", "v", false, "print the path, blob sha and commit")
}

func rmFlags(a *app, fs *pflag.FlagSet) {
	fs.BoolVarP(&a.recursive, "recursive", "r", false, "delete directories and every file under them")
	fs.BoolVarP(&a.force, "force", "f", false, "ignore paths that do not exist")
	msgFlag(a, fs)
	fs.BoolVarP(&a.verbose, "verbose", "v", false, "print each deleted path and the commit")
}

// msgFlag registers the -m flag shared by the commands that commit.
func msgFlag(a *app, fs *pflag.FlagSet) {
	fs.StringVarP(&a.msg, "message", "m", "", "commit `message`")
}

func (a *app) cmdPut(c *command, args []string) error {
	p := args[0]
	content, err := io.ReadAll(a.stdin)
	if err != nil {
		return err
	}
	if strings.HasSuffix(p, ".md") {
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
		broken, err := wiki.BrokenLinks(a.repo, []*page.Page{pg})
		if err != nil {
			return &gitError{err}
		}
		for _, is := range broken {
			a.warn(is)
		}
	} else if err := page.CheckFilePath(p); err != nil {
		return &invalidError{"bad_path: " + err.Error()}
	}
	msg := a.msg
	if msg == "" {
		msg = "wikictl: put " + p
	}
	base := a.base
	res, err := a.commit([]repo.Change{{Path: p, Content: content, Base: &base}}, msg, "")
	if err != nil {
		return err
	}
	out := map[string]string{"path": p, "sha": res.SHAs[p], "commit": res.Commit}
	a.emit(out, func(w io.Writer) {
		if a.verbose {
			fmt.Fprintf(w, "%s\t%s\t%s\n", escapeControl(p), res.SHAs[p], res.Commit)
		}
	})
	return nil
}

// warn prints a non-blocking issue on stderr.
func (a *app) warn(is page.Issue) {
	fmt.Fprintf(a.stderr, "wikictl: warning: %s:%d: %s: %s\n", is.Path, is.Line, is.Code, escapeControl(is.Message))
}

// cmdRm deletes files, and with -r directories, in one commit. As rm does, a
// path that cannot be deleted is reported and the others are still deleted.
func (a *app) cmdRm(c *command, args []string) error {
	files, err := a.repo.Files(args)
	if err != nil {
		return &gitError{err}
	}
	under := func(p string) []string {
		return slices.DeleteFunc(slices.Clone(files), func(f string) bool { return !strings.HasPrefix(f, p+"/") })
	}
	for _, p := range args {
		for _, x := range strings.Split(p, "/") {
			if err := page.CheckName(x); err != nil {
				return &invalidError{"bad_path: " + err.Error()}
			}
		}
		if !strings.Contains(p, "/") && len(under(p)) == 0 {
			return &invalidError{"bad_path: " + p + ": a file at the wiki root cannot be deleted"}
		}
	}
	var targets []string
	failed := false
	for _, p := range args {
		switch sub := under(p); {
		case len(sub) > 0 && !a.recursive:
			fmt.Fprintf(a.stderr, "wikictl: %s: is a directory\n", escapeControl(p))
			failed = true
		case len(sub) > 0:
			targets = append(targets, sub...)
		case slices.Contains(files, p):
			targets = append(targets, p)
		case !a.force:
			fmt.Fprintf(a.stderr, "wikictl: %s: no such file or directory\n", escapeControl(p))
			failed = true
		}
	}
	slices.Sort(targets)
	targets = slices.Compact(targets)
	deleted, commit := []string{}, ""
	if len(targets) > 0 {
		objs, err := a.repo.Stat(targets)
		if err != nil {
			return &gitError{err}
		}
		if _, err := a.missing(targets, func(p string) bool { _, ok := objs[p]; return ok }); err != nil {
			return err
		}
		var changes []repo.Change
		for _, p := range targets {
			if o, ok := objs[p]; ok {
				base := o.SHA
				changes = append(changes, repo.Change{Path: p, Delete: true, Base: &base})
				deleted = append(deleted, p)
			}
		}
		msg := a.msg
		if msg == "" {
			msg = "wikictl: rm " + strings.Join(args, " ")
		}
		res, err := a.commit(changes, msg, "rm")
		if err != nil {
			return err
		}
		commit = res.Commit
	}
	a.emit(map[string]any{"paths": deleted, "commit": commit}, func(w io.Writer) {
		if a.verbose {
			for _, p := range deleted {
				fmt.Fprintf(w, "%s\t%s\n", escapeControl(p), commit)
			}
		}
	})
	if failed {
		return exitStatus(ExitError)
	}
	return nil
}

func (a *app) cmdMv(c *command, args []string) error {
	fromDir, toDir := strings.HasSuffix(args[0], "/"), strings.HasSuffix(args[1], "/")
	cleaned, err := cleanPaths(args)
	if err != nil {
		return err
	}
	from, to := cleaned[0], cleaned[1]
	if fromDir && toDir {
		return a.mvDir(from, to, a.msg)
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
	changes, err := wiki.Relocate(a.repo, map[string]string{from: to})
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
	msg := a.msg
	if msg == "" {
		msg = "wikictl: mv " + from + " " + to
	}
	res, err := a.commit(changes, msg, "mv")
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
	changes, err := wiki.Relocate(a.repo, mapping)
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

// cleanPaths applies wiki.Clean to paths given on the command line.
func cleanPaths(paths []string) ([]string, error) {
	out := make([]string, len(paths))
	for i, p := range paths {
		c, err := wiki.Clean(p)
		if err != nil {
			return nil, &invalidError{"bad_path: " + err.Error()}
		}
		out[i] = c
	}
	return out, nil
}
