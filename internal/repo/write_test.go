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
	cases := map[string]string{
		"To /r.git\n \tabc:refs/heads/main\tdef..123\nDone\n":                                "ok",
		"To /r.git\n*\tabc:refs/heads/main\t[new branch]\nDone\n":                            "ok",
		"To /r.git\n!\tabc:refs/heads/main\t[rejected] (stale info)\nDone\n":                 "stale",
		"To /r.git\n!\tabc:refs/heads/main\t[remote rejected] (pre-receive hook declined)\n": "rejected",
		"fatal: could not read from remote repository\n":                                     "none",
		"": "none",
	}
	for in, want := range cases {
		if got := pushStatus(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
