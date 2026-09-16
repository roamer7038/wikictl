package repo

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Change is one file in a commit. Base is the optimistic-lock check used by
// put, mv and rm: nil skips the check, "" requires that the file does not exist yet, and
// any other value must equal the current blob sha.
type Change struct {
	Path    string
	Content []byte
	Delete  bool
	Base    *string
	Mode    string // "" keeps the mode of the file replaced, or 100644 for a new file
}

// Author is used as both author and committer of a commit.
type Author struct{ Name, Email string }

// Conflict is an optimistic-lock failure. Reason is "exists" when a new page
// already exists, or "changed" when the blob sha differs from Base.
type Conflict struct {
	Path    string
	Reason  string
	SHA     string // current blob sha, "" when the file is gone
	Content []byte // current content
}

func (c *Conflict) Error() string { return fmt.Sprintf("conflict(%s): %s", c.Reason, c.Path) }

// Moved is a commit that every attempt failed to push because another push
// moved the remote branch first: the lease of the last attempt was stale.
// Nothing was written, so the caller can run the same command again. A push
// rejected for any other reason is not a Moved, since running it again would
// not help.
type Moved struct {
	Attempts int   // attempts made before giving up
	Err      error // rejection of the last attempt, as git reported it
}

func (e *Moved) Error() string {
	return fmt.Sprintf("conflict(moved): the remote branch moved during %d attempts", e.Attempts)
}

func (e *Moved) Unwrap() error { return e.Err }

// Result is a successful commit.
type Result struct {
	Commit string
	SHAs   map[string]string // blob sha of every written path
}

const zeroSHA = "0000000000000000000000000000000000000000"

// Commit fetches, checks every Base, builds a commit on top of the remote
// branch and pushes it with --force-with-lease. When another push wins the
// race the whole sequence is retried, up to attempts times, after a random
// wait so that writers rejected together do not retry together. When every
// attempt loses the race, the error is a Moved. It never creates a working
// tree or a merge state.
func (r *Repo) Commit(changes []Change, msg string, au Author) (*Result, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	const attempts = 3
	var last error
	var lastRetry retryReason
	var wait time.Duration
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryWait(wait, attempt))
		}
		start := time.Now()
		if err := r.Fetch(); err != nil {
			return nil, err
		}
		head, err := r.trackingHead()
		if err != nil {
			return nil, err
		}
		ents, err := r.entries(head, changes)
		if err != nil {
			return nil, err
		}
		if err := checkReplace(changes, ents); err != nil {
			return nil, err
		}
		for _, c := range changes {
			if c.Base == nil {
				continue
			}
			cur := ents[c.Path].SHA
			if *c.Base == "" && cur != "" {
				return nil, r.conflict(c.Path, "exists", cur)
			} else if *c.Base != "" && cur != *c.Base {
				return nil, r.conflict(c.Path, "changed", cur)
			}
		}
		written := slices.Clone(changes)
		for i, c := range written {
			if c.Mode == "" {
				written[i].Mode = cmp.Or(ents[c.Path].Mode, "100644")
			}
		}
		res, retry, err := r.buildAndPush(head, written, msg, au)
		if err == nil {
			return res, nil
		}
		last, lastRetry = err, retry
		if retry == noRetry {
			return nil, err
		}
		wait = time.Since(start)
	}
	// Only a stale lease is certainly another push winning the race. A ref
	// that could not be locked, or a branch that moved while the push was
	// rejected, can also be a permission, lock file or hook failure that
	// running the command again would not fix, so it stays a git failure.
	if lastRetry == retryStale {
		return nil, &Moved{Attempts: attempts, Err: last}
	}
	return nil, last
}

// PathError is a change that would replace a directory with a file, or write
// a file below a path that is a file.
type PathError struct{ Path, Reason string }

func (e *PathError) Error() string { return e.Path + ": " + e.Reason }

