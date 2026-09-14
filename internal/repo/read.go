package repo

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// isPagePath reports whether p can be a page: a .md file below the root,
// with no component starting with a dot.
func isPagePath(p string) bool {
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

// stripRef removes the "<ref>:" prefix that ls-tree and grep print, and drops non-pages.
func stripRef(r *Repo, lines string) []string {
	var res []string
	prefix := r.trackingRef() + ":"
	for _, l := range strings.Split(strings.TrimSpace(lines), "\n") {
		p := strings.TrimPrefix(l, prefix)
		if p != "" && isPagePath(p) {
			res = append(res, p)
		}
	}
	return res
}

// List returns the page paths under dirs (or the whole tree when dirs is nil).
// Directories that do not exist are ignored.
func (r *Repo) List(dirs []string) ([]string, error) {
	head, err := r.Head()
	if err != nil || head == "" {
		return nil, err
	}
	args := append([]string{"ls-tree", "-r", "--name-only", r.trackingRef()}, pathspec(dirs)...)
	out, err := r.Git(args...)
	if err != nil {
		return nil, err
	}
	return stripRef(r, out), nil
}

// Fold maps every rune of s to the smallest rune of its simple Unicode case
// folding orbit, so that strings.Contains(Fold(s), Fold(word)) matches word
// in s ignoring case, non-ASCII letters included.
func Fold(s string) string { return strings.Map(foldRune, s) }

func foldRune(r rune) rune {
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		m = min(m, f)
	}
	return m
}

// foldPattern turns word into an extended regular expression matching the
// same text as Fold. A rune with other case forms becomes an alternation of
// all of them, so the match does not depend on the locale git runs in.
func foldPattern(word string) string {
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

// Grep returns the pages under dirs that contain the words as fixed strings,
// ignoring case as Fold does. With all set, a page must contain every word
// (--all-match).
func (r *Repo) Grep(words []string, all bool, dirs []string) ([]string, error) {
	head, err := r.Head()
	if err != nil || head == "" || len(words) == 0 {
		return nil, err
	}
	args := []string{"grep", "-E", "-l"}
	if all {
		args = append(args, "--all-match")
	}
	for _, w := range words {
		args = append(args, "-e", foldPattern(w))
	}
	args = append(args, r.trackingRef())
	args = append(args, pathspec(dirs)...)
	out, err := r.gitStrict(args...)
	if noResult(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return stripRef(r, out), nil
}

// GrepDeprecated returns the set of pages under dirs whose frontmatter has "status: deprecated".
func (r *Repo) GrepDeprecated(dirs []string) (map[string]bool, error) {
	res := map[string]bool{}
	head, err := r.Head()
	if err != nil {
		return nil, err
	}
	if head == "" {
		return res, nil
	}
	args := append([]string{"grep", "-l", "-E", "-e", `^status:[[:space:]]*deprecated[[:space:]]*$`, r.trackingRef()}, pathspec(dirs)...)
	out, err := r.gitStrict(args...)
	if noResult(err) {
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	for _, p := range stripRef(r, out) {
		res[p] = true
	}
	return res, nil
}

// Cat returns the contents of paths using one "cat-file --batch" call.
// Paths that do not exist or are not files are absent from the result.
func (r *Repo) Cat(paths []string) (map[string][]byte, error) {
	res, _, err := r.CatSHA(paths)
	return res, err
}

// CatSHA is Cat that also returns the blob sha of every path it read, so that
// a change built from the contents can use the sha as its Base.
func (r *Repo) CatSHA(paths []string) (map[string][]byte, map[string]string, error) {
	res, shas := map[string][]byte{}, map[string]string{}
	if len(paths) == 0 {
		return res, shas, nil
	}
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(r.trackingRef() + ":" + p + "\n")
	}
	out, err := r.GitIn(in.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, nil, err
	}
	rd := bufio.NewReader(strings.NewReader(out))
	for _, p := range paths {
		hdr, err := rd.ReadString('\n')
		if err != nil {
			break
		}
		f := strings.Fields(hdr)
		if len(f) < 3 {
			continue // "<object> missing"
		}
		n, _ := strconv.Atoi(f[2])
		buf := make([]byte, n)
		if _, err := io.ReadFull(rd, buf); err != nil {
			return nil, nil, err
		}
		rd.ReadByte()
		if f[1] == "blob" {
			res[p] = buf
			shas[p] = f[0]
		}
	}
	return res, shas, nil
}

// Updated returns the last commit time of every file under dirs, from one
// pass over "git log --name-only". Renames are not followed.
func (r *Repo) Updated(dirs []string) (map[string]time.Time, error) {
	res := map[string]time.Time{}
	head, err := r.Head()
	if err != nil {
		return nil, err
	}
	if head == "" {
		return res, nil
	}
	args := append([]string{"log", "--format=%x00%cI", "--name-only", r.trackingRef()}, pathspec(dirs)...)
	out, err := r.Git(args...)
	if err != nil {
		return nil, err
	}
	var cur time.Time
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "\x00") {
			cur, _ = time.Parse(time.RFC3339, strings.TrimPrefix(l, "\x00"))
			continue
		}
		if l == "" {
			continue
		}
		if _, seen := res[l]; !seen {
			res[l] = cur
		}
	}
	return res, nil
}

// treeEntry returns the type and object sha of path at commit head, or ""
// when absent. It uses ls-tree, which fails when a tree on the way cannot be
// read, unlike "rev-parse <commit>:<path>", which then exits with 1 and
// nothing on stderr as it does for an absent path.
func (r *Repo) treeEntry(head, path string) (typ, sha string, err error) {
	out, err := r.Git("ls-tree", "-z", head, "--", path)
	if err != nil {
		return "", "", err
	}
	for _, l := range strings.Split(out, "\x00") {
		meta, p, ok := strings.Cut(l, "\t")
		if f := strings.Fields(meta); ok && p == path && len(f) == 3 {
			return f[1], f[2], nil
		}
	}
	return "", "", nil
}

// BlobSHA returns the object sha of path at commit head, or "" when absent.
// A failure of git is an error.
func (r *Repo) BlobSHA(head, path string) (string, error) {
	if head == "" {
		return "", nil
	}
	_, sha, err := r.treeEntry(head, path)
	return sha, err
}

// CheckMissing is called for paths that Cat did not return. It returns an
// error when git cannot tell whether a path exists at the tracking ref, or
// when a path is a blob there that the mirror cannot read; cat-file --batch
// reports both as "missing". A path that does not exist or is not a file is
// not an error, and neither is a blob that can be read, which a fetch made
// after Cat has added.
func (r *Repo) CheckMissing(paths []string) error {
	head, err := r.Head()
	if err != nil || head == "" {
		return err
	}
	for _, p := range paths {
		typ, sha, err := r.treeEntry(head, p)
		if err != nil {
			return err
		}
		if typ != "blob" {
			continue
		}
		if _, err := r.Git("cat-file", "-e", sha); err != nil {
			return fmt.Errorf("cannot read %s (blob %s) from the mirror: %w", p, sha, err)
		}
	}
	return nil
}
