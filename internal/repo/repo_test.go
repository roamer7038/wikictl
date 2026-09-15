package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// newRemote creates a local bare repository to act as the remote. With
// withInitial it is seeded with global/index.md.
func newRemote(t *testing.T, withInitial bool) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	d := t.TempDir()
	remote := filepath.Join(d, "remote.git")
	run(t, "", "git", "init", "-q", "--bare", "-b", "main", remote)
	if withInitial {
		seedRemote(t, remote, map[string]string{"global/index.md": "---\nsummary: entry point\n---\n# global\n"})
	}
	return remote
}

func seedRemote(t *testing.T, remote string, files map[string]string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "w")
	run(t, "", "git", "clone", "-q", remote, work)
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(work, p)), 0o755)
		os.WriteFile(filepath.Join(work, p), []byte(c), 0o644)
	}
	run(t, work, "git", "add", "-A")
	run(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "seed")
	run(t, work, "git", "push", "-q", "origin", "HEAD:main")
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

// TestSnapshot checks that reads after Snapshot keep using the fixed commit
// when a later fetch moves the tracking ref, and that Commit still builds on
// the latest commit of the branch.
func TestSnapshot(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	if err := r.Snapshot(); err != nil {
		t.Fatal(err)
	}
	before, _ := r.Head()
	seedRemote(t, remote, map[string]string{"global/new.md": "---\nsummary: n\n---\n# n\n"})
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if head, err := r.Head(); err != nil || head != before {
		t.Errorf("Head after fetch: %s %v, want %s", head, err, before)
	}
	if list, err := r.List(); err != nil || strings.Contains(strings.Join(list, "\n"), "global/new.md") {
		t.Errorf("List after fetch: %v %v", list, err)
	}
	if c, _, err := r.CatSHA([]string{"global/new.md"}); err != nil || c["global/new.md"] != nil {
		t.Errorf("CatSHA after fetch: %v %v", c, err)
	}
	empty := ""
	if _, err := r.Commit([]Change{{Path: "global/mine.md", Content: []byte("# m\n"), Base: &empty}}, "mine", Author{"a", "a@a"}); err != nil {
		t.Fatal(err)
	}
	tree := run(t, "", "git", "--git-dir", remote, "ls-tree", "-r", "--name-only", "main")
	if !strings.Contains(tree, "global/new.md") || !strings.Contains(tree, "global/mine.md") {
		t.Errorf("Commit must build on the latest commit: %s", tree)
	}
}

// TestSnapshotOfEmptyBranch checks that after Snapshot of a remote without the
// branch, reads find no page even when a later fetch creates the branch.
func TestSnapshotOfEmptyBranch(t *testing.T) {
	remote := newRemote(t, false)
	r := openFetched(t, remote)
	if err := r.Snapshot(); err != nil {
		t.Fatal(err)
	}
	seedRemote(t, remote, map[string]string{"global/new.md": "# n\n"})
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if c, _, err := r.CatSHA([]string{"global/new.md"}); err != nil || len(c) != 0 {
		t.Errorf("CatSHA after fetch: %v %v", c, err)
	}
	if s, err := r.Stat([]string{"global/new.md"}); err != nil || len(s) != 0 {
		t.Errorf("Stat after fetch: %v %v", s, err)
	}
}