// entries returns the type and object sha at commit head of the path of every
// change and of every directory above a written path, from one "ls-tree" of
// the directories that hold them; ls-tree reads no blob, and fails when a
// tree it lists cannot be read. An absent path has no entry. A directory that
// ls-tree descends into instead of listing is known as a tree by the entries
// below it. A path with a newline or NUL is an error. The CLI rejects such
// paths before, and git takes a newline in the -z input of update-index, so
// the check is defence in depth (#64) against a path adding entries of its
// own to the tree.
func (r *Repo) entries(head string, changes []Change) (map[string]Entry, error) {
	for _, c := range changes {
		if strings.ContainsAny(c.Path, "\n\x00") {
			return nil, fmt.Errorf("path %q contains a newline or NUL", c.Path)
		}
	}
	res := map[string]Entry{}
	if head == "" || len(changes) == 0 {
		return res, nil
	}
	args := []string{"ls-tree", "-z", head, "--"}
	seen := map[string]bool{}
	// list adds the directory that holds p; with the trailing slash ls-tree
	// lists the entries of the directory.
	list := func(p string) {
		d := path.Dir(p)
		if d != "." {
			d += "/"
		}
		if !seen[d] {
			seen[d] = true
			args = append(args, d)
		}
	}
	for _, c := range changes {
		list(c.Path)
		if !c.Delete {
			for d := path.Dir(c.Path); d != "."; d = path.Dir(d) {
				list(d)
			}
		}
	}
	out, err := r.Git(args...)
	if err != nil {
		return nil, err
	}
	for _, e := range parseTree(out) {
		res[e.Path] = e
		for d := path.Dir(e.Path); d != "." && res[d].Type == ""; d = path.Dir(d) {
			res[d] = Entry{Path: d, Type: "tree"}
		}
	}
	return res, nil
}

// checkReplace returns a PathError for a written path that is a directory, a
// submodule or a symbolic link, or that is below a file the changes do not
// delete.
func checkReplace(changes []Change, ents map[string]Entry) error {
	deleted := map[string]bool{}
	for _, c := range changes {
		deleted[c.Path] = deleted[c.Path] || c.Delete
	}
	for _, c := range changes {
		if c.Delete {
			continue
		}
		switch e := ents[c.Path]; {
		case e.Type == "tree":
			return &PathError{c.Path, "is a directory"}
		case e.Type == "commit":
			return &PathError{c.Path, "is a submodule"}
		case e.Mode == "120000":
			return &PathError{c.Path, "is a symbolic link"}
		}
		for d := path.Dir(c.Path); d != "."; d = path.Dir(d) {
			if t := ents[d].Type; t != "" && t != "tree" && !deleted[d] {
				return &PathError{c.Path, d + " is a file"}
			}
		}
	}
	return nil
}

// retryWait returns a random duration in [0, 2^(attempt+3)*d), where d is
// how long the rejected attempt took. Scaling by d keeps the writers spread
// out whether a push takes milliseconds or seconds.
func retryWait(d time.Duration, attempt int) time.Duration {
	n := int64(d) << (attempt + 3)
	if n <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(n))
}

// conflict returns a Conflict with the current content of the page, or the
// error of git when the content cannot be read.
func (r *Repo) conflict(path, reason, sha string) error {
	cf := &Conflict{Path: path, Reason: reason, SHA: sha}
	if sha != "" {
		out, err := r.Git("cat-file", "-p", sha)
		if err != nil {
			return err
		}
		cf.Content = []byte(out)
	}
	return cf
}

// retryReason says whether a failed attempt is worth retrying, and why.
type retryReason int

const (
	noRetry     retryReason = iota // the error is final
	retryStale                     // the lease was stale: another push moved the branch
	retryLocked                    // the ref could not be locked, or the branch moved meanwhile
)

