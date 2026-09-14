package repo

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
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

// Result is a successful commit.
type Result struct {
	Commit string
	SHAs   map[string]string // blob sha of every written path
}

const zeroSHA = "0000000000000000000000000000000000000000"

// Commit fetches, checks every Base, builds a commit on top of the remote
// branch and pushes it with --force-with-lease. When another push wins the
// race the whole sequence is retried, up to three attempts, after a random
// wait so that writers rejected together do not retry together. It never
// creates a working tree or a merge state.
func (r *Repo) Commit(changes []Change, msg string, au Author) (*Result, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	var last error
	var wait time.Duration
	for attempt := 0; attempt < 3; attempt++ {
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
		shas, err := r.baseSHAs(head, changes)
		if err != nil {
			return nil, err
		}
		for _, c := range changes {
			if c.Base == nil {
				continue
			}
			if cur := shas[c.Path]; *c.Base == "" && cur != "" {
				return nil, r.conflict(c.Path, "exists", cur)
			} else if *c.Base != "" && cur != *c.Base {
				return nil, r.conflict(c.Path, "changed", cur)
			}
		}
		res, retry, err := r.buildAndPush(head, changes, msg, au)
		if err == nil {
			return res, nil
		}
		last = err
		if !retry {
			return nil, err
		}
		wait = time.Since(start)
	}
	return nil, last
}

// baseSHAs returns the blob sha at commit head of each path of the changes
// that have a Base, from one "ls-tree". A path that is absent or not a blob
// has no entry. ls-tree fails when a tree on the way to a path cannot be read.
func (r *Repo) baseSHAs(head string, changes []Change) (map[string]string, error) {
	res := map[string]string{}
	args := []string{"ls-tree", "-z", head, "--"}
	for _, c := range changes {
		if c.Base != nil && !slices.Contains(args[4:], c.Path) {
			args = append(args, c.Path)
		}
	}
	if head == "" || len(args) == 4 {
		return res, nil
	}
	out, err := r.Git(args...)
	if err != nil {
		return nil, err
	}
	for entry := range strings.SplitSeq(out, "\x00") {
		meta, p, ok := strings.Cut(entry, "\t")
		if f := strings.Fields(meta); ok && len(f) == 3 && f[1] == "blob" {
			res[p] = f[2]
		}
	}
	return res, nil
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

// buildAndPush creates the commit with plumbing commands in a temporary index
// and pushes it. retry is true when the push was rejected because another push
// moved or locked the remote branch.
func (r *Repo) buildAndPush(head string, changes []Change, msg string, au Author) (res *Result, retry bool, err error) {
	for _, c := range changes {
		if strings.ContainsAny(c.Path, "\n\x00") {
			return nil, false, fmt.Errorf("path %q contains a newline or NUL", c.Path)
		}
	}
	// The index lives in a new directory inside the mirror, where no other
	// user or process can create or replace it.
	idxDir, err := os.MkdirTemp(r.Dir, "wikictl-index-*")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(idxDir)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(idxDir, "index"),
		"GIT_AUTHOR_NAME=" + au.Name, "GIT_AUTHOR_EMAIL=" + au.Email,
		"GIT_COMMITTER_NAME=" + au.Name, "GIT_COMMITTER_EMAIL=" + au.Email}
	git := func(stdin []byte, args ...string) (string, error) { return r.run(env, stdin, args...) }

	if head != "" {
		if _, err := git(nil, "read-tree", head); err != nil {
			return nil, false, err
		}
	} else if _, err := git(nil, "read-tree", "--empty"); err != nil {
		return nil, false, err
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
			return nil, false, err
		}
		files = append(files, f)
	}
	var blobs []string
	if len(files) > 0 {
		out, err := git([]byte(strings.Join(files, "\n")+"\n"), "hash-object", "-w", "--no-filters", "--stdin-paths")
		if err != nil {
			return nil, false, err
		}
		if blobs = strings.Fields(out); len(blobs) != len(files) {
			return nil, false, fmt.Errorf("hash-object printed %d object names for %d files", len(blobs), len(files))
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
		fmt.Fprintf(&info, "100644 %s\t%s\x00", shas[c.Path], c.Path)
	}
	if _, err := git(info.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return nil, false, err
	}
	out, err := git(nil, "write-tree")
	if err != nil {
		return nil, false, err
	}
	tree := strings.TrimSpace(out)
	// A change set that leaves the tree as it is creates no commit.
	if head != "" {
		cur, err := git(nil, "rev-parse", head+"^{tree}")
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(cur) == tree {
			return &Result{Commit: head, SHAs: shas}, false, nil
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
		return nil, false, err
	}
	commit := strings.TrimSpace(out)
	lease := head
	if lease == "" {
		lease = zeroSHA
	}
	pout, perr := r.Git("push", "--porcelain", "origin", commit+":refs/heads/"+r.Branch,
		"--force-with-lease=refs/heads/"+r.Branch+":"+lease)
	switch pushStatus(pout) {
	case pushOK:
		if err := r.updateTrackingRef(head, commit); err != nil {
			return nil, false, fmt.Errorf("pushed %s, but updating %s failed: %w", commit, r.trackingRef(), err)
		}
		return &Result{Commit: commit, SHAs: shas}, false, nil
	case pushStale:
		return nil, true, fmt.Errorf("push rejected: the remote branch moved")
	default:
		retry := r.remoteMoved(head, pout, perr)
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
	pushNone     pushResult = iota // no refspec line, e.g. a connection or authentication failure
	pushOK                         // the ref was updated or already up to date
	pushStale                      // the lease failed
	pushRejected                   // rejected for another reason
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
			return pushRejected
		}
	}
	return pushNone
}
