package repo

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCommitConcurrentMirrors pushes different pages at the same time from
// separate mirrors of one remote. The server rejects the losers of the ref
// update race, and every commit must still land within the retry limit.
func TestCommitConcurrentMirrors(t *testing.T) {
	remote := newRemote(t, true)
	const n = 3
	mirrors := make([]*Repo, n)
	for i := range mirrors {
		mirrors[i] = openFetched(t, remote)
	}
	empty := ""
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, r := range mirrors {
		wg.Add(1)
		go func(i int, r *Repo) {
			defer wg.Done()
			<-start
			path := fmt.Sprintf("global/p%d.md", i)
			_, errs[i] = r.Commit([]Change{{Path: path, Content: []byte("---\nsummary: p\n---\n"), Base: &empty}}, "put "+path, Author{"a", "a@a"})
		}(i, r)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	files := run(t, "", "git", "--git-dir", remote, "ls-tree", "-r", "--name-only", "main")
	for i := 0; i < n; i++ {
		if p := fmt.Sprintf("global/p%d.md", i); !strings.Contains(files, p+"\n") {
			t.Errorf("%s missing from remote:\n%s", p, files)
		}
	}
}

// TestUpdateTrackingRef checks that a failed update of the tracking ref is an
// error only when the ref does not contain the pushed commit.
func TestUpdateTrackingRef(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	old, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, remote, map[string]string{"global/other.md": "---\nsummary: o\n---\n"})
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	cur, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	// The ref is no longer at old, so update-ref fails, but it contains old.
	if err := r.updateTrackingRef(old, old); err != nil {
		t.Errorf("ref containing the commit: %v", err)
	}
	out, err := r.Git("-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "--no-gpg-sign", old+"^{tree}", "-m", "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.updateTrackingRef(old, strings.TrimSpace(out)); err == nil {
		t.Error("ref without the commit: no error")
	}
	if h, _ := r.Head(); h != cur {
		t.Errorf("ref moved to %s, want %s", h, cur)
	}
}

func TestRetryWait(t *testing.T) {
	if w := retryWait(0, 1); w != 0 {
		t.Errorf("retryWait(0, 1) = %v, want 0", w)
	}
	for attempt := 1; attempt < 3; attempt++ {
		limit := 10 * time.Millisecond << (attempt + 3)
		for i := 0; i < 100; i++ {
			if w := retryWait(10*time.Millisecond, attempt); w < 0 || w >= limit {
				t.Fatalf("retryWait(10ms, %d) = %v, want [0, %v)", attempt, w, limit)
			}
		}
	}
}

func TestPushStatus(t *testing.T) {
	cases := map[string]pushResult{
		"To /r.git\n \tabc:refs/heads/main\tdef..123\nDone\n":                                pushOK,
		"To /r.git\n*\tabc:refs/heads/main\t[new branch]\nDone\n":                            pushOK,
		"To /r.git\n!\tabc:refs/heads/main\t[rejected] (stale info)\nDone\n":                 pushStale,
		"To /r.git\n!\tabc:refs/heads/main\t[remote rejected] (pre-receive hook declined)\n": pushRejected,
		"fatal: could not read from remote repository\n":                                     pushNone,
		"": pushNone,
	}
	for in, want := range cases {
		if got := pushStatus(in); got != want {
			t.Errorf("%q: got %d want %d", in, got, want)
		}
	}
}

// TestCommitRejectsPathSeparators checks that a path containing a newline or
// NUL cannot add entries of its own to the tree, such as a symlink or a file
// under .github/workflows/.
func TestCommitRejectsPathSeparators(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	sha := strings.Fields(run(t, "", "git", "--git-dir", remote, "rev-parse", "main:global/index.md"))[0]
	before := run(t, "", "git", "--git-dir", remote, "ls-tree", "-r", "main")
	paths := []string{
		"global/index.md\n120000 " + sha + "\tglobal/link.md\n100644 " + sha + "\t.github/workflows/x.yml",
		"global/index.md\x00100644 " + sha + "\t.github/workflows/x.yml",
	}
	for _, p := range paths {
		for _, c := range []Change{{Path: p, Delete: true}, {Path: p, Content: []byte("x")}} {
			if _, err := r.Commit([]Change{c}, "inject", Author{"a", "a@a"}); err == nil {
				t.Errorf("Commit(%q, delete=%v) succeeded", p, c.Delete)
			}
			if after := run(t, "", "git", "--git-dir", remote, "ls-tree", "-r", "main"); after != before {
				t.Fatalf("Commit(%q, delete=%v) changed the tree:\n%s", p, c.Delete, after)
			}
		}
	}
}

// TestCommitRejectsPathSeparatorsOnEmptyBranch checks that a path containing a
// newline or NUL is rejected when the remote branch does not exist yet.
func TestCommitRejectsPathSeparatorsOnEmptyBranch(t *testing.T) {
	remote := newRemote(t, false)
	r := openFetched(t, remote)
	for _, p := range []string{"a/x\ny.md", "a/x\x00y.md"} {
		for _, c := range []Change{{Path: p, Delete: true}, {Path: p, Content: []byte("x")}} {
			if _, err := r.Commit([]Change{c}, "inject", Author{"a", "a@a"}); err == nil {
				t.Errorf("Commit(%q, delete=%v) succeeded", p, c.Delete)
			}
			if refs := run(t, "", "git", "--git-dir", remote, "for-each-ref"); refs != "" {
				t.Fatalf("Commit(%q, delete=%v) created refs:\n%s", p, c.Delete, refs)
			}
		}
	}
}