// buildAndPush creates the commit with plumbing commands in a temporary index
// and pushes it. retry says whether the push was rejected because another push
// moved or locked the remote branch, and which of the two. Commit has checked
// the paths in entries.
func (r *Repo) buildAndPush(head string, changes []Change, msg string, au Author) (res *Result, retry retryReason, err error) {
	// The index lives in a new directory inside the mirror, where no other
	// user or process can create or replace it.
	idxDir, err := os.MkdirTemp(r.Dir, "wikictl-index-*")
	if err != nil {
		return nil, noRetry, err
	}
	defer os.RemoveAll(idxDir)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(idxDir, "index"),
		"GIT_AUTHOR_NAME=" + au.Name, "GIT_AUTHOR_EMAIL=" + au.Email,
		"GIT_COMMITTER_NAME=" + au.Name, "GIT_COMMITTER_EMAIL=" + au.Email}
	git := func(stdin []byte, args ...string) (string, error) { return r.runGit(false, env, stdin, args...) }

	if _, err := git(nil, "read-tree", cmp.Or(head, "--empty")); err != nil {
		return nil, noRetry, err
	}
	// The contents are written to files so that one hash-object stores them
	// all; --no-filters stores them as given, whatever the attributes say.
	var files []string
	for i, c := range changes {
		if c.Delete {
			continue
		}
		f := filepath.Join(idxDir, strconv.Itoa(i))
		if err := os.WriteFile(f, c.Content, 0o600); err != nil {
			return nil, noRetry, err
		}
		files = append(files, f)
	}
	var blobs []string
	if len(files) > 0 {
		out, err := git([]byte(strings.Join(files, "\n")+"\n"), "hash-object", "-w", "--no-filters", "--stdin-paths")
		if err != nil {
			return nil, noRetry, err
		}
		if blobs = strings.Fields(out); len(blobs) != len(files) {
			return nil, noRetry, fmt.Errorf("hash-object printed %d object names for %d files", len(blobs), len(files))
		}
	}
	shas := map[string]string{}
	var info bytes.Buffer
	for _, c := range changes {
		if c.Delete {
			// Mode 0 removes the entry; --remove does not work without a working tree.
			fmt.Fprintf(&info, "0 %s\t%s\x00", zeroSHA, c.Path)
			continue
		}
		shas[c.Path], blobs = blobs[0], blobs[1:]
		fmt.Fprintf(&info, "%s %s\t%s\x00", c.Mode, shas[c.Path], c.Path)
	}
	if _, err := git(info.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return nil, noRetry, err
	}
	out, err := git(nil, "write-tree")
	if err != nil {
		return nil, noRetry, err
	}
	tree := strings.TrimSpace(out)
	// A change set that leaves the tree as it is creates no commit.
	if head != "" {
		cur, err := git(nil, "rev-parse", head+"^{tree}")
		if err != nil {
			return nil, noRetry, err
		}
		if strings.TrimSpace(cur) == tree {
			return &Result{Commit: head, SHAs: shas}, noRetry, nil
		}
	}
	// --no-gpg-sign: a commit.gpgsign setting would otherwise start a signing
	// prompt that GIT_TERMINAL_PROMPT=0 does not suppress.
	args := []string{"commit-tree", "--no-gpg-sign", tree, "-m", msg}
	if head != "" {
		args = append(args, "-p", head)
	}
	out, err = git(nil, args...)
	if err != nil {
		return nil, noRetry, err
	}
	commit := strings.TrimSpace(out)
	pout, perr := r.Git("push", "--porcelain", "origin", commit+":refs/heads/"+r.Branch,
		"--force-with-lease=refs/heads/"+r.Branch+":"+cmp.Or(head, zeroSHA))
	switch pushStatus(pout) {
	case pushOK:
		if err := r.updateTrackingRef(head, commit); err != nil {
			return nil, noRetry, fmt.Errorf("pushed %s, but updating %s failed: %w", commit, r.trackingRef(), err)
		}
		return &Result{Commit: commit, SHAs: shas}, noRetry, nil
	case pushStale:
		return nil, retryStale, fmt.Errorf("push rejected: %s", redactText(strings.TrimSpace(pout)))
	default:
		retry := noRetry
		if r.remoteMoved(head, pout, perr) {
			retry = retryLocked
		}
		if perr != nil {
			return nil, retry, perr
		}
		return nil, retry, fmt.Errorf("push failed: %s", strings.TrimSpace(pout))
	}
}

// updateTrackingRef moves the tracking ref from head to the pushed commit. A
// read that fetched after the push may have moved the ref already, so a
// failure is not an error when the ref now contains commit.
func (r *Repo) updateTrackingRef(head, commit string) error {
	args := []string{"update-ref", r.trackingRef(), commit}
	if head != "" {
		args = append(args, head)
	}
	_, err := r.Git(args...)
	if err == nil {
		return nil
	}
	if _, aerr := r.Git("merge-base", "--is-ancestor", commit, r.trackingRef()); aerr == nil {
		return nil
	}
	return err
}

// remoteMoved reports whether a failed push lost a race with another push.
// The server rejects the ref update with "cannot lock ref" when another push
// updated or locked the branch first; that message appears in the porcelain
// line or on stderr depending on the server. Otherwise the branch is fetched
// and compared with head.
func (r *Repo) remoteMoved(head, pout string, perr error) bool {
	msg := pout
	var ge *GitError
	if errors.As(perr, &ge) {
		msg += ge.Stderr
	}
	if strings.Contains(msg, "cannot lock ref") {
		return true
	}
	if r.Fetch() != nil {
		return false
	}
	cur, err := r.trackingHead()
	return err == nil && cur != head
}

// pushResult is the outcome of a push as reported by pushStatus.
type pushResult int

const (
	pushFailed pushResult = iota // rejected for another reason, or no refspec line, e.g. a connection or authentication failure
	pushOK                       // the ref was updated or already up to date
	pushStale                    // the lease failed
)

// pushStatus classifies the refspec line of "push --porcelain" output.
func pushStatus(out string) pushResult {
	for _, l := range strings.Split(out, "\n") {
		if len(l) < 2 || l[1] != '\t' {
			continue
		}
		switch l[0] {
		case ' ', '+', '-', '*', '=':
			return pushOK
		case '!':
			if strings.Contains(l, "stale info") {
				return pushStale
			}
			return pushFailed
		}
	}
	return pushFailed
}