func openFetched(t *testing.T, remote string) *Repo {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "m"), remote, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOpenAndFetch(t *testing.T) {
	remote := newRemote(t, true)
	mirror := filepath.Join(t.TempDir(), "m")
	r, err := Open(mirror, remote, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "main" {
		t.Errorf("branch=%q", r.Branch)
	}
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	h, err := r.Head()
	if err != nil || len(h) != 40 {
		t.Fatalf("head=%q err=%v", h, err)
	}
	r2, _ := Open(mirror, remote, "")
	if r2.Branch != "main" {
		t.Error("branch not persisted")
	}
	unlock, err := r.lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestOpenConcurrent(t *testing.T) {
	remote := newRemote(t, true)
	for round := 0; round < 5; round++ {
		mirror := filepath.Join(t.TempDir(), "m")
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := Open(mirror, remote, "")
				if err == nil {
					err = r.Fetch()
				}
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
}

// TestFetchConcurrent fetches the same mirror from several goroutines while
// another clone keeps pushing, so that the fetches update the tracking ref
// from different old values at the same time.
func TestFetchConcurrent(t *testing.T) {
	remote := newRemote(t, true)
	mirror := openFetched(t, remote).Dir
	work := filepath.Join(t.TempDir(), "w")
	run(t, "", "git", "clone", "-q", remote, work)
	done := make(chan struct{})
	pushed := make(chan struct{})
	go func() {
		defer close(pushed)
		for {
			select {
			case <-done:
				return
			default:
			}
			c := exec.Command("sh", "-c", "git -c user.name=t -c user.email=t@t commit -q --allow-empty -m c && git push -q origin HEAD:main")
			c.Dir = work
			if out, err := c.CombinedOutput(); err != nil {
				t.Errorf("push: %v\n%s", err, out)
				return
			}
		}
	}()
	const n, fetches = 8, 20
	errs := make(chan error, n*fetches)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := Open(mirror, remote, "")
			if err != nil {
				errs <- err
				return
			}
			for j := 0; j < fetches; j++ {
				errs <- r.Fetch()
			}
		}()
	}
	wg.Wait()
	close(done)
	<-pushed
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestFetchStaleLockNotRetried checks that a fetch that cannot lock the
// tracking ref because of a stale lock file fails after one git fetch.
func TestFetchStaleLockNotRetried(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	seedRemote(t, remote, map[string]string{"global/new.md": "# n\n"})
	if err := os.WriteFile(filepath.Join(r.Dir, r.trackingRef()+".lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)
	err := r.Fetch()
	if err == nil || !strings.Contains(err.Error(), "cannot lock ref") {
		t.Fatalf("Fetch: %v, want a cannot lock ref error", err)
	}
	b, _ := os.ReadFile(trace)
	if n := strings.Count(string(b), "built-in: git fetch "); n != 1 {
		t.Errorf("git fetch ran %d times, want 1", n)
	}
}

func TestOpenLeftoverDir(t *testing.T) {
	remote := newRemote(t, true)
	empty := filepath.Join(t.TempDir(), "m")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Open(empty, remote, "")
	if err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	for name, setup := range map[string]func(string) error{
		"non-empty dir": func(p string) error {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(p, "stray"), nil, 0o644)
		},
		"file": func(p string) error { return os.WriteFile(p, nil, 0o644) },
	} {
		mirror := filepath.Join(t.TempDir(), "m")
		if err := setup(mirror); err != nil {
			t.Fatal(err)
		}
		_, err := Open(mirror, remote, "")
		if err == nil || !strings.Contains(err.Error(), "is not a git repository; delete it") {
			t.Errorf("%s: err=%v", name, err)
		}
		if name == "non-empty dir" {
			if _, err := os.Stat(filepath.Join(mirror, "stray")); err != nil {
				t.Errorf("stray file must be kept: %v", err)
			}
		}
	}
}

// A mirror whose remote.origin.url is another repository must not be used.
func TestOpenRemoteMismatch(t *testing.T) {
	remote := newRemote(t, true)
	other := newRemote(t, true)
	mirror := filepath.Join(t.TempDir(), "m")
	if _, err := Open(mirror, remote, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(mirror, remote, ""); err != nil {
		t.Errorf("same remote: %v", err)
	}
	_, err := Open(mirror, other, "")
	if err == nil || !strings.Contains(err.Error(), "mirror "+mirror+" is for another repository") {
		t.Errorf("other remote: %v", err)
	}
}

func TestOpenEmptyRemote(t *testing.T) {
	remote := newRemote(t, false)
	r, err := Open(filepath.Join(t.TempDir(), "m"), remote, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if h, err := r.Head(); err != nil || h != "" {
		t.Errorf("empty remote must have no head, got %q, %v", h, err)
	}
}

// TestReadGitFailure reads from a directory that is not a git repository, so
// that every git command fails. No read may report the failure as an empty
// result.
func TestReadGitFailure(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := &Repo{Dir: t.TempDir(), Branch: "main"}
	if h, err := r.Head(); err == nil {
		t.Errorf("Head = %q, nil", h)
	}
	if got, err := r.List(); err == nil {
		t.Errorf("List = %v, nil", got)
	}
	if got, err := r.Grep([]string{"x"}); err == nil {
		t.Errorf("Grep = %v, nil", got)
	}
	if got, err := r.GrepDeprecated(); err == nil {
		t.Errorf("GrepDeprecated = %v, nil", got)
	}
	if got, err := r.Updated([]string{"global/a.md"}); err == nil {
		t.Errorf("Updated = %v, nil", got)
	}
}

// TestCheckMissing checks the paths that are not errors: an absent path, a
// directory and a blob that can be read.
func TestCheckMissing(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	if err := r.CheckMissing([]string{"global/none.md", "none/x.md", "global", "global/index.md"}); err != nil {
		t.Error(err)
	}
	empty := openFetched(t, newRemote(t, false))
	if err := empty.CheckMissing([]string{"global/index.md"}); err != nil {
		t.Error(err)
	}
}

// TestTreeEntries checks that paths too many for one ls-tree call are split
// among several calls, and that an entry is found in the last of them.
func TestTreeEntries(t *testing.T) {
	r := openFetched(t, newRemote(t, true))
	head, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for n := 0; n <= 2*maxTreeArgs; n += len(paths[len(paths)-1]) {
		paths = append(paths, "a/"+strconv.Itoa(len(paths))+".md")
	}
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)
	ents, err := r.treeEntries(head, append(paths, "global/index.md", "global/index.md"))
	if err != nil || len(ents) != 1 || ents["global/index.md"].Type != "blob" {
		t.Errorf("treeEntries = %v, %v", ents, err)
	}
	out, _ := os.ReadFile(trace)
	if n := strings.Count(string(out), " ls-tree -z "); n < 3 {
		t.Errorf("%d ls-tree calls, want at least 3", n)
	}
}

func TestRead(t *testing.T) {
	remote := newRemote(t, true)
	seedRemote(t, remote, map[string]string{
		"global/git-push.md": "---\nsummary: about push\n---\n# push\nforce-with-lease and Lease\n",
		"projects/a/x.md":    "---\nsummary: x\nstatus: deprecated\n---\n# x\nlease\n",
		"machines/h/y.md":    "---\nsummary: y\n---\n# y\nnothing\n",
		"README.md":          "not a page",
		".hidden/z.md":       "---\nsummary: z\n---\n",
		"global/notes.txt":   "lease",
		"global/日本語.md":      "---\nsummary: non-ascii\n---\n# 日本語\nlease\n",
	})
	r := openFetched(t, remote)
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 || list[2] != "global/日本語.md" {
		t.Errorf("list=%v", list)
	}
	got, _ := r.Grep([]string{"LEASE", "force"})
	if len(got) != 1 || got[0] != "global/git-push.md" {
		t.Errorf("all-match grep=%v", got)
	}
	got, _ = r.Grep([]string{"lease"})
	if len(got) != 3 {
		t.Errorf("grep=%v", got)
	}
	if got, _ := r.Grep([]string{"zzz-none"}); len(got) != 0 {
		t.Errorf("no match must be empty: %v", got)
	}
	dep, _ := r.GrepDeprecated()
	if !dep["projects/a/x.md"] || len(dep) != 1 {
		t.Errorf("dep=%v", dep)
	}
	c, shas, _ := r.CatSHA([]string{"global/git-push.md", "missing.md", "machines/h/y.md"})
	if !strings.HasPrefix(string(c["global/git-push.md"]), "---") || c["missing.md"] != nil || c["machines/h/y.md"] == nil {
		t.Errorf("cat=%v", c)
	}
	if len(shas["global/git-push.md"]) != 40 || shas["missing.md"] != "" {
		t.Errorf("shas=%v", shas)
	}
	up, _ := r.Updated([]string{"global/git-push.md", "global/日本語.md"})
	if up["global/git-push.md"].IsZero() || up["global/日本語.md"].IsZero() {
		t.Errorf("updated=%v", up)
	}
}

// TestReadQuotedNames checks the reads of pages whose names git quotes
// unless -z is given: names with a newline, a control character, a double
// quote or a backslash.
func TestReadQuotedNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the file system does not allow these names")
	}
	remote := newRemote(t, true)
	names := []string{"global/a\nb.md", "global/c\x01.md", "global/q\"\\.md"}
	files := map[string]string{}
	for _, n := range names {
		files[n] = "---\nsummary: s\nstatus: deprecated\n---\nlease\n" + n + "\n"
	}
	seedRemote(t, remote, files)
	r := openFetched(t, remote)
	list, err := r.List()
	if want := []string{names[0], names[1], "global/index.md", names[2]}; err != nil || !slices.Equal(list, want) {
		t.Errorf("List = %q, %v", list, err)
	}
	if got, err := r.Grep([]string{"lease"}); err != nil || !slices.Equal(got, names) {
		t.Errorf("Grep = %q, %v", got, err)
	}
	dep, err := r.GrepDeprecated()
	if err != nil || len(dep) != 3 || !dep[names[0]] || !dep[names[1]] || !dep[names[2]] {
		t.Errorf("GrepDeprecated = %v, %v", dep, err)
	}
	paths := append([]string{"global/no\nne.md", "global/a"}, names...)
	paths = append(paths, "global/index.md")
	c, shas, err := r.CatSHA(paths)
	if err != nil || len(c) != 4 || len(shas) != 4 {
		t.Fatalf("CatSHA = %q, %q, %v", c, shas, err)
	}
	for _, n := range names {
		if string(c[n]) != files[n] {
			t.Errorf("CatSHA[%q] = %q", n, c[n])
		}
	}
	objs, err := r.Stat(paths)
	if err != nil || len(objs) != 4 {
		t.Fatalf("Stat = %v, %v", objs, err)
	}
	for _, n := range names {
		if objs[n].SHA != shas[n] || objs[n].Size != int64(len(files[n])) {
			t.Errorf("Stat[%q] = %v", n, objs[n])
		}
	}
}

func TestCatSkipsTrees(t *testing.T) {
	remote := newRemote(t, true)
	seedRemote(t, remote, map[string]string{"global/sub.md/a.md": "---\nsummary: a\n---\n"})
	r := openFetched(t, remote)
	c, _, err := r.CatSHA([]string{"global/sub.md", "global/index.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c["global/sub.md"]; ok || !strings.HasPrefix(string(c["global/index.md"]), "---") {
		t.Errorf("cat=%q", c)
	}
}

func TestCommitAndConflict(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	au := Author{Name: "agent@h", Email: "agent@h.invalid"}
	empty := ""
	res, err := r.Commit([]Change{{Path: "global/a.md", Content: []byte("---\nsummary: a\n---\n# a\n"), Base: &empty}}, "put a", au)
	if err != nil || len(res.Commit) != 40 || len(res.SHAs["global/a.md"]) != 40 {
		t.Fatalf("%+v %v", res, err)
	}
	c, _, _ := r.CatSHA([]string{"global/a.md"})
	if c["global/a.md"] == nil {
		t.Fatal("not readable after commit")
	}
	_, err = r.Commit([]Change{{Path: "global/a.md", Content: []byte("x"), Base: &empty}}, "again", au)
	var cf *Conflict
	if !errors.As(err, &cf) || cf.Reason != "exists" {
		t.Fatalf("want exists conflict, got %v", err)
	}
	old := res.SHAs["global/a.md"]
	if _, err := r.Commit([]Change{{Path: "global/a.md", Content: []byte("---\nsummary: a2\n---\n"), Base: &old}}, "update", au); err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit([]Change{{Path: "global/a.md", Content: []byte("y"), Base: &old}}, "stale", au)
	if !errors.As(err, &cf) || cf.Reason != "changed" || !strings.Contains(string(cf.Content), "a2") {
		t.Fatalf("want changed conflict, got %v", err)
	}
	seedRemote(t, remote, map[string]string{"global/other.md": "---\nsummary: o\n---\n"})
	cur := cf.SHA
	if _, err := r.Commit([]Change{{Path: "global/a.md", Content: []byte("---\nsummary: a3\n---\n"), Base: &cur}}, "retry", au); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := r.CatSHA([]string{"global/other.md"}); c["global/other.md"] == nil {
		t.Error("other.md must survive")
	}
	if _, err := r.Commit([]Change{{Path: "global/a.md", Delete: true}}, "rm", au); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := r.CatSHA([]string{"global/a.md"}); c["global/a.md"] != nil {
		t.Error("not deleted")
	}
	if out, _ := r.Git("log", "-1", "--format=%an <%ae>", r.trackingRef()); strings.TrimSpace(out) != "agent@h <agent@h.invalid>" {
		t.Error(out)
	}
}

func TestCommitEmptyRemote(t *testing.T) {
	remote := newRemote(t, false)
	r := openFetched(t, remote)
	empty := ""
	_, err := r.Commit([]Change{{Path: "global/index.md", Content: []byte("---\nsummary: i\n---\n"), Base: &empty}}, "init", Author{"a", "a@a"})
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := r.Head(); len(h) != 40 {
		t.Error("head not set")
	}
}

// Directory names may contain '*' and '[', so paths must match literally
// rather than as git wildcards.
func TestReadLiteralDirs(t *testing.T) {
	remote := newRemote(t, true)
	seedRemote(t, remote, map[string]string{
		"projects/a*/p.md":   "---\nsummary: p\n---\n# p\nlease\n",
		"projects/app/x.md":  "---\nsummary: x\n---\n# x\nlease\n",
		"projects/[ab]/q.md": "---\nsummary: q\n---\n# q\nlease\n",
		"projects/a/y.md":    "---\nsummary: y\n---\n# y\nlease\n",
	})
	r := openFetched(t, remote)
	for _, tc := range []struct{ dir, want string }{
		{"projects/a*", "projects/a*/p.md"},
		{"projects/[ab]", "projects/[ab]/q.md"},
	} {
		dirs := []string{tc.dir}
		if got, _ := r.Files(dirs); len(got) != 1 || got[0] != tc.want {
			t.Errorf("files %s=%v", tc.dir, got)
		}
		if got, _ := r.GrepRecords([]string{"-l"}, []string{"lease"}, dirs); len(got) != 1 || got[0][0] != tc.want {
			t.Errorf("grep %s=%v", tc.dir, got)
		}
		up, _ := r.Updated([]string{tc.want})
		if len(up) != 1 || up[tc.want].IsZero() {
			t.Errorf("updated %s=%v", tc.dir, up)
		}
	}
	if got, err := r.Files([]string{"nope", "projects/a*"}); err != nil || len(got) != 1 {
		t.Errorf("a directory that does not exist must be ignored: %v %v", got, err)
	}
}

func TestGrepFoldsNonASCII(t *testing.T) {
	remote := newRemote(t, true)
	seedRemote(t, remote, map[string]string{
		"global/apfel.md":  "---\nsummary: Äpfel\n---\n# Äpfel\n",
		"global/kelvin.md": "---\nsummary: 273 K\n---\n",
		"global/sigma.md":  "---\nsummary: ΟΔΟΣ\n---\n",
		"global/a.b.md":    "---\nsummary: a.b [x]\n---\n",
		"global/axb.md":    "---\nsummary: axb x\n---\n",
	})
	r := openFetched(t, remote)
	for _, tc := range []struct {
		words []string
		want  string
	}{
		{[]string{"äpfel"}, "global/apfel.md"},
		{[]string{"ÄPFEL"}, "global/apfel.md"},
		{[]string{"273 k"}, "global/kelvin.md"},
		{[]string{"οδος"}, "global/sigma.md"},
		{[]string{"A.B", "[X]"}, "global/a.b.md"},
	} {
		got, err := r.Grep(tc.words)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Errorf("Grep(%q)=%v, %v; want [%s]", tc.words, got, err, tc.want)
		}
	}
}

func TestReportsError(t *testing.T) {
	for stderr, want := range map[string]bool{
		"": false,
		"warning: unable to access '/home/u/.config/git/attributes': Permission denied\n": false,
		"11:47:25.934953 git.c:463               trace: built-in: git grep -l -e x\n":     false,
		"error: 'main:g/b.md': unable to read debddc32c7a32af3cc2c787797d0d282bcf18d07\n": true,
		"warning: something\nfatal: bad object main\n":                                    true,
		"hint: the error: prefix inside a line is not an error\n":                         false,
		"warning: ignoring broken ref refs/remotes/origin/main\n":                         true,
	} {
		if got := reportsError(stderr); got != want {
			t.Errorf("reportsError(%q) = %v, want %v", stderr, got, want)
		}
	}
}
