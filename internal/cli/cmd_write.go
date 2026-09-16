package cli

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"sort"
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
		var pe *repo.PathError
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, &gitError{err}
	}
	return res, nil
}

func putFlags(a *app, fs *pflag.FlagSet) {
	fs.StringVar(&a.base, "base", "", "blob `sha` of the file to replace, from stat")
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
	content, err := io.ReadAll(a.stdin)
	if err != nil {
		return err
	}
	return a.writeFile(args[0], content, a.base, "put")
}

// writeFile checks content as put does and commits it as p, replacing the file
// whose blob sha is base, or creating the file when base is empty. cmd names
// the command in the default commit message.
func (a *app) writeFile(p string, content []byte, base, cmd string) error {
	if strings.HasSuffix(p, ".md") {
		pg := page.Parse(p, content)
		var blocking []page.Issue
		for _, is := range pg.Issues {
			switch is.Code {
			case "bad_path", "frontmatter_invalid", "page_too_large":
				blocking = append(blocking, is)
			default:
				if len(blocking) == 0 {
					a.warn(is)
				}
			}
		}
		// bad_path comes first in Issues. A page path at the root that is a
		// directory is reported as edit reports it, unless the content is
		// also rejected.
		switch {
		case len(blocking) == 1 && blocking[0].Code == "bad_path":
			return a.badPath(p, errors.New(blocking[0].Message))
		case len(blocking) > 0:
			return &invalidError{blocking[0].Code + ": " + blocking[0].Message}
		}
		// Broken links never block the write, so that link targets can be created afterwards.
		broken, err := wiki.BrokenLinks(a.repo, []*page.Page{pg})
		if err != nil {
			return &gitError{err}
		}
		for _, is := range broken {
			a.warn(is)
		}
	} else if err := checkFilePath(p); err != nil {
		return a.badPath(p, err)
	}
	// put resolves a conflict by reapplying the change; edit is run again.
	rerun := ""
	if cmd != "put" {
		rerun = cmd
	}
	res, err := a.commit([]repo.Change{{Path: p, Content: content, Base: &base}}, a.commitMessage(cmd, []string{p}), rerun)
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

// commitMessage returns the message given with -m, else the default commit
// message "wikictl: <cmd> <args>", which names only the first argument and the
// number of the others when the arguments are long.
func (a *app) commitMessage(cmd string, args []string) string {
	if a.msg != "" {
		return a.msg
	}
	msg := "wikictl: " + cmd + " " + strings.Join(args, " ")
	if len(msg) > 200 && len(args) > 1 {
		msg = fmt.Sprintf("wikictl: %s %s and %d more", cmd, args[0], len(args)-1)
	}
	return msg
}

// warn prints a non-blocking issue on stderr.
func (a *app) warn(is page.Issue) {
	fmt.Fprintf(a.stderr, "wikictl: warning: %s:%d: %s: %s\n", escapeControl(is.Path), is.Line, is.Code, escapeControl(is.Message))
}

// checkRm requires a path unless -f is given, which lets rm be run with paths
// that a caller built and that may be empty.
func (a *app) checkRm(c *command, args []string) error {
	if len(args) == 0 && !a.force {
		return &usageError{c, "missing argument"}
	}
	return nil
}

// cmdRm deletes files, and with -r directories, in one commit. As rm does, a
// path that cannot be deleted is reported and the others are still deleted.
func (a *app) cmdRm(c *command, args []string) error {
	// Only "rm -f" reaches this without a path; it deletes nothing.
	if len(args) == 0 {
		a.emit(map[string]any{"paths": []string{}, "commit": ""}, nil)
		return nil
	}
	for _, p := range args {
		for _, x := range strings.Split(p, "/") {
			if err := page.CheckName(x); err != nil {
				return &invalidError{"bad_path: " + err.Error()}
			}
		}
	}
	entries, err := a.repo.Entries(args)
	if err != nil {
		return &gitError{err}
	}
	files := make([]string, len(entries))
	shas := map[string]string{}
	for i, e := range entries {
		files[i], shas[e.Path] = e.Path, e.SHA
	}
	slices.Sort(files)
	for _, p := range args {
		if !strings.Contains(p, "/") && shas[p] != "" {
			return &invalidError{"bad_path: " + p + ": a file at the wiki root cannot be deleted"}
		}
	}
	var targets []string
	failed := false
	for _, p := range args {
		switch sub := filesUnder(files, p); {
		case len(sub) > 0 && !a.recursive:
			fmt.Fprintf(a.stderr, "wikictl: %s: is a directory\n", escapeControl(p))
			failed = true
		case len(sub) > 0:
			targets = append(targets, sub...)
		case shas[p] != "":
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
		var changes []repo.Change
		for _, p := range targets {
			base := shas[p]
			changes = append(changes, repo.Change{Path: p, Delete: true, Base: &base})
		}
		deleted = targets
		res, err := a.commit(changes, a.commitMessage("rm", args), "rm")
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

func mvFlags(a *app, fs *pflag.FlagSet) {
	fs.BoolVarP(&a.noTargetDir, "no-target-directory", "T", false, "treat the destination as the new name, not as a directory")
	fs.StringVarP(&a.targetDir, "target-directory", "t", "", "move every source into `dir`")
	msgFlag(a, fs)
	fs.BoolVarP(&a.verbose, "verbose", "v", false, "print each moved file and the commit")
}

type movedFile struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (a *app) checkMv(c *command, args []string) error {
	switch {
	case a.targetDir != "" && a.noTargetDir:
		return &usageError{c, "-t and -T cannot be combined"}
	case a.targetDir == "" && len(args) < 2:
		return &usageError{c, "missing destination"}
	case a.noTargetDir && len(args) > 2:
		return &usageError{c, "-T takes one source"}
	}
	srcs, dst := args, a.targetDir
	if dst == "" {
		srcs, dst = args[:len(args)-1], args[len(args)-1]
	}
	var err error
	a.cleaned, err = cleanPaths(append(slices.Clone(srcs), dst))
	return err
}

// cmdMv moves files and directories as mv does and rewrites the links to the
// moved pages, in one commit. A destination that exists is never replaced; as
// mv does, a source that cannot be moved is reported and the others are still
// moved.
func (a *app) cmdMv(c *command, args []string) error {
	// checkMv cleaned the sources followed by the destination.
	cleaned := a.cleaned
	srcs, dst := args[:len(cleaned)-1], cmp.Or(a.targetDir, args[len(args)-1])
	entries, err := a.repo.Entries(nil)
	if err != nil {
		return &gitError{err}
	}
	files := make([]string, len(entries))
	modes := map[string]string{}
	for i, e := range entries {
		files[i], modes[e.Path] = e.Path, e.Mode
	}
	slices.Sort(files)
	isSubmodule := func(f string) bool { return modes[f] == "160000" }
	// under returns the files below dir, or every file for the root.
	under := func(dir string) []string {
		if dir == "." {
			return files
		}
		return filesUnder(files, dir)
	}
	to := cleaned[len(cleaned)-1]
	into := !a.noTargetDir && len(under(to)) > 0
	if !into && (a.targetDir != "" || len(srcs) > 1) {
		return fmt.Errorf("%s: not a directory", to)
	}
	if !into && to == "." {
		return &invalidError{"bad_path: the root of the wiki cannot be replaced"}
	}
	mapping := map[string]string{}
	// movedTo holds the paths moved to, and movedToDirs the directories above them.
	movedTo, movedToDirs := map[string]bool{}, map[string]bool{}
	move := func(f, np string) {
		mapping[f] = np
		movedTo[np] = true
		for d := path.Dir(np); d != "." && !movedToDirs[d]; d = path.Dir(d) {
			movedToDirs[d] = true
		}
	}
	// taken reports whether p exists, or is, contains or is below a path already moved to.
	taken := func(p string) bool {
		if modes[p] != "" || (p != "." && len(under(p)) > 0) || movedTo[p] || movedToDirs[p] {
			return true
		}
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			if movedTo[d] {
				return true
			}
		}
		return false
	}
	// belowFile reports whether a directory above p is a file.
	belowFile := func(p string) bool {
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			if modes[d] != "" {
				return true
			}
		}
		return false
	}
	failed := false
	fail := func(p, msg string) {
		fmt.Fprintf(a.stderr, "wikictl: %s: %s\n", escapeControl(p), msg)
		failed = true
	}
	for i, src := range cleaned[:len(srcs)] {
		target := to
		if into {
			target = join(to, path.Base(src))
		}
		sub := under(src)
		switch {
		case src == ".":
			return &invalidError{"bad_path: the root of the wiki cannot be moved"}
		case len(sub) > 0:
			// Files moved by an earlier source are not moved again.
			sub = slices.DeleteFunc(slices.Clone(sub), func(f string) bool { _, moved := mapping[f]; return moved })
			if len(sub) == 0 {
				fail(srcs[i], "no such file or directory")
				continue
			}
			if slices.ContainsFunc(sub, isSubmodule) {
				fail(srcs[i], "cannot move a submodule")
				continue
			}
			if target == src || strings.HasPrefix(target, src+"/") {
				fail(srcs[i], "cannot move a directory into itself")
				continue
			}
			for _, seg := range strings.Split(target, "/") {
				if err := page.CheckName(seg); err != nil {
					return &invalidError{"bad_path: " + err.Error()}
				}
				if !page.Recommended(seg) {
					a.warn(page.NameStyle(target+"/", seg))
				}
			}
		case modes[src] != "":
			if _, moved := mapping[src]; moved {
				fail(srcs[i], "no such file or directory")
				continue
			}
			if !strings.Contains(src, "/") {
				return &invalidError{"bad_path: " + src + ": a file at the wiki root cannot be moved"}
			}
			if isSubmodule(src) {
				fail(srcs[i], "cannot move a submodule")
				continue
			}
			if strings.HasSuffix(srcs[i], "/") {
				fail(srcs[i], "not a directory")
				continue
			}
			if !into && strings.HasSuffix(dst, "/") {
				fail(dst, "not a directory")
				continue
			}
			if strings.HasSuffix(target, ".md") {
				for _, is := range page.PathIssues(target) {
					if is.Code == "bad_path" {
						return &invalidError{is.Code + ": " + is.Message}
					}
					a.warn(is)
				}
			} else if err := page.CheckFilePath(target); err != nil {
				return &invalidError{"bad_path: " + err.Error()}
			}
			sub = []string{src}
		default:
			fail(srcs[i], "no such file or directory")
			continue
		}
		if belowFile(target) {
			fail(target, "not a directory")
			continue
		}
		if taken(target) {
			fail(target, "not replacing")
			continue
		}
		for _, f := range sub {
			np := target + strings.TrimPrefix(f, src)
			if err := checkFilePath(np); err != nil {
				return &invalidError{"bad_path: " + np + ": " + err.Error()}
			}
			move(f, np)
		}
	}

	moved, rewritten, commit := []movedFile{}, 0, ""
	if len(mapping) > 0 {
		changes, n, err := a.moveChanges(entries, mapping, modes)
		if err != nil {
			return err
		}
		res, err := a.commit(changes, a.commitMessage("mv", append(slices.Clone(srcs), dst)), "mv")
		if err != nil {
			return err
		}
		for _, f := range slices.Sorted(maps.Keys(mapping)) {
			moved = append(moved, movedFile{f, mapping[f]})
		}
		rewritten, commit = n, res.Commit
	}
	a.emit(map[string]any{"moved": moved, "rewritten": rewritten, "commit": commit}, func(w io.Writer) {
		if a.verbose {
			for _, m := range moved {
				fmt.Fprintf(w, "%s\t%s\t%s\n", escapeControl(m.From), escapeControl(m.To), commit)
			}
		}
	})
	if failed {
		return exitStatus(ExitError)
	}
	return nil
}

// checkFilePath checks the path of a file that is written or moved to:
// CheckPath for a page, CheckFilePath for any other file.
func checkFilePath(p string) error {
	if strings.HasSuffix(p, ".md") {
		return page.CheckPath(p)
	}
	return page.CheckFilePath(p)
}

// badPath returns the error for p, a path that the path check rejected with
// err. A directory at the wiki root is reported as a directory, as a deeper
// one is; any other path is bad_path.
func (a *app) badPath(p string, err error) error {
	if !strings.Contains(p, "/") && page.CheckName(p) == nil {
		msg, ferr := a.fileMessage([]string{p})
		if ferr != nil {
			return ferr
		}
		if m := msg(p); m != noSuchFile {
			return fmt.Errorf("%s: %s", p, m)
		}
	}
	return &invalidError{"bad_path: " + err.Error()}
}

// moveChanges builds the changes that move the files of mapping (old path ->
// new path), each keeping its mode from modes: pages through wiki.Relocate
// over entries, the files of the whole tree, with the old name added to
// aliases when it changes, and other files unchanged. It also returns the
// number of other pages whose links were rewritten.
func (a *app) moveChanges(entries []repo.Entry, mapping, modes map[string]string) ([]repo.Change, int, error) {
	sources := slices.Collect(maps.Keys(mapping))
	objs, err := a.repo.Stat(sources)
	if err != nil {
		return nil, 0, &gitError{err}
	}
	if _, err := a.missing(sources, func(p string) bool { _, ok := objs[p]; return ok }); err != nil {
		return nil, 0, err
	}
	pages, others := map[string]string{}, []string{}
	for f, np := range mapping {
		if strings.HasSuffix(f, ".md") {
			pages[f] = np
		} else {
			others = append(others, f)
		}
	}
	var changes []repo.Change
	if len(pages) > 0 {
		if changes, err = wiki.Relocate(a.repo, entries, pages); err != nil {
			return nil, 0, &gitError{err}
		}
	}
	// A symbolic link named like a page is moved like a page, so that links to
	// it are rewritten, but its target is kept as it is.
	var symlinks []string
	for f := range pages {
		if modes[f] == "120000" {
			symlinks = append(symlinks, f)
		}
	}
	targetsOf, _, err := a.repo.CatSHA(symlinks)
	if err != nil {
		return nil, 0, &gitError{err}
	}
	targets := map[string]string{}
	for f, np := range pages {
		targets[np] = f
	}
	rewritten := 0
	for i, ch := range changes {
		from, moved := targets[ch.Path]
		if moved {
			changes[i].Mode = modes[from]
		}
		switch {
		case ch.Delete:
		case !moved:
			rewritten++
		case modes[from] == "120000":
			changes[i].Content = targetsOf[from]
		case strings.TrimSuffix(path.Base(from), ".md") != strings.TrimSuffix(path.Base(ch.Path), ".md"):
			changes[i].Content = page.AddAlias(ch.Content, strings.TrimSuffix(path.Base(from), ".md"))
		}
	}
	if len(others) > 0 {
		contents, shas, err := a.repo.CatSHA(others)
		if err != nil {
			return nil, 0, &gitError{err}
		}
		none := ""
		for _, f := range others {
			base := shas[f]
			changes = append(changes, repo.Change{Path: mapping[f], Content: contents[f], Base: &none, Mode: modes[f]}, repo.Change{Path: f, Delete: true, Base: &base})
		}
	}
	return changes, rewritten, nil
}

// filesUnder returns the files below directory dir from the sorted files: they
// sort from dir+"/" up to dir+"0", as '0' follows '/'.
func filesUnder(files []string, dir string) []string {
	return files[sort.SearchStrings(files, dir+"/"):sort.SearchStrings(files, dir+"0")]
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
