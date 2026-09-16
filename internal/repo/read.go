package repo

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// IsPagePath reports whether p can be a page: a .md file below the root,
// with no component starting with a dot.
func IsPagePath(p string) bool {
	if !strings.HasSuffix(p, ".md") || !strings.Contains(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

// pathspec turns dirs into a git pathspec argument list, or nil for the whole tree.
func pathspec(dirs []string) []string {
	if len(dirs) == 0 {
		return nil
	}
	out := []string{"--"}
	for _, d := range dirs {
		out = append(out, strings.TrimSuffix(d, "/"))
	}
	return out
}

// Entry is a file of the tree: its path, mode, object type and object sha.
type Entry struct{ Path, Mode, Type, SHA string }

// parseTree returns the entries of the output of "ls-tree -z".
func parseTree(out string) []Entry {
	var res []Entry
	for rec := range strings.SplitSeq(out, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		if f := strings.Fields(meta); ok && len(f) == 3 {
			res = append(res, Entry{p, f[0], f[1], f[2]})
		}
	}
	return res
}

// Entries returns the files under dirs, or of the whole tree when dirs is nil,
// pages or not. Directories that do not exist are ignored.
func (r *Repo) Entries(dirs []string) ([]Entry, error) {
	if r.snapshot == "" {
		return nil, nil
	}
	out, err := r.Git(append([]string{"ls-tree", "-r", "-z", r.snapshot}, pathspec(dirs)...)...)
	if err != nil {
		return nil, err
	}
	return parseTree(out), nil
}

// Files returns the paths of Entries.
func (r *Repo) Files(dirs []string) ([]string, error) {
	ents, err := r.Entries(dirs)
	var res []string
	for _, e := range ents {
		res = append(res, e.Path)
	}
	return res, err
}

// FoldPattern turns word into an extended regular expression matching word
// ignoring case, non-ASCII letters included. A rune with other case forms
// becomes an alternation of all of them, so the match does not depend on the
// locale git runs in.
func FoldPattern(word string) string {
	var b strings.Builder
	for i := 0; i < len(word); {
		r, size := utf8.DecodeRuneInString(word[i:])
		lit := word[i : i+size]
		i += size
		if r == utf8.RuneError || unicode.SimpleFold(r) == r {
			b.WriteString(regexp.QuoteMeta(lit))
			continue
		}
		b.WriteString("(" + lit)
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			b.WriteString("|" + string(f))
		}
		b.WriteString(")")
	}
	return b.String()
}

// emptyTree is the object name of the empty tree, which every repository
// knows without holding the object.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// GrepRecords runs "git grep -z" with flags and the patterns, each given with
// -e, on the files under dirs at the commit that reads use, and returns one
// record per entry of the output: a path with -l or -L, a path and a count
// with -c, and otherwise a path, a line number and the line. Column output is
// turned off whatever the git configuration says. No match is not an error.
func (r *Repo) GrepRecords(flags, patterns, dirs []string) ([][]string, error) {
	// A wiki without any commit is searched as the empty tree instead of
	// skipping git, so that the exit codes stay the same: a pattern that does
	// not compile is still an error of git, and one that matches nothing is
	// still no match.
	tree := r.snapshot
	if tree == "" {
		tree = emptyTree
	}
	args := append([]string{"grep", "-z", "--no-column"}, flags...)
	for _, p := range patterns {
		args = append(args, "-e", p)
	}
	args = append(append(args, tree), pathspec(dirs)...)
	out, err := r.gitStrict(args...)
	if noResult(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A path ends with NUL, and the last field of a record with a newline; a
	// path may contain a newline, but a line cannot.
	fields := 3
	switch {
	case slices.Contains(flags, "-l") || slices.Contains(flags, "-L"):
		fields = 1
	case slices.Contains(flags, "-c"):
		fields = 2
	}
	prefix := tree + ":"
	var res [][]string
	for out != "" {
		f := make([]string, fields)
		for i := range f {
			sep := "\x00"
			if i > 0 && i == fields-1 {
				sep = "\n"
			}
			f[i], out, _ = strings.Cut(out, sep)
		}
		f[0] = strings.TrimPrefix(f[0], prefix)
		res = append(res, f)
	}
	return res, nil
}

// CatSHA returns the contents and the blob sha of paths using one "cat-file
// --batch" call, so that a change built from the contents can use the sha as
// its Base. Paths that do not exist or are not files are absent from the
// result.
func (r *Repo) CatSHA(paths []string) (map[string][]byte, map[string]string, error) {
	res, shas := map[string][]byte{}, map[string]string{}
	names, err := r.refPaths(paths)
	if err != nil {
		return nil, nil, err
	}
	ents, err := r.catFile(names, true)
	if err != nil {
		return nil, nil, err
	}
	for i, p := range paths {
		if e := ents[i]; e.typ == "blob" {
			res[p] = e.content
			shas[p] = e.SHA
		}
	}
	return res, shas, nil
}

// Object is the sha and size of a blob.
type Object struct {
	SHA  string
	Size int64
}

// Stat returns the sha and size of every path that is a file at the commit
// that reads use, using one "cat-file --batch-check" call, which reads no
// contents.
// Paths that do not exist or are not files are absent from the result.
func (r *Repo) Stat(paths []string) (map[string]Object, error) {
	res := map[string]Object{}
	names, err := r.refPaths(paths)
	if err != nil {
		return nil, err
	}
	ents, err := r.catFile(names, false)
	if err != nil {
		return nil, err
	}
	for i, p := range paths {
		if e := ents[i]; e.typ == "blob" {
			res[p] = e.Object
		}
	}
	return res, nil
}

// CatLimit returns the contents of the files of at most max bytes, and the
// sha and size of every file as Stat returns them; the contents of the files
// over max are not read. Paths that do not exist or are not files are absent
// from the result; a path that is a blob the mirror cannot read is an error,
// as CheckMissing reports it.
func (r *Repo) CatLimit(paths []string, max int64) (contents map[string][]byte, objs map[string]Object, err error) {
	objs, err = r.Stat(paths)
	if err != nil {
		return nil, nil, err
	}
	contents = map[string][]byte{}
	var small, shas, absent []string
	for _, p := range paths {
		o, ok := objs[p]
		switch {
		case !ok:
			absent = append(absent, p)
		case o.Size > max:
		default:
			small = append(small, p)
			shas = append(shas, o.SHA)
		}
	}
	if err := r.CheckMissing(absent); err != nil {
		return nil, nil, err
	}
	ents, err := r.catFile(shas, true)
	if err != nil {
		return nil, nil, err
	}
	for i, p := range small {
		if e := ents[i]; e.typ == "blob" {
			contents[p] = e.content
		}
	}
	return contents, objs, nil
}

// refPaths turns paths into object names at the commit that reads use, for
// catFile. cat-file reads one name per line and drops a trailing carriage
// return, so a path with a newline or a carriage return is looked up with
// ls-tree instead and named by its blob sha, or by zeroSHA, which cat-file
// reports missing, when it is not a blob. git is run only for such paths.
func (r *Repo) refPaths(paths []string) ([]string, error) {
	names := make([]string, len(paths))
	var odd []string
	for i, p := range paths {
		names[i] = r.snapshot + ":" + p
		if strings.ContainsAny(p, "\n\r") {
			odd = append(odd, p)
		}
	}
	if len(odd) == 0 || r.snapshot == "" {
		return names, nil
	}
	ents, err := r.treeEntries(odd)
	if err != nil {
		return nil, err
	}
	for i, p := range paths {
		if strings.ContainsAny(p, "\n\r") {
			names[i] = zeroSHA
			if e := ents[p]; e.Type == "blob" {
				names[i] = e.SHA
			}
		}
	}
	return names, nil
}

// catEntry is the answer of cat-file for one object name. typ is empty when
// the object is missing.
type catEntry struct {
	Object
	typ     string
	content []byte
}

// catFile looks up names with one "cat-file --batch" call, or with
// "cat-file --batch-check" when withContent is false, and returns one entry
// per name. When Snapshot found no branch, every name is missing and git is
// not run, so that a branch fetched since then is not read.
// The names are given one per line, so none may contain a newline or a
// carriage return; see refPaths.
func (r *Repo) catFile(names []string, withContent bool) ([]catEntry, error) {
	res := make([]catEntry, len(names))
	if len(names) == 0 || r.snapshot == "" {
		return res, nil
	}
	args := []string{"cat-file", "--batch-check"}
	if withContent {
		args[1] = "--batch"
	}
	var in bytes.Buffer
	for _, n := range names {
		in.WriteString(n + "\n")
	}
	out, err := r.runGit(false, nil, in.Bytes(), args...)
	if err != nil {
		return nil, err
	}
	rd := bufio.NewReader(strings.NewReader(out))
	for i := range names {
		hdr, err := rd.ReadString('\n')
		if err != nil {
			break
		}
		f := strings.Fields(hdr)
		if len(f) < 3 {
			continue // "<object> missing"
		}
		n, _ := strconv.ParseInt(f[2], 10, 64)
		e := catEntry{Object: Object{SHA: f[0], Size: n}, typ: f[1]}
		if withContent {
			e.content = make([]byte, n)
			if _, err := io.ReadFull(rd, e.content); err != nil {
				return nil, err
			}
			rd.ReadByte()
		}
		res[i] = e
	}
	return res, nil
}

// Updated returns the last commit time of each of files, reading "git log -z
// --name-only --full-history" from the newest commit and stopping once every
// file has been seen. The log is limited to the directories of the files when
// they are in a few directories; git log slows down with every path it is
// given. Renames are not followed.
func (r *Repo) Updated(files []string) (map[string]time.Time, error) {
	res := map[string]time.Time{}
	if r.snapshot == "" || len(files) == 0 {
		return res, nil
	}
	want := map[string]bool{}
	var dirs []string
	for _, f := range files {
		want[f] = true
		if d := path.Dir(f); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) > 8 || slices.Contains(dirs, ".") {
		dirs = nil
	}
	args := append([]string{"log", "-z", "--format=%x00%x01%cI", "--name-only", "--full-history", r.snapshot}, pathspec(dirs)...)
	c := r.command(nil, args)
	var errb bytes.Buffer
	c.Stderr = &errb
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, &GitError{Args: args, Err: err}
	}
	// Each commit is "\x00\x01<time>\x00", then a newline and its paths, each
	// ending with NUL. Paths are not quoted, and none is empty.
	rd := bufio.NewReader(out)
	var cur time.Time
	prev, first := "-", false
	for len(res) < len(want) {
		tok, err := rd.ReadString(0)
		if err != nil {
			break
		}
		tok = tok[:len(tok)-1]
		if prev == "" && strings.HasPrefix(tok, "\x01") {
			cur, _ = time.Parse(time.RFC3339, tok[1:])
			prev, first = tok, true
			continue
		}
		p := tok
		if first {
			p = strings.TrimPrefix(tok, "\n")
		}
		if _, seen := res[p]; want[p] && !seen {
			res[p] = cur
		}
		prev, first = tok, false
	}
	if len(res) == len(want) {
		c.Process.Kill()
		c.Wait()
		return res, nil
	}
	if err := c.Wait(); err != nil {
		return nil, &GitError{Args: args, Stderr: errb.String(), Err: err}
	}
	return res, nil
}

// maxTreeArgs is the number of bytes of paths given to one ls-tree call by
// treeEntries, which keeps the command line within the limits of Windows.
const maxTreeArgs = 8 << 10

// treeEntries returns the entry at the commit that reads use of each of paths
// that exists. It uses ls-tree, which fails when a tree on the way cannot be
// read, unlike "rev-parse <commit>:<path>", which then exits with 1 and
// nothing on stderr as it does for an absent path. The paths are split among
// as few calls as maxTreeArgs allows.
func (r *Repo) treeEntries(paths []string) (map[string]Entry, error) {
	paths = slices.Compact(slices.Sorted(slices.Values(paths)))
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	res := map[string]Entry{}
	for len(paths) > 0 {
		args, n := []string{"ls-tree", "-z", r.snapshot, "--"}, 0
		for len(paths) > 0 && (n == 0 || n+len(paths[0]) <= maxTreeArgs) {
			args = append(args, paths[0])
			n += len(paths[0]) + 1
			paths = paths[1:]
		}
		out, err := r.Git(args...)
		if err != nil {
			return nil, err
		}
		for _, e := range parseTree(out) {
			if want[e.Path] {
				res[e.Path] = e
			}
		}
	}
	return res, nil
}

// CheckMissing is called for paths that CatSHA or Stat did not return. It
// returns an error when git cannot tell whether a path exists at the commit
// that reads use, or when a path is a blob there that the mirror cannot read;
// cat-file --batch reports both as "missing". A path that does not exist or is
// not a file is not an error. git is not run when paths is empty.
func (r *Repo) CheckMissing(paths []string) error {
	if len(paths) == 0 || r.snapshot == "" {
		return nil
	}
	ents, err := r.treeEntries(paths)
	if err != nil {
		return err
	}
	for _, p := range paths {
		e, ok := ents[p]
		if !ok || e.Type != "blob" {
			continue
		}
		if _, err := r.Git("cat-file", "-e", e.SHA); err != nil {
			return fmt.Errorf("cannot read %s (blob %s) from the mirror: %w", p, e.SHA, err)
		}
	}
	return nil
}
