package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestOpenEmptyRemote(t *testing.T) {
	remote := newRemote(t, false)
	r, err := Open(filepath.Join(t.TempDir(), "m"), remote, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if h, _ := r.Head(); h != "" {
		t.Errorf("empty remote must have no head, got %q", h)
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
	list, err := r.List([]string{"global", "projects/a", "machines/h", "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 || list[2] != "global/日本語.md" {
		t.Errorf("list=%v", list)
	}
	got, _ := r.Grep([]string{"LEASE", "force"}, true, []string{"global", "projects/a"})
	if len(got) != 1 || got[0] != "global/git-push.md" {
		t.Errorf("all-match grep=%v", got)
	}
	got, _ = r.Grep([]string{"lease"}, true, []string{"global", "projects/a"})
	if len(got) != 3 {
		t.Errorf("grep=%v", got)
	}
	if got, _ := r.Grep([]string{"zzz-none"}, true, nil); len(got) != 0 {
		t.Errorf("no match must be empty: %v", got)
	}
	dep, _ := r.GrepDeprecated([]string{"global", "projects/a"})
	if !dep["projects/a/x.md"] || len(dep) != 1 {
		t.Errorf("dep=%v", dep)
	}
	c, _ := r.Cat([]string{"global/git-push.md", "missing.md", "machines/h/y.md"})
	if !strings.HasPrefix(string(c["global/git-push.md"]), "---") || c["missing.md"] != nil || c["machines/h/y.md"] == nil {
		t.Errorf("cat=%v", c)
	}
	up, _ := r.Updated([]string{"global"})
	if up["global/git-push.md"].IsZero() || up["global/日本語.md"].IsZero() {
		t.Errorf("updated=%v", up)
	}
	h, _ := r.Head()
	sha, _ := r.BlobSHA(h, "global/git-push.md")
	if len(sha) != 40 {
		t.Errorf("sha=%q", sha)
	}
	if s, _ := r.BlobSHA(h, "none.md"); s != "" {
		t.Error("missing must be empty")
	}
}

func TestCatSkipsTrees(t *testing.T) {
	remote := newRemote(t, true)
	seedRemote(t, remote, map[string]string{"global/sub.md/a.md": "---\nsummary: a\n---\n"})
	r := openFetched(t, remote)
	c, err := r.Cat([]string{"global/sub.md", "global/index.md"})
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
	c, _ := r.Cat([]string{"global/a.md"})
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
	if c, _ := r.Cat([]string{"global/other.md"}); c["global/other.md"] == nil {
		t.Error("other.md must survive")
	}
	if _, err := r.Commit([]Change{{Path: "global/a.md", Delete: true}}, "rm", au); err != nil {
		t.Fatal(err)
	}
	if c, _ := r.Cat([]string{"global/a.md"}); c["global/a.md"] != nil {
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

// Directory names may contain '*' and '[', so dirs must match literally
// rather than as git wildcards.
func TestReadLiteralDirs(t *testing.T) {
	remote := newRemote(t, true)
	seedRemote(t, remote, map[string]string{
		"projects/a*/p.md":   "---\nsummary: p\nstatus: deprecated\n---\n# p\nlease\n",
		"projects/app/x.md":  "---\nsummary: x\nstatus: deprecated\n---\n# x\nlease\n",
		"projects/[ab]/q.md": "---\nsummary: q\n---\n# q\nlease\n",
		"projects/a/y.md":    "---\nsummary: y\n---\n# y\nlease\n",
	})
	r := openFetched(t, remote)
	for _, tc := range []struct{ dir, want string }{
		{"projects/a*", "projects/a*/p.md"},
		{"projects/[ab]", "projects/[ab]/q.md"},
	} {
		dirs := []string{tc.dir}
		if got, _ := r.List(dirs); len(got) != 1 || got[0] != tc.want {
			t.Errorf("list %s=%v", tc.dir, got)
		}
		if got, _ := r.Grep([]string{"lease"}, false, dirs); len(got) != 1 || got[0] != tc.want {
			t.Errorf("grep %s=%v", tc.dir, got)
		}
		up, _ := r.Updated(dirs)
		if len(up) != 1 || up[tc.want].IsZero() {
			t.Errorf("updated %s=%v", tc.dir, up)
		}
	}
	if dep, _ := r.GrepDeprecated([]string{"projects/a*"}); len(dep) != 1 || !dep["projects/a*/p.md"] {
		t.Errorf("dep=%v", dep)
	}
}
