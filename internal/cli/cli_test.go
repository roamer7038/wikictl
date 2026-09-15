package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// setupEmpty creates, in a new directory, a bare repository remote.git
// without any branch and the configuration config.yaml for it, and returns
// the path of the configuration.
func setupEmpty(t *testing.T) (cfgPath string) {
	t.Helper()
	isolateGit(t)
	t.Setenv("WIKICTL_PROFILE", "")
	d := t.TempDir()
	remote := filepath.Join(d, "remote.git")
	mustRun(t, "", "git", "init", "-q", "--bare", "-b", "main", remote)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(d, "cache"))
	cfgPath = filepath.Join(d, "config.yaml")
	os.WriteFile(cfgPath, []byte("repo: "+remote+"\nauthor: {name: agent, email: a@a}\n"), 0o600)
	return cfgPath
}

// setup creates a wiki as setupEmpty does and pushes four pages to it.
func setup(t *testing.T) (cfgPath string) {
	t.Helper()
	cfgPath = setupEmpty(t)
	pushFiles(t, cfgPath, map[string]string{
		"global/index.md":   "---\nsummary: entry point\n---\n# global\n",
		"global/push.md":    "---\nsummary: how to push\ntype: policy\ntags: [git]\n---\n# push\nuse force-with-lease. [index](index.md)\n\n## Links\n- part_of: [index](index.md)\n",
		"projects/app/x.md": "---\nsummary: x\n---\n# x\nlease\n",
		"machines/h1/y.md":  "---\nsummary: y\nstatus: deprecated\n---\n# y\nlease\n",
	})
	return cfgPath
}

// cloneRemote clones the repository of a configuration that setupEmpty
// created into a new directory and returns the path of the clone.
func cloneRemote(t *testing.T, cfg string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "w")
	mustRun(t, "", "git", "clone", "-q", filepath.Join(filepath.Dir(cfg), "remote.git"), work)
	return work
}

// pushFiles writes files, keyed by path, in a clone of the repository of cfg
// and pushes them to main in one commit.
func pushFiles(t *testing.T, cfg string, files map[string]string) {
	t.Helper()
	work := cloneRemote(t, cfg)
	for p, c := range files {
		f := filepath.Join(work, p)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, work, "git", "add", "-A")
	commitAndPush(t, work)
}

// commitAndPush commits the index of the clone work and pushes it to main.
func commitAndPush(t *testing.T, work string) {
	t.Helper()
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "test")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func runCLI(t *testing.T, cfg string, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Main(append([]string{"--config", cfg}, args...), strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

// mustUnmarshal decodes the JSON output of a command into v and stops the test
// when out is not valid JSON.
func mustUnmarshal(t *testing.T, out string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
}

func TestReadCommands(t *testing.T) {
	cfg := setup(t)
	code, out, errs := runCLI(t, cfg, "", "grep", "lease")
	if code != 0 || out != "global/push.md:use force-with-lease. [index](index.md)\nmachines/h1/y.md:lease\nprojects/app/x.md:lease\n" {
		t.Errorf("grep: code=%d out=%q errs=%q", code, out, errs)
	}
	var res struct {
		Items []struct {
			Path, Text string
			Line       int
		}
	}
	_, out, _ = runCLI(t, cfg, "", "grep", "--json", "-w", "lease", "projects")
	mustUnmarshal(t, out, &res)
	if len(res.Items) != 1 || res.Items[0].Path != "projects/app/x.md" || res.Items[0].Line != 5 || res.Items[0].Text != "lease" {
		t.Errorf("grep --json: %s", out)
	}
	for args, want := range map[string]string{
		"-in LEASE projects":                  "projects/app/x.md:5:lease\n",
		"-h lease projects":                   "lease\n",
		"-hn lease projects":                  "5:lease\n",
		"-hc lease global":                    "1\n",
		"-hl lease global":                    "global/push.md\n",
		"--no-filename -i LEASE projects":     "lease\n",
		"-hi LEASE projects":                  "lease\n",
		"-hL lease global":                    "global/index.md\n",
		"-hvn lease projects":                 "1:---\n2:summary: x\n3:---\n4:# x\n",
		"-hq lease":                           "",
		"-c lease global":                     "global/push.md:1\n",
		"-L lease global":                     "global/index.md\n",
		"-v -c lease projects":                "projects/app/x.md:4\n",
		"-l --all-match -e lease -e summary:": "global/push.md\nmachines/h1/y.md\nprojects/app/x.md\n",
		"-lE lease|entry global":              "global/index.md\nglobal/push.md\n",
		"-lF [index] global":                  "global/push.md\n",
		"-l -e with-lease -e point /global/":  "global/index.md\nglobal/push.md\n",
		"-q lease":                            "",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"grep"}, strings.Fields(args)...)...); code != 0 || out != want {
			t.Errorf("grep %s: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
	for _, mode := range []string{"", "-n", "-c"} {
		args := append(strings.Fields(mode), "--json", "lease", "global", "projects")
		_, want, _ := runCLI(t, cfg, "", append([]string{"grep"}, args...)...)
		if _, got, _ := runCLI(t, cfg, "", append([]string{"grep", "-h"}, args...)...); got != want || !strings.Contains(got, `"path"`) {
			t.Errorf("grep -h %v: got %q, want %q", args, got, want)
		}
	}
	if code, out, _ := runCLI(t, cfg, "", "grep", "-q", "zzz-none"); code != ExitError || out != "" {
		t.Errorf("grep -q without a match: code=%d out=%q", code, out)
	}
	if code, out, errs := runCLI(t, cfg, "", "grep", "lease", "global/none", "projects"); code != ExitUsage ||
		out != "projects/app/x.md:lease\n" || errs != "wikictl: global/none: no such file or directory\n" {
		t.Errorf("grep with a missing path: code=%d out=%q errs=%q", code, out, errs)
	}
	code, out, _ = runCLI(t, cfg, "", "stat", "--json", "global/push.md")
	var st struct {
		Items []struct {
			Sha, Title, Summary, Type string
			Tags                      []string
		}
	}
	mustUnmarshal(t, out, &st)
	if code != 0 || len(st.Items) != 1 || len(st.Items[0].Sha) != 40 || st.Items[0].Title != "push" || st.Items[0].Summary != "how to push" ||
		st.Items[0].Type != "policy" || !slices.Equal(st.Items[0].Tags, []string{"git"}) {
		t.Errorf("stat: %s", out)
	}
	if code, out, _ = runCLI(t, cfg, "", "cat", "global/push.md", "global/index.md"); code != 0 ||
		out != "---\nsummary: how to push\ntype: policy\ntags: [git]\n---\n# push\nuse force-with-lease. [index](index.md)\n\n## Links\n- part_of: [index](index.md)\n---\nsummary: entry point\n---\n# global\n" {
		t.Errorf("cat: code=%d %q", code, out)
	}
	if _, out, _ = runCLI(t, cfg, "", "links", "global/push.md"); out != "out\tpart_of\tglobal/index.md\n" {
		t.Errorf("links of push.md: %q", out)
	}
	if _, out, _ = runCLI(t, cfg, "", "links", "global/index.md"); out != "in\tpart_of\tglobal/push.md\n" {
		t.Errorf("links of index.md: %q", out)
	}
	if _, out, _ = runCLI(t, cfg, "", "links", "-o", "global/index.md"); out != "" {
		t.Errorf("links -o of index.md: %q", out)
	}
	code, out, errs = runCLI(t, cfg, "", "cat", "global/none.md", "global/index.md", "global")
	if code != 1 || out != "---\nsummary: entry point\n---\n# global\n" || errs != "wikictl: global/none.md: no such file or directory\nwikictl: global: is a directory\n" {
		t.Errorf("cat with missing paths: code=%d out=%q errs=%q", code, out, errs)
	}
	if _, out, _ = runCLI(t, cfg, "", "ls"); out != "global/\nmachines/\nprojects/\n" {
		t.Errorf("ls: %q", out)
	}
	if _, out, _ = runCLI(t, cfg, "", "ls", "global", "projects/app/x.md"); out != "projects/app/x.md\n\nglobal:\nindex.md\npush.md\n" {
		t.Errorf("ls of a file and a directory: %q", out)
	}
	if _, out, _ = runCLI(t, cfg, "", "ls", "projects", "global"); out != "global:\nindex.md\npush.md\n\nprojects:\napp/\n" {
		t.Errorf("ls must sort its arguments: %q", out)
	}
	if code, out, _ = runCLI(t, cfg, "", "ls", "none", "projects"); code != 1 || out != "projects:\napp/\n" {
		t.Errorf("ls with a missing path must keep the heading: code=%d %q", code, out)
	}
	if _, out, _ = runCLI(t, cfg, "", "ls", "-R", "machines"); out != "machines:\nh1/\n\nmachines/h1:\n" {
		t.Errorf("ls -R must hide the deprecated page: %q", out)
	}
	if _, out, _ = runCLI(t, cfg, "", "ls", "-Ra", "machines"); out != "machines:\nh1/\n\nmachines/h1:\ny.md\n" {
		t.Errorf("ls -Ra: %q", out)
	}
	_, out, _ = runCLI(t, cfg, "", "ls", "-l", "global")
	var cols [][]string
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		cols = append(cols, strings.Fields(l))
	}
	if len(cols) != 2 || len(cols[0]) < 4 || len(cols[1]) < 4 ||
		cols[0][0] != "-" || cols[0][2] != "index.md" || strings.Join(cols[0][3:], " ") != "entry point" ||
		cols[1][0] != "policy" || cols[1][2] != "push.md" || strings.Join(cols[1][3:], " ") != "how to push" {
		t.Errorf("ls -l: %q", out)
	}
	var ls struct {
		Items []struct{ Path, Kind string }
	}
	_, out, _ = runCLI(t, cfg, "", "ls", "-R", "--json")
	mustUnmarshal(t, out, &ls)
	var entries []string
	for _, it := range ls.Items {
		entries = append(entries, it.Kind+" "+it.Path)
	}
	if want := []string{"dir global", "dir machines", "dir projects", "file global/index.md", "file global/push.md", "dir machines/h1", "dir projects/app", "file projects/app/x.md"}; !slices.Equal(entries, want) {
		t.Errorf("ls -R --json: %v", entries)
	}
	if code, out, _ = runCLI(t, cfg, "", "context", "--json"); code != 0 {
		t.Errorf("context: %s", out)
	}
	// Global flags may follow the command; "--" ends flag parsing.
	if code, out, _ := runCLI(t, cfg, "", "lint", "--", "global/push.md"); code != 0 || out != "" {
		t.Errorf("lint -- path: code=%d out=%q", code, out)
	}
}

// TestDeprecatedStatus checks that ls and tree hide a page by the status in
// its frontmatter, whether quoted or not, and not by a line in its body, and
// that grep searches every page.
func TestDeprecatedStatus(t *testing.T) {
	cfg := setup(t)
	for p, c := range map[string]string{
		"global/howto.md":  "---\nsummary: howto\n---\n# howto\n```yaml\nstatus: deprecated\n```\nlease\n",
		"global/quoted.md": "---\nsummary: quoted\nstatus: \"deprecated\"\n---\n# quoted\nlease\n",
	} {
		if code, _, errs := runCLI(t, cfg, c, "put", p); code != 0 {
			t.Fatalf("put %s: code=%d %s", p, code, errs)
		}
	}
	check := func(args []string, shown, hidden []string) {
		t.Helper()
		_, out, _ := runCLI(t, cfg, "", args...)
		for _, s := range shown {
			if !strings.Contains(out, s) {
				t.Errorf("%v must list %s: %q", args, s, out)
			}
		}
		for _, s := range hidden {
			if strings.Contains(out, s) {
				t.Errorf("%v must hide %s: %q", args, s, out)
			}
		}
	}
	check([]string{"grep", "-l", "lease"}, []string{"global/howto.md", "global/quoted.md", "machines/h1/y.md"}, nil)
	check([]string{"ls", "-R"}, []string{"howto.md"}, []string{"quoted.md", "y.md"})
	check([]string{"ls", "-Ra"}, []string{"quoted.md", "y.md"}, nil)
	check([]string{"tree"}, []string{"howto.md"}, []string{"quoted.md", "y.md"})
	check([]string{"tree", "-a"}, []string{"quoted.md", "y.md"}, nil)
}

// TestBinaryPage checks that a page with a NUL, which git grep -I takes as
// binary, is still found when links -i and ls search for candidates, while
// grep skips it.
func TestBinaryPage(t *testing.T) {
	cfg := setup(t)
	for p, c := range map[string]string{
		"global/bin.md": "---\nsummary: bin\n---\n# bin\n\x00 lease [index](index.md)\n",
		"global/old.md": "---\nsummary: old\nstatus: deprecated\n---\n# old\n\x00\n",
	} {
		if code, _, errs := runCLI(t, cfg, c, "put", p); code != 0 {
			t.Fatalf("put %s: code=%d %s", p, code, errs)
		}
	}
	if _, out, _ := runCLI(t, cfg, "", "links", "-i", "global/index.md"); !strings.Contains(out, "global/bin.md") {
		t.Errorf("links -i must list global/bin.md: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "ls", "global"); !strings.Contains(out, "bin.md") || strings.Contains(out, "old.md") {
		t.Errorf("ls must list bin.md and hide old.md: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "grep", "-l", "lease"); strings.Contains(out, "bin.md") {
		t.Errorf("grep must skip bin.md: %q", out)
	}
}

func TestPutRm(t *testing.T) {
	cfg := setup(t)
	code, out, _ := runCLI(t, cfg, "---\nsummary: new page\n---\n# n\n", "put", "--json", "global/new.md")
	var res struct{ Path, Sha, Commit string }
	mustUnmarshal(t, out, &res)
	if code != 0 || len(res.Sha) != 40 {
		t.Fatalf("code=%d %s", code, out)
	}
	code, out, _ = runCLI(t, cfg, "---\nsummary: x\n---\n", "put", "--json", "global/new.md")
	var cf struct{ Error, Reason, Sha, Content, Message string }
	mustUnmarshal(t, out, &cf)
	if code != 3 || cf.Error != "conflict" || cf.Reason != "exists" || cf.Sha != res.Sha || !strings.Contains(cf.Message, "already exists") {
		t.Errorf("code=%d %s", code, out)
	}
	code, out, _ = runCLI(t, cfg, "---\nsummary: updated page\n---\n# n2\n", "put", "--json", "--base", res.Sha, "global/new.md")
	if code != 0 {
		t.Fatalf("%s", out)
	}
	code, out, _ = runCLI(t, cfg, "---\nsummary: z\n---\n", "put", "--json", "--base", res.Sha, "global/new.md")
	mustUnmarshal(t, out, &cf)
	if code != 3 || cf.Reason != "changed" || !strings.Contains(cf.Content, "updated page") || !strings.Contains(cf.Message, "changed since it was read") {
		t.Errorf("code=%d %s", code, out)
	}
	// A missing summary, or no frontmatter at all, is only a warning; the write goes through.
	if code, _, errs := runCLI(t, cfg, "# no fm\n", "put", "global/nofm.md"); code != 0 || !strings.Contains(errs, "missing_summary") {
		t.Errorf("no frontmatter: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\ntype: note\n---\n# no summary\n", "put", "global/nosum.md"); code != 0 || !strings.Contains(errs, "missing_summary") {
		t.Errorf("no summary: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "---\nsummary: [\n---\n", "put", "global/bad.md"); code != 4 {
		t.Errorf("invalid frontmatter code=%d", code)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n", "put", "global/my page.md"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("bad path: code=%d errs=%q", code, errs)
	}
	// A name outside the recommended form is only a warning.
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n", "put", "global/Bad_Name.md"); code != 0 || !strings.Contains(errs, "name_style") {
		t.Errorf("style name: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n", "put", "global/日本語.md"); code != 0 || !strings.Contains(errs, "name_style") {
		t.Errorf("non-ascii name: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/日本語.md"); code != 0 {
		t.Errorf("non-ascii cat code=%d", code)
	}
	// A broken link is only a warning; the write goes through.
	code, _, errs := runCLI(t, cfg, "---\nsummary: w\n---\n# w\n[g](gone.md)\n", "put", "global/warn.md")
	if code != 0 || !strings.Contains(errs, "warning") {
		t.Errorf("code=%d errs=%q", code, errs)
	}
	if code, _, _ = runCLI(t, cfg, "", "rm", "global/new.md"); code != 0 {
		t.Error("rm failed")
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/new.md"); code != 1 {
		t.Error("still exists")
	}
	code, out, _ = runCLI(t, cfg, "---\nsummary: z\n---\n", "put", "--json", "--base", res.Sha, "global/new.md")
	cf = struct{ Error, Reason, Sha, Content, Message string }{}
	mustUnmarshal(t, out, &cf)
	if code != 3 || cf.Reason != "changed" || cf.Sha != "" || !strings.Contains(cf.Message, "deleted since it was read") {
		t.Errorf("deleted: code=%d %s", code, out)
	}
}

// TestRmRejectsBadPath checks that rm refuses paths that are not page paths,
// including a path whose newline would add entries to the tree.
func TestRmRejectsBadPath(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	pushFiles(t, cfg, map[string]string{"README.md": "# wiki\n"})
	lsTree := func() string {
		out, err := exec.Command("git", "--git-dir", remote, "ls-tree", "-r", "main").Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	before := lsTree()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "main:global/index.md").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	for _, p := range []string{
		"global/index.md\n120000 " + sha + "\tglobal/link.md\n100644 " + sha + "\t.github/workflows/x.yml",
		"global/index.md\x00",
		"global/.hidden.md",
	} {
		if code, _, errs := runCLI(t, cfg, "", "rm", p); code != 4 || !strings.Contains(errs, "bad_path") {
			t.Errorf("rm %q: code=%d errs=%q", p, code, errs)
		}
	}
	if after := lsTree(); after != before {
		t.Errorf("rm changed the tree:\n%s", after)
	}
}

// isolateGit keeps the user's global git configuration (hooks, gpgsign,
// templates) out of the repositories the tests create.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// TestPathForms checks that a leading "/" or "./" and a trailing "/" in a path
// argument make no difference, and that a path outside the wiki is rejected.
func TestPathForms(t *testing.T) {
	cfg := setup(t)
	var st struct{ Items []struct{ Path string } }
	for _, p := range []string{"/global/push.md", "./global/push.md", "global//push.md"} {
		code, out, errs := runCLI(t, cfg, "", "stat", "--json", p)
		if code != 0 {
			t.Fatalf("stat %s: code=%d %s", p, code, errs)
		}
		if mustUnmarshal(t, out, &st); len(st.Items) != 1 || st.Items[0].Path != "global/push.md" {
			t.Errorf("stat %s: %s", p, out)
		}
	}
	if code, out, _ := runCLI(t, cfg, "", "ls", "--json", "/projects/"); code != 0 || !strings.Contains(out, `"path":"projects/app"`) {
		t.Errorf("ls /projects/: code=%d out=%s", code, out)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: s\n---\n# s\n", "put", "/global/slash.md"); code != 0 {
		t.Fatalf("put /global/slash.md: code=%d %s", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/slash.md"); code != 0 {
		t.Error("put with a leading / must write global/slash.md")
	}
	for _, args := range [][]string{{"cat", "../global/push.md"}, {"rm", "/../x.md"}, {"lint", "global/../../x.md"}, {"ls", ".."}, {"mv", "global/push.md", "../push.md"}} {
		if code, _, errs := runCLI(t, cfg, "", args...); code != ExitInvalid || !strings.Contains(errs, "outside the wiki") {
			t.Errorf("%v: code=%d errs=%q", args, code, errs)
		}
	}
	if code, out, errs := runCLI(t, cfg, "", "cat", "global/push.md\nglobal/index.md"); code != ExitInvalid || out != "" || !strings.Contains(errs, "bad_path") {
		t.Errorf("get with a newline in the path: code=%d out=%q errs=%q", code, out, errs)
	}
}

// TestPutIntoEmptyRepository checks that the first put into a repository
// without any branch creates the branch, and that reads work before and after.
func TestPutIntoEmptyRepository(t *testing.T) {
	cfg := setupEmpty(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	if code, out, errs := runCLI(t, cfg, "", "ls", "--json"); code != 0 || out != `{"items":[]}`+"\n" {
		t.Fatalf("ls before the first put: code=%d out=%q %s", code, out, errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n# a\n", "put", "global/a.md"); code != 0 {
		t.Fatalf("first put: code=%d %s", code, errs)
	}
	if got := gitOut(t, "--git-dir", remote, "ls-tree", "-r", "--name-only", "main"); got != "global/a.md" {
		t.Errorf("tree of main: %q", got)
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/a.md"); code != 0 {
		t.Error("cat after the first put failed")
	}
}

func TestMvAndLint(t *testing.T) {
	cfg := setup(t)
	code, out, errs := runCLI(t, cfg, "", "mv", "--json", "global/push.md", "projects/app/push-notes.md")
	if code != 0 {
		t.Fatalf("mv code=%d %s %s", code, out, errs)
	}
	_, out, _ = runCLI(t, cfg, "", "cat", "projects/app/push-notes.md")
	if !strings.Contains(out, "[index](../../global/index.md)\n") || !strings.Contains(out, "- part_of: [index](../../global/index.md)\n") {
		t.Errorf("self links not rewritten: %q", out)
	}
	if _, out, _ = runCLI(t, cfg, "", "stat", "--json", "projects/app/push-notes.md"); !strings.Contains(out, `"aliases":["push"]`) {
		t.Errorf("aliases: %s", out)
	}
	runCLI(t, cfg, "---\nsummary: x\n---\n# x\n[p](push-notes.md)\n", "put", "--base", shaOf(t, cfg, "projects/app/x.md"), "projects/app/x.md")
	if code, _, e := runCLI(t, cfg, "", "mv", "projects/app/push-notes.md", "global/push.md"); code != 0 {
		t.Fatalf("mv back: %s", e)
	}
	_, out, _ = runCLI(t, cfg, "", "cat", "projects/app/x.md")
	if !strings.Contains(out, "](../../global/push.md)") {
		t.Errorf("referrer not rewritten: %s", out)
	}
	if code, _, _ := runCLI(t, cfg, "", "mv", "global/push.md", "global/index.md"); code != 1 {
		t.Error("mv onto existing must fail")
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/push.md", "global/a:b.md"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv to name with ':': code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/push.md", "global/Push.md"); code != 0 || !strings.Contains(errs, "name_style") {
		t.Errorf("mv to style name: code=%d errs=%q", code, errs)
	}
	runCLI(t, cfg, "", "mv", "global/Push.md", "global/push.md")
	runCLI(t, cfg, "---\nsummary: b\n---\n# b\n[gone](gone.md)\n\n## Links\n- x\n* see_also: [i](index.md)\n  + index.md | untyped\n", "put", "global/broken.md")
	_, out, _ = runCLI(t, cfg, "", "links", "--json", "-o", "global/broken.md")
	var b struct {
		Items []struct{ Direction, Type, Target, Note string }
	}
	mustUnmarshal(t, out, &b)
	if len(b.Items) != 3 || b.Items[0].Type != "see_also" || b.Items[1].Type != "see_also" || b.Items[1].Target != "global/index.md" || b.Items[1].Note != "untyped" ||
		b.Items[2].Type != "mentions" || b.Items[2].Target != "global/gone.md" || b.Items[2].Direction != "out" {
		t.Errorf("tolerant links: %s", out)
	}
	code, out, _ = runCLI(t, cfg, "", "lint", "--json")
	var li struct {
		Items []struct {
			Path, Code string
			Line       int
		}
	}
	mustUnmarshal(t, out, &li)
	codes := map[string]int{}
	for _, it := range li.Items {
		codes[it.Code]++
	}
	if code != 4 || codes["broken_link"] != 1 || codes["links_syntax"] != 1 {
		t.Errorf("code=%d %s", code, out)
	}
	if code, _, _ := runCLI(t, cfg, "", "lint", "projects/app/x.md"); code != 0 {
		t.Error("clean page must pass")
	}
}

// TestMvKeepsLineEndings checks that the pages mv rewrites keep their CRLF
// line endings and BOM.
func TestMvKeepsLineEndings(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	if code, _, errs := runCLI(t, cfg, "\xef\xbb\xbf---\r\nsummary: c\r\n---\r\n# c\r\n[i](index.md)\r\n", "put", "global/crlf.md"); code != 0 {
		t.Fatalf("put: %s", errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\r\nsummary: r\r\n---\r\n# r\r\n[c](crlf.md)\r\n", "put", "global/ref.md"); code != 0 {
		t.Fatalf("put: %s", errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/crlf.md", "projects/app/crlf2.md"); code != 0 {
		t.Fatalf("mv: %s", errs)
	}
	for p, want := range map[string]string{
		"projects/app/crlf2.md": "\xef\xbb\xbf---\r\nsummary: c\r\naliases:\r\n  - crlf\r\n---\r\n# c\r\n[i](../../global/index.md)",
		"global/ref.md":         "---\r\nsummary: r\r\n---\r\n# r\r\n[c](../projects/app/crlf2.md)",
	} {
		if got := gitOut(t, "--git-dir", remote, "show", "main:"+p); got != want {
			t.Errorf("%s: %q", p, got)
		}
	}
}

// TestLintDirs checks that lint checks the pages under a directory
// recursively, and each page once when the arguments overlap.
func TestLintDirs(t *testing.T) {
	cfg := setup(t)
	runCLI(t, cfg, "# a\n", "put", "global/sub/a.md")
	runCLI(t, cfg, "# b\n", "put", "projects/app/b.md")
	runCLI(t, cfg, "png", "put", "machines/raw/img.png")
	runCLI(t, cfg, "---\nsummary: in\n---\n# in\n", "put", "notes/x.md/in.md")
	for _, c := range []struct {
		args []string
		code int
		out  string
	}{
		{[]string{"global"}, 4, "global/sub/a.md:1: missing_summary: frontmatter is missing\n"},
		{[]string{"/global/", "projects/app/x.md"}, 4, "global/sub/a.md:1: missing_summary: frontmatter is missing\n"},
		{[]string{"global", "global/sub/a.md"}, 4, "global/sub/a.md:1: missing_summary: frontmatter is missing\n"},
		{[]string{"projects", "global/push.md"}, 4, "projects/app/b.md:1: missing_summary: frontmatter is missing\n"},
		{[]string{"global/push.md", "global/push.md"}, 0, ""},
		{[]string{"global/sub/a.md", "global/sub/a.md"}, 4, "global/sub/a.md:1: missing_summary: frontmatter is missing\n"},
		{[]string{"."}, 4, "global/sub/a.md:1: missing_summary: frontmatter is missing\nnotes/x.md/in.md:0: name_style: name \"x.md\": lowercase ASCII letters, digits and hyphens are recommended\nprojects/app/b.md:1: missing_summary: frontmatter is missing\n"},
		{[]string{"notes/x.md"}, 4, "notes/x.md/in.md:0: name_style: name \"x.md\": lowercase ASCII letters, digits and hyphens are recommended\n"},
		{[]string{"machines"}, 0, ""},
		{[]string{"machines/raw"}, 0, ""},
	} {
		if code, out, _ := runCLI(t, cfg, "", append([]string{"lint"}, c.args...)...); code != c.code || out != c.out {
			t.Errorf("lint %v: code=%d out=%q", c.args, code, out)
		}
	}
}

func TestLintNames(t *testing.T) {
	cfg := setup(t)
	runCLI(t, cfg, "---\nsummary: i\n---\n", "put", "global/Index.md")
	runCLI(t, cfg, "---\nsummary: s\n---\n", "put", "global/style_name.md")
	runCLI(t, cfg, "---\nsummary: d\n---\n", "put", "projects/App/d.md")
	code, out, _ := runCLI(t, cfg, "", "lint", "--json")
	var li struct {
		Items []struct {
			Path, Code, Message string
		}
	}
	mustUnmarshal(t, out, &li)
	got := map[string]string{}
	for _, it := range li.Items {
		got[it.Path+" "+it.Code] = it.Message
	}
	want := []string{"global/Index.md case_collision", "global/index.md case_collision", "global/Index.md name_style", "global/style_name.md name_style", "projects/App/d.md case_collision", "projects/app/x.md case_collision", "projects/App/d.md name_style"}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("missing %q in %s", w, out)
		}
	}
	if code != 4 || len(li.Items) != len(want) {
		t.Errorf("code=%d items=%d %s", code, len(li.Items), out)
	}
	// Explicit paths report only their own issues, but collisions are still found against the whole tree.
	code, out, _ = runCLI(t, cfg, "", "lint", "global/index.md")
	if code != 4 || !strings.Contains(out, "global/index.md:0: case_collision") || strings.Contains(out, "Index.md:0") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

// shaOf returns the blob sha of p as stat prints it.
func shaOf(t *testing.T, cfg, p string) string {
	t.Helper()
	_, out, _ := runCLI(t, cfg, "", "stat", "--json", p)
	var st struct{ Items []struct{ Sha string } }
	mustUnmarshal(t, out, &st)
	if len(st.Items) != 1 {
		t.Fatalf("stat %s: %s", p, out)
	}
	return st.Items[0].Sha
}

func TestMvDir(t *testing.T) {
	cfg := setup(t)
	// Set up x.md linking to global/index.md and global/ref.md linking to x.md.
	runCLI(t, cfg, "---\nsummary: x\n---\n# x\n[i](../../global/index.md)\n", "put", "--base", shaOf(t, cfg, "projects/app/x.md"), "projects/app/x.md")
	runCLI(t, cfg, "---\nsummary: z\n---\n# z\n[x](x.md)\n", "put", "projects/app/z.md")
	runCLI(t, cfg, "---\nsummary: ref\n---\n# ref\n\n## Links\n- see_also: [x](../projects/app/x.md)\n", "put", "global/ref.md")
	code, out, errs := runCLI(t, cfg, "", "mv", "--json", "projects/app/", "projects/app2/")
	if code != 0 {
		t.Fatalf("code=%d %s %s", code, out, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "projects/app/x.md"); code != 1 {
		t.Error("old path must be gone")
	}
	_, out, _ = runCLI(t, cfg, "", "cat", "projects/app2/x.md")
	if !strings.Contains(out, "](../../global/index.md)") {
		t.Errorf("moved page self link: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "cat", "projects/app2/z.md")
	if !strings.Contains(out, "](x.md)") {
		t.Errorf("intra-dir link must stay relative: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "links", "--json", "global/ref.md")
	if !strings.Contains(out, `"target":"projects/app2/x.md"`) {
		t.Errorf("external referrer: %s", out)
	}
	if code, _, _ := runCLI(t, cfg, "", "lint"); code != 0 {
		t.Error("lint must pass after dir mv")
	}
	// A directory given as the destination receives the source, and a path that exists there is not replaced.
	runCLI(t, cfg, "---\nsummary: g\n---\n# g\n", "put", "global/app2/x.md")
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/app2", "global"); code != 1 || errs != "wikictl: global/app2: not replacing\n" {
		t.Errorf("mv onto an existing directory: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/app2/", "projects/new dir/"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv dir to bad name: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/app2", "projects/App3"); code != 0 || !strings.Contains(errs, "name_style") {
		t.Errorf("mv dir to style name: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/App3/", "projects/app2/"); code != 0 {
		t.Errorf("mv dir back: code=%d errs=%q", code, errs)
	}
	for _, from := range []string{"./", "../", "/", "projects/../"} {
		if code, _, errs := runCLI(t, cfg, "", "mv", from, "x/"); code != 4 || !strings.Contains(errs, "bad_path") {
			t.Errorf("mv %s x/ must be rejected: code=%d errs=%q", from, code, errs)
		}
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/index.md"); code != 0 {
		t.Error("rejected dir mv must not move pages")
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/index.md/", "projects"); code != 1 || !strings.Contains(errs, "global/index.md/: not a directory") {
		t.Errorf("mv of a file given as a directory: code=%d errs=%q", code, errs)
	}
}

// TestMvRewriteScope checks that mv rewrites only the links to the moved pages,
// keeps how they are written, and counts only the pages it rewrote.
func TestMvRewriteScope(t *testing.T) {
	cfg := setup(t)
	unrelated := "---\nsummary: u\n---\n# u\nsee [t](other.md \"title\") and [d](./index.md) and `[c](a.md)`\n"
	for p, c := range map[string]string{
		"global/a.md":         "---\nsummary: a\n---\n# a\n",
		"global/unrelated.md": unrelated,
		"global/ref.md":       "---\nsummary: r\n---\n# r\n[r](./a.md \"t\") `[c](a.md)` [x](../projects/app/x.md#h)\n",
	} {
		if code, _, errs := runCLI(t, cfg, c, "put", p); code != 0 {
			t.Fatalf("put %s: code=%d %s", p, code, errs)
		}
	}
	unrelatedSha := shaOf(t, cfg, "global/unrelated.md")
	body := func(p string) string {
		t.Helper()
		_, out, _ := runCLI(t, cfg, "", "cat", p)
		return out
	}

	code, out, errs := runCLI(t, cfg, "", "mv", "--json", "global/a.md", "global/a2.md")
	if code != 0 {
		t.Fatalf("mv: code=%d %s %s", code, out, errs)
	}
	var res struct {
		Rewritten int
		Moved     []struct{ From, To string }
	}
	mustUnmarshal(t, out, &res)
	if res.Rewritten != 1 {
		t.Errorf("mv rewritten=%d, want 1: %s", res.Rewritten, out)
	}
	if got := shaOf(t, cfg, "global/unrelated.md"); got != unrelatedSha {
		t.Errorf("unrelated page changed: %s", body("global/unrelated.md"))
	}
	if b := body("global/ref.md"); !strings.Contains(b, "[r](./a2.md \"t\") `[c](a.md)` [x](../projects/app/x.md#h)") {
		t.Errorf("referrer: %q", b)
	}

	code, out, errs = runCLI(t, cfg, "", "mv", "--json", "projects/app/", "projects/app2/")
	if code != 0 {
		t.Fatalf("mv dir: code=%d %s %s", code, out, errs)
	}
	res.Rewritten, res.Moved = 0, nil
	mustUnmarshal(t, out, &res)
	if len(res.Moved) != 1 || res.Rewritten != 1 {
		t.Errorf("mv dir moved=%d rewritten=%d, want 1 and 1: %s", len(res.Moved), res.Rewritten, out)
	}
	if got := shaOf(t, cfg, "global/unrelated.md"); got != unrelatedSha {
		t.Errorf("unrelated page changed by dir mv: %s", body("global/unrelated.md"))
	}
	if b := body("global/ref.md"); !strings.Contains(b, "[x](../projects/app2/x.md#h)") {
		t.Errorf("referrer after dir mv: %q", b)
	}
}

// TestMvRmStaleMirror checks that mv and rm report a conflict and write
// nothing when a page they read was changed from another mirror in between.
func TestMvRmStaleMirror(t *testing.T) {
	cfg := setup(t)
	d := filepath.Dir(cfg)
	cacheA, cacheB := filepath.Join(d, "cache"), filepath.Join(d, "cache-b")
	if code, _, errs := runCLI(t, cfg, "", "ls"); code != 0 {
		t.Fatalf("ls: code=%d %s", code, errs)
	}
	// fromB runs a command with another mirror, leaving mirror A stale.
	fromB := func(stdin string, args ...string) {
		t.Helper()
		t.Setenv("XDG_CACHE_HOME", cacheB)
		defer t.Setenv("XDG_CACHE_HOME", cacheA)
		if code, _, errs := runCLI(t, cfg, stdin, args...); code != 0 {
			t.Fatalf("%v: code=%d %s", args, code, errs)
		}
	}
	type conflict struct{ Error, Reason, Path, Sha, Content, Message string }
	wantConflict := func(name, out string, code int, path, reason, content, cmd string) {
		t.Helper()
		var cf conflict
		mustUnmarshal(t, out, &cf)
		if code != ExitConflict || cf.Error != "conflict" || cf.Reason != reason || cf.Path != path ||
			!strings.Contains(cf.Content, content) || !strings.Contains(cf.Message, "run "+cmd+" again") {
			t.Errorf("%s: code=%d %s", name, code, out)
		}
	}

	// A linking page changed: global/push.md links to global/index.md.
	t.Setenv("XDG_CACHE_HOME", cacheB)
	sha := shaOf(t, cfg, "global/push.md")
	t.Setenv("XDG_CACHE_HOME", cacheA)
	edited := "---\nsummary: how to push\n---\n# push\n[index](index.md)\nEDIT BY B\n"
	fromB(edited, "put", "--base", sha, "global/push.md")
	code, out, _ := runCLI(t, cfg, "", "--no-fetch", "mv", "--json", "global/index.md", "global/start.md")
	wantConflict("mv referrer", out, code, "global/push.md", "changed", "EDIT BY B", "mv")
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/start.md"); code != 1 {
		t.Error("mv with a conflict must not write")
	}
	if code, out, errs := runCLI(t, cfg, "", "mv", "global/index.md", "global/start.md"); code != 0 {
		t.Fatalf("mv again: code=%d %s %s", code, out, errs)
	}
	_, out, _ = runCLI(t, cfg, "", "cat", "global/push.md")
	if !strings.Contains(out, "EDIT BY B") || !strings.Contains(out, "](start.md)") {
		t.Errorf("mv again must keep the change and rewrite the link: %s", out)
	}

	// The moved page changed.
	fromB("---\nsummary: x\n---\n# x\nEDIT BY B\n", "put", "--base", shaOf(t, cfg, "projects/app/x.md"), "projects/app/x.md")
	code, out, _ = runCLI(t, cfg, "", "--no-fetch", "mv", "--json", "projects/app/", "projects/app2/")
	wantConflict("mv dir source", out, code, "projects/app/x.md", "changed", "EDIT BY B", "mv")

	// The destination was created.
	fromB("---\nsummary: d\n---\n# d\n", "put", "global/dest.md")
	code, out, _ = runCLI(t, cfg, "", "--no-fetch", "mv", "--json", "global/push.md", "global/dest.md")
	wantConflict("mv destination", out, code, "global/dest.md", "exists", "# d", "mv")
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/push.md"); code != 0 {
		t.Error("mv onto a created page must not move the source")
	}

	// rm of a page that changed.
	sha = shaOf(t, cfg, "global/push.md")
	fromB("---\nsummary: p\n---\n# p\nEDIT 2\n", "put", "--base", sha, "global/push.md")
	code, out, _ = runCLI(t, cfg, "", "--no-fetch", "rm", "--json", "global/push.md")
	wantConflict("rm changed", out, code, "global/push.md", "changed", "EDIT 2", "rm")
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/push.md"); code != 0 {
		t.Error("rm with a conflict must not delete the page")
	}
}

func TestOptionalSummary(t *testing.T) {
	cfg := setup(t)
	runCLI(t, cfg, "---\ntype: note\n---\n# Heading Title\nlease\n", "put", "global/nosum.md")
	runCLI(t, cfg, "---\ndescription: from description\n---\n# d\nlease\n", "put", "global/desc.md")
	runCLI(t, cfg, "no heading, no frontmatter\nlease\n", "put", "global/nofm.md")
	type item struct{ Path, Summary, Title string }
	var res struct{ Items []item }
	byPath := func(items []item) map[string]item {
		m := map[string]item{}
		for _, it := range items {
			m[it.Path] = it
		}
		return m
	}
	_, out, _ := runCLI(t, cfg, "", "ls", "--json", "global")
	mustUnmarshal(t, out, &res)
	got := byPath(res.Items)
	if it := got["global/nofm.md"]; it.Summary != "" || it.Title != "nofm" {
		t.Errorf("ls nofm: %+v", it)
	}
	if it := got["global/nosum.md"]; it.Summary != "" || it.Title != "Heading Title" {
		t.Errorf("ls nosum: %+v", it)
	}
	if it := got["global/desc.md"]; it.Summary != "from description" {
		t.Errorf("ls desc: %+v", it)
	}
	_, out, _ = runCLI(t, cfg, "", "ls", "-l", "global")
	for name, want := range map[string]string{"nosum.md": "Heading Title", "nofm.md": "nofm", "index.md": "entry point"} {
		found := false
		for _, l := range strings.Split(out, "\n") {
			if f := strings.Fields(l); len(f) >= 4 && f[2] == name && strings.HasSuffix(l, "  "+want) {
				found = true
			}
		}
		if !found {
			t.Errorf("ls -l must show %q for %s: %q", want, name, out)
		}
	}
	// lint still reports the missing summary.
	code, out, _ := runCLI(t, cfg, "", "lint", "global/nosum.md", "global/nofm.md", "global/desc.md")
	if code != 4 || strings.Count(out, "missing_summary") != 2 || strings.Contains(out, "desc.md") {
		t.Errorf("lint: code=%d out=%q", code, out)
	}
}

func TestTree(t *testing.T) {
	cfg := setup(t)
	if code, _, errs := runCLI(t, cfg, "---\nsummary: z\n---\n# z\n", "put", "projects/app/sub/z.md"); code != 0 {
		t.Fatalf("put: code=%d %s", code, errs)
	}
	for args, want := range map[string]string{
		"":               ".\n├── global\n│   ├── index.md\n│   └── push.md\n├── machines\n│   └── h1\n└── projects\n    └── app\n        ├── sub\n        │   └── z.md\n        └── x.md\n\n6 directories, 4 files\n",
		"-d -L 2":        ".\n├── global\n├── machines\n│   └── h1\n└── projects\n    └── app\n\n5 directories\n",
		"-a machines":    "machines\n└── h1\n    └── y.md\n\n1 directory, 1 file\n",
		"/projects/app/": "projects/app\n├── sub\n│   └── z.md\n└── x.md\n\n1 directory, 2 files\n",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"tree"}, strings.Fields(args)...)...); code != 0 || out != want {
			t.Errorf("tree %s: code=%d errs=%q\n%s", args, code, errs, out)
		}
	}
	var res struct {
		Items              []struct{ Path, Kind string }
		Directories, Files int
	}
	code, out, _ := runCLI(t, cfg, "", "tree", "--json", "projects")
	mustUnmarshal(t, out, &res)
	if code != 0 || res.Directories != 2 || res.Files != 2 || len(res.Items) != 4 || res.Items[0].Path != "projects/app" || res.Items[0].Kind != "dir" {
		t.Errorf("tree --json: code=%d %s", code, out)
	}
	if code, out, errs := runCLI(t, cfg, "", "tree", "global/index.md", "none"); code != 1 || out != "\n0 directories, 0 files\n" ||
		errs != "wikictl: global/index.md: not a directory\nwikictl: none: no such file or directory\n" {
		t.Errorf("tree of paths that are not directories: code=%d out=%q errs=%q", code, out, errs)
	}
}

// TestMissingPaths checks that every command reports a path that does not
// exist, or is of the wrong kind, with the same wording on standard error, and
// prints its result JSON with --json.
func TestMissingPaths(t *testing.T) {
	cfg := setup(t)
	runCLI(t, cfg, "# a\n", "put", "global/sub/a.md")
	for _, c := range []struct {
		args []string
		code int
		errs string
		json string
	}{
		{[]string{"cat", "none"}, ExitError, "none: no such file or directory", `{"items":[]}`},
		{[]string{"cat", "global"}, ExitError, "global: is a directory", `{"items":[]}`},
		{[]string{"cat", "global/"}, ExitError, "global: is a directory", `{"items":[]}`},
		{[]string{"cat", "."}, ExitError, ".: is a directory", `{"items":[]}`},
		{[]string{"stat", "none"}, ExitError, "none: no such file or directory", `{"items":[]}`},
		{[]string{"stat", "global"}, ExitError, "global: is a directory", `{"items":[]}`},
		{[]string{"links", "none"}, ExitError, "none: no such file or directory", `{"items":[]}`},
		{[]string{"links", "global"}, ExitError, "global: is a directory", `{"items":[]}`},
		{[]string{"ls", "none"}, ExitError, "none: no such file or directory", `{"items":[]}`},
		{[]string{"find", "none"}, ExitError, "none: no such file or directory", `{"items":[]}`},
		{[]string{"tree", "none"}, ExitError, "none: no such file or directory", `{"directories":0,"files":0,"items":[]}`},
		{[]string{"tree", "global/index.md"}, ExitError, "global/index.md: not a directory", `{"directories":0,"files":0,"items":[]}`},
		{[]string{"lint", "none"}, ExitError, "none: no such file or directory", `{"items":[]}`},
		{[]string{"lint", "global/sub/a.md", "none"}, ExitError, "none: no such file or directory",
			`{"items":[{"path":"global/sub/a.md","line":1,"code":"missing_summary","message":"frontmatter is missing"}]}`},
		{[]string{"rm", "global/none.md"}, ExitError, "global/none.md: no such file or directory", `{"commit":"","paths":[]}`},
		{[]string{"mv", "global/none.md", "global/new.md"}, ExitError, "global/none.md: no such file or directory", `{"commit":"","moved":[],"rewritten":0}`},
		{[]string{"grep", "lease", "none"}, ExitUsage, "none: no such file or directory", `{"items":[]}`},
	} {
		for _, js := range []bool{false, true} {
			args := c.args
			if js {
				args = append([]string{"--json"}, args...)
			}
			code, out, errs := runCLI(t, cfg, "", args...)
			if code != c.code || errs != "wikictl: "+c.errs+"\n" || (js && out != c.json+"\n") {
				t.Errorf("%v: code=%d out=%q errs=%q", args, code, out, errs)
			}
		}
	}
}

func TestProfile(t *testing.T) {
	cfg := setup(t)
	b, _ := os.ReadFile(cfg)
	remote := strings.TrimPrefix(strings.SplitN(string(b), "\n", 2)[0], "repo: ")
	os.WriteFile(cfg, []byte("author: {name: agent, email: a@a}\nprofiles:\n  home: {repo: "+remote+"}\n  work: {repo: /nonexistent/work.git}\n"), 0o600)

	code, out, errs := runCLI(t, cfg, "", "context", "--json", "--profile", "home")
	if code != 0 || !strings.Contains(out, `"profile":"home"`) || !strings.Contains(out, `"profile_source":"flag"`) || !strings.Contains(out, `"repo":"`+remote+`"`) {
		t.Errorf("--profile: code=%d %s %s", code, out, errs)
	}
	t.Setenv("WIKICTL_PROFILE", "home")
	if code, out, errs := runCLI(t, cfg, "", "context", "--json"); code != 0 || !strings.Contains(out, `"profile_source":"env"`) {
		t.Errorf("WIKICTL_PROFILE: code=%d %s %s", code, out, errs)
	}
	t.Setenv("WIKICTL_PROFILE", "")
	if code, out, _ := runCLI(t, cfg, "", "--json", "context", "--profile", "nope"); code != ExitUsage || !strings.Contains(out, `"error":"usage"`) {
		t.Errorf("unknown profile: code=%d %s", code, out)
	}
	if code, _, errs := runCLI(t, cfg, "", "context"); code != ExitUsage || !strings.Contains(errs, "no profile is selected") {
		t.Errorf("no profile: code=%d %s", code, errs)
	}
}

func TestProfileMatch(t *testing.T) {
	cfg := setup(t)
	d := filepath.Dir(cfg)
	home := filepath.Join(d, "remote.git")
	work := filepath.Join(d, "work-wiki.git")
	lab := filepath.Join(d, "lab-wiki.git")
	mustRun(t, "", "git", "clone", "-q", "--bare", home, work)
	mustRun(t, "", "git", "clone", "-q", "--bare", home, lab)
	app := filepath.Join(d, "app")
	mustRun(t, "", "git", "init", "-q", app)
	mustRun(t, app, "git", "remote", "add", "origin", "git@gitlab.example.com:team/app.git")
	labDir := filepath.Join(d, "lab", "sub")
	os.MkdirAll(labDir, 0o755)
	other := filepath.Join(d, "other")
	os.MkdirAll(other, 0o755)
	os.WriteFile(cfg, []byte(`author: {name: agent, email: a@a}
default_profile: home
profiles:
  home: {repo: `+home+`}
  work:
    repo: `+work+`
    match: {remotes: ["gitlab.example.com/team/*"]}
  lab:
    repo: `+lab+`
    match: {paths: ["`+filepath.Dir(labDir)+`"]}
`), 0o600)

	type ctxOut struct{ Profile, ProfileSource, Repo, Mirror string }
	contextIn := func(dir string) ctxOut {
		t.Helper()
		t.Chdir(dir)
		code, out, errs := runCLI(t, cfg, "", "context", "--json")
		if code != 0 {
			t.Fatalf("context in %s: code=%d %s %s", dir, code, out, errs)
		}
		var raw struct {
			Profile       string `json:"profile"`
			ProfileSource string `json:"profile_source"`
			Repo          string `json:"repo"`
			Mirror        string `json:"mirror"`
		}
		mustUnmarshal(t, out, &raw)
		return ctxOut{raw.Profile, raw.ProfileSource, raw.Repo, raw.Mirror}
	}
	byRemote, byPath, byDefault := contextIn(app), contextIn(labDir), contextIn(other)
	if byRemote.Profile != "work" || byRemote.ProfileSource != "match" || byRemote.Repo != work {
		t.Errorf("match.remotes: %+v", byRemote)
	}
	if byPath.Profile != "lab" || byPath.ProfileSource != "match" || byPath.Repo != lab {
		t.Errorf("match.paths: %+v", byPath)
	}
	if byDefault.Profile != "home" || byDefault.ProfileSource != "default" || byDefault.Repo != home {
		t.Errorf("default: %+v", byDefault)
	}
	if byRemote.Mirror == "" || byRemote.Mirror == byPath.Mirror || byRemote.Mirror == byDefault.Mirror || byPath.Mirror == byDefault.Mirror {
		t.Errorf("each profile needs its own mirror: %q %q %q", byRemote.Mirror, byPath.Mirror, byDefault.Mirror)
	}

	// A write from the matched directory reaches only the matched wiki.
	t.Chdir(app)
	if code, _, errs := runCLI(t, cfg, "---\nsummary: w\n---\n# w\n", "put", "global/only-work.md"); code != 0 {
		t.Fatalf("put: code=%d %s", code, errs)
	}
	for bare, want := range map[string]bool{work: true, home: false, lab: false} {
		err := exec.Command("git", "--git-dir", bare, "cat-file", "-e", "main:global/only-work.md").Run()
		if (err == nil) != want {
			t.Errorf("%s has page: %v, want %v", bare, err == nil, want)
		}
	}

	os.WriteFile(cfg, []byte("repo: "+home+"\nprofiles:\n  work:\n    match: {remote: [\"gitlab.example.com/team/*\"]}\n"), 0o600)
	if code, _, errs := runCLI(t, cfg, "", "context"); code != 0 || errs != "wikictl: warning: config file "+cfg+": unknown key \"profiles.work.match.remote\" is ignored\n" {
		t.Errorf("unknown key: code=%d %q", code, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "--json", "context"); code != 0 || !json.Valid([]byte(out)) || !strings.Contains(errs, `unknown key "profiles.work.match.remote"`) {
		t.Errorf("unknown key with --json: code=%d out=%q errs=%q", code, out, errs)
	}
	os.WriteFile(cfg, []byte("reop: "+home+"\n"), 0o600)
	if code, _, errs := runCLI(t, cfg, "", "context"); code != ExitUsage ||
		!strings.HasPrefix(errs, "wikictl: warning: config file "+cfg+": unknown key \"reop\" is ignored\n") || !strings.Contains(errs, "repo is not set") {
		t.Errorf("unknown key with a config error: code=%d %q", code, errs)
	}
}

// Two repo URLs that differ only where "/" and "_" swap places must use
// separate mirrors, so that each write reaches its own wiki.
func TestMirrorPerRepo(t *testing.T) {
	cfg := setup(t)
	d := filepath.Dir(cfg)
	seed := filepath.Join(d, "remote.git")
	work := filepath.Join(d, "srv", "foo_bar", "wiki.git")
	private := filepath.Join(d, "srv", "foo", "bar_wiki.git")
	mustRun(t, "", "git", "clone", "-q", "--bare", seed, work)
	mustRun(t, "", "git", "clone", "-q", "--bare", seed, private)
	os.WriteFile(cfg, []byte(`author: {name: agent, email: a@a}
profiles:
  work: {repo: `+work+`}
  private: {repo: `+private+`}
`), 0o600)

	if code, _, errs := runCLI(t, cfg, "", "--profile", "work", "ls"); code != 0 {
		t.Fatalf("ls work: code=%d %s", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: s\n---\n# s\n", "--profile", "private", "put", "personal/secret.md"); code != 0 {
		t.Fatalf("put private: code=%d %s", code, errs)
	}
	for bare, want := range map[string]bool{private: true, work: false} {
		err := exec.Command("git", "--git-dir", bare, "cat-file", "-e", "main:personal/secret.md").Run()
		if (err == nil) != want {
			t.Errorf("%s has page: %v, want %v", bare, err == nil, want)
		}
	}

	mirror := func(profile string) string {
		t.Helper()
		code, out, errs := runCLI(t, cfg, "", "--profile", profile, "--no-fetch", "context", "--json")
		if code != 0 {
			t.Fatalf("context %s: code=%d %s", profile, code, errs)
		}
		var c struct {
			Mirror string `json:"mirror"`
		}
		mustUnmarshal(t, out, &c)
		return c.Mirror
	}
	if w, p := mirror("work"), mirror("private"); w == p || filepath.Dir(w) != filepath.Join(d, "cache", "wikictl") {
		t.Errorf("mirrors: work %q, private %q", w, p)
	}
}

// Credentials in the repo URL and in the origin remote of the current
// directory must not appear in context output or in git error messages.
func TestCredentialsRedacted(t *testing.T) {
	cfg := setup(t)
	d := filepath.Dir(cfg)
	app := filepath.Join(d, "app")
	mustRun(t, "", "git", "init", "-q", app)
	mustRun(t, app, "git", "remote", "add", "origin", "https://bob:ghp_remote@example.com/team/app.git")
	t.Chdir(app)

	os.WriteFile(cfg, []byte("repo: nope://alice:ghp_repo@example.com/wiki.git\nbranch: main\nauthor: {name: agent, email: a@a}\n"), 0o600)
	code, out, errs := runCLI(t, cfg, "", "--no-fetch", "context", "--json")
	if code != 0 {
		t.Fatalf("context: code=%d %s %s", code, out, errs)
	}
	var cx struct {
		Repo, Remote string
	}
	mustUnmarshal(t, out, &cx)
	if cx.Repo != "nope://***@example.com/wiki.git" || cx.Remote != "https://***@example.com/team/app.git" {
		t.Errorf("context json: %+v", cx)
	}
	_, out, _ = runCLI(t, cfg, "", "--no-fetch", "context")
	if !strings.Contains(out, "repo: nope://***@example.com/wiki.git\n") || !strings.Contains(out, "remote: https://***@example.com/team/app.git\n") {
		t.Errorf("context text: %s", out)
	}
	for _, s := range []string{out, errs} {
		if strings.Contains(s, "ghp_") {
			t.Errorf("credentials in output: %s", s)
		}
	}

	// Without branch, the remote HEAD is read with ls-remote, which fails for
	// the unknown scheme; the arguments of the git command are in the message.
	os.WriteFile(cfg, []byte("repo: nope://alice:ghp_repo@example.com/other.git\nauthor: {name: agent, email: a@a}\n"), 0o600)
	code, out, errs = runCLI(t, cfg, "", "ls")
	if code != ExitGit || !strings.Contains(errs, "ls-remote --symref nope://***@example.com/other.git HEAD") || strings.Contains(out+errs, "ghp_") {
		t.Errorf("ls: code=%d %s %s", code, out, errs)
	}
	code, out, _ = runCLI(t, cfg, "", "--json", "ls")
	var e errorOut
	mustUnmarshal(t, out, &e)
	if code != ExitGit || e.Error != "git" || strings.Contains(e.Message, "ghp_") || !strings.Contains(e.Message, "nope://***@example.com") {
		t.Errorf("ls --json: code=%d %s", code, out)
	}
}

func TestMirrorName(t *testing.T) {
	a := mirrorName("/srv/foo_bar/wiki.git")
	b := mirrorName("/srv/foo/bar_wiki.git")
	if a == b {
		t.Errorf("colliding names: %q", a)
	}
	for repo, prefix := range map[string]string{
		"/srv/foo_bar/wiki.git":                      "wiki-",
		"git@github.com:team/app.wiki.git":           "app.wiki-",
		"https://alice:token@example.com/team/wiki/": "wiki-",
		"https://alice:token@example.com":            "example.com-",
		"https://token@example.com:8443/":            "8443-",
		`C:\wikis\notes`:                             "notes-",
		"":                                           "wiki-",
	} {
		got := mirrorName(repo)
		if !strings.HasPrefix(got, prefix) || len(got) != len(prefix)+12 || strings.Contains(got, "token") {
			t.Errorf("mirrorName(%q) = %q, want %s<12 hex digits>", repo, got, prefix)
		}
	}
}

// A GIT_DIR inherited from a git hook or alias must not redirect the
// mirror's git commands to the caller's repository.
func TestPutIgnoresCallerGitDir(t *testing.T) {
	cfg := setup(t)
	d := t.TempDir()
	projRemote := filepath.Join(d, "proj.git")
	mustRun(t, "", "git", "init", "-q", "--bare", "-b", "main", projRemote)
	proj := filepath.Join(d, "proj")
	mustRun(t, "", "git", "clone", "-q", projRemote, proj)
	mustRun(t, proj, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "proj")
	mustRun(t, proj, "git", "push", "-q", "origin", "HEAD:main")
	projHead := gitOut(t, "--git-dir", projRemote, "rev-parse", "main")
	if code, _, errs := runCLI(t, cfg, "", "cat", "global/index.md"); code != 0 {
		t.Fatalf("cat code=%d %s", code, errs)
	}

	t.Setenv("GIT_DIR", filepath.Join(proj, ".git"))
	t.Setenv("GIT_WORK_TREE", proj)
	if code, _, errs := runCLI(t, cfg, "---\nsummary: n\n---\n# n\n", "put", "global/note.md"); code != 0 {
		t.Fatalf("put code=%d %s", code, errs)
	}
	os.Unsetenv("GIT_DIR")
	os.Unsetenv("GIT_WORK_TREE")

	wikiRemote := filepath.Join(filepath.Dir(cfg), "remote.git")
	if err := exec.Command("git", "--git-dir", wikiRemote, "cat-file", "-e", "main:global/note.md").Run(); err != nil {
		t.Error("note.md not pushed to the wiki remote")
	}
	if got := gitOut(t, "--git-dir", projRemote, "rev-parse", "main"); got != projHead {
		t.Errorf("project remote main moved: %s -> %s", projHead, got)
	}
	if out, _ := exec.Command("git", "--git-dir", filepath.Join(proj, ".git"), "config", "--get", "wikictl.branch").Output(); len(out) != 0 {
		t.Errorf("wikictl.branch written to the project repository: %s", out)
	}
}

func gitOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// TestWriteWithoutChange checks that writes whose source is not a page, or
// that leave the tree unchanged, create no commit on the remote.
func TestWriteWithoutChange(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	pushFiles(t, cfg, map[string]string{"README.md": "# wiki\n"})
	head := gitOut(t, "--git-dir", remote, "rev-parse", "main")

	if code, out, errs := runCLI(t, cfg, "", "mv", "README.md", "global/readme.md"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv of a non-page file: code=%d out=%q errs=%q", code, out, errs)
	}
	if got := gitOut(t, "--git-dir", remote, "rev-parse", "main"); got != head {
		t.Fatalf("rejected mv moved the remote branch: %s -> %s", head, got)
	}

	sha := shaOf(t, cfg, "global/index.md")
	code, out, errs := runCLI(t, cfg, "---\nsummary: entry point\n---\n# global\n", "put", "--json", "--base", sha, "global/index.md")
	var res struct{ Path, Sha, Commit string }
	mustUnmarshal(t, out, &res)
	if code != 0 || res.Sha != sha || res.Commit != head {
		t.Errorf("put of unchanged content: code=%d out=%q errs=%q, want sha=%s commit=%s", code, out, errs, sha, head)
	}
	if got := gitOut(t, "--git-dir", remote, "rev-parse", "main"); got != head {
		t.Errorf("put of unchanged content created a commit: %s -> %s", head, got)
	}
}

// TestPageLimits pushes pages beyond the size and nesting limits without put
// and checks that the read commands finish and lint reports them.
func TestPageLimits(t *testing.T) {
	cfg := setup(t)
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	pages := map[string]string{
		"global/deep.md":  "---\nsummary: deep\nx: " + nest(30000) + "\n---\n# deep\n",
		"global/large.md": "---\nsummary: large\nx: " + nest(200000) + "\n---\n# large\n",
	}
	pushFiles(t, cfg, pages)

	if code, out, errs := runCLI(t, cfg, "", "ls", "--json", "global"); code != 0 || !strings.Contains(out, "global/deep.md") {
		t.Errorf("ls: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "grep", "--json", "-l", "summary", "global"); code != 0 || !strings.Contains(out, "global/large.md") {
		t.Errorf("grep: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "stat", "--json", "global/deep.md"); code != 0 || !strings.Contains(out, `"title":"deep","summary":""`) {
		t.Errorf("stat: code=%d out=%.300q errs=%q", code, out, errs)
	}
	code, out, _ := runCLI(t, cfg, "", "lint")
	for _, w := range []string{"global/deep.md:1: frontmatter_invalid", "global/large.md:1: frontmatter_invalid"} {
		if !strings.Contains(out, w) {
			t.Errorf("lint: missing %q in %q", w, out)
		}
	}
	if code != 4 {
		t.Errorf("lint: code=%d", code)
	}
	pages["global/huge.md"] = "---\nsummary: huge\n---\n# huge\n" + strings.Repeat("lorem ipsum\n", 100000)
	for p, c := range pages {
		if code, _, errs := runCLI(t, cfg, c, "put", p+".new.md"); code != 4 {
			t.Errorf("put %s: code=%d errs=%q", p, code, errs)
		}
	}
}

// TestGrepIgnoreCaseFixed checks that -i with -F ignores the case of letters
// other than ASCII, whatever the locale git runs in.
func TestGrepIgnoreCaseFixed(t *testing.T) {
	cfg := setup(t)
	for p, c := range map[string]string{
		"global/both.md": "---\nsummary: both\n---\n# both\nÄpfel ΟΔΟΣ (a.b)\n",
		"global/one.md":  "---\nsummary: one\n---\n# one\nÄPFEL axb\n",
	} {
		if code, _, errs := runCLI(t, cfg, c, "put", p); code != 0 {
			t.Fatalf("put %s: %s", p, errs)
		}
	}
	for args, want := range map[string]string{
		"-iFl äpfel global":                        "global/both.md\nglobal/one.md\n",
		"-iFl --all-match -e äpfel -e οδος global": "global/both.md\n",
		"-iFl (A.B) global":                        "global/both.md\n",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"grep"}, strings.Fields(args)...)...); code != 0 || out != want {
			t.Errorf("grep %s: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
}

// A relative repo in the config file is resolved against the directory of
// the config file, whatever the current directory is, and the mirror is
// named after and points to the resolved path.
func TestRelativeRepo(t *testing.T) {
	cfg := setupEmpty(t)
	d := filepath.Dir(cfg)
	os.WriteFile(cfg, []byte("repo: ./remote.git\nauthor: {name: a, email: a@a}\n"), 0o600)
	elsewhere := filepath.Join(d, "elsewhere")
	os.MkdirAll(elsewhere, 0o755)
	t.Chdir(elsewhere)

	if code, _, errs := runCLI(t, cfg, "---\nsummary: s\n---\n# s\nrelative-word\n", "put", "global/rel.md"); code != 0 {
		t.Fatalf("put: code=%d %s", code, errs)
	}
	code, out, errs := runCLI(t, cfg, "", "grep", "--json", "-l", "relative-word")
	if code != 0 {
		t.Fatalf("grep: code=%d %s", code, errs)
	}
	var res struct{ Items []struct{ Path string } }
	mustUnmarshal(t, out, &res)
	if len(res.Items) != 1 || res.Items[0].Path != "global/rel.md" {
		t.Errorf("grep: %s", out)
	}

	abs := filepath.Join(d, "remote.git")
	code, out, errs = runCLI(t, cfg, "", "--no-fetch", "context", "--json")
	if code != 0 {
		t.Fatalf("context: code=%d %s", code, errs)
	}
	var cx struct{ Repo, Mirror string }
	mustUnmarshal(t, out, &cx)
	if cx.Repo != abs || filepath.Base(cx.Mirror) != mirrorName(abs) {
		t.Errorf("context: %s", out)
	}
	if got := gitOut(t, "--git-dir", cx.Mirror, "config", "--get", "remote.origin.url"); strings.TrimSpace(got) != abs {
		t.Errorf("remote.origin.url = %q, want %q", got, abs)
	}
}

// TestGrepGitConfig checks that grep settings in the git configuration change
// neither the pattern syntax nor the output.
func TestGrepGitConfig(t *testing.T) {
	cfg := setup(t)
	gc := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(gc, []byte("[grep]\n\tcolumn = true\n\tpatternType = perl\n\tlineNumber = true\n[color]\n\tui = always\n\tgrep = always\n"), 0o600)
	t.Setenv("GIT_CONFIG_GLOBAL", gc)
	for args, want := range map[string]string{
		`-n le\(a\)se projects`: "projects/app/x.md:5:lease\n",
		"-c lease projects":     "projects/app/x.md:1\n",
		"-l lease projects":     "projects/app/x.md\n",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"grep"}, strings.Fields(args)...)...); code != 0 || out != want {
			t.Errorf("grep %s: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
}

// TestReadGitConfig checks that color, attributes, template and log encoding
// settings in the git configuration change neither the backlinks, the pages
// that ls hides as deprecated, the pages grep finds, nor the update time. Each
// command runs in a new mirror, which a template directory would populate.
func TestReadGitConfig(t *testing.T) {
	for name, conf := range map[string]string{
		"color.ui":          "[color]\n\tui = always\n",
		"color.grep":        "[color]\n\tgrep = always\n",
		"attributesFile":    "[core]\n\tattributesFile = DIR/attributes\n",
		"templateDir":       "[init]\n\ttemplateDir = DIR/template\n",
		"logOutputEncoding": "[i18n]\n\tlogOutputEncoding = UTF-16\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := setup(t)
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "attributes"), []byte("*.md -diff\n"), 0o600)
			os.MkdirAll(filepath.Join(dir, "template", "info"), 0o755)
			os.WriteFile(filepath.Join(dir, "template", "info", "attributes"), []byte("*.md binary\n"), 0o600)
			cmds := [][]string{{"links", "-i", "global/index.md"}, {"ls", "machines/h1"}, {"grep", "-l", "lease"}, {"stat", "global/push.md"}}
			want := make([]string, len(cmds))
			for i, args := range cmds {
				_, want[i], _ = runCLI(t, cfg, "", args...)
			}
			if !strings.Contains(want[0], "global/push.md") || strings.Contains(want[1], "y.md") || !strings.Contains(want[2], "projects/app/x.md") || !strings.Contains(want[3], "updated: 2") {
				t.Fatalf("without configuration: %q", want)
			}
			gc := filepath.Join(dir, "gitconfig")
			os.WriteFile(gc, []byte(strings.ReplaceAll(conf, "DIR", dir)), 0o600)
			t.Setenv("GIT_CONFIG_GLOBAL", gc)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
			for i, args := range cmds {
				if code, out, errs := runCLI(t, cfg, "", args...); code != 0 || out != want[i] {
					t.Errorf("%v: code=%d out=%q errs=%q, want %q", args, code, out, errs, want[i])
				}
			}
		})
	}
}

// TestWriteGitConfig checks that put succeeds when the git configuration sets
// hooks that fail or signed pushes, and that the hooks do not run.
func TestWriteGitConfig(t *testing.T) {
	for name, conf := range map[string]string{
		"hooksPath": "[core]\n\thooksPath = HOOKS\n",
		"gpgSign":   "[push]\n\tgpgSign = true\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := setup(t)
			hooks := t.TempDir()
			marker := filepath.Join(hooks, "ran")
			if strings.Contains(conf, "HOOKS") {
				os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
				os.WriteFile(filepath.Join(hooks, "post-index-change"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755)
			}
			gc := filepath.Join(t.TempDir(), "gitconfig")
			os.WriteFile(gc, []byte(strings.ReplaceAll(conf, "HOOKS", hooks)), 0o600)
			t.Setenv("GIT_CONFIG_GLOBAL", gc)
			if code, _, errs := runCLI(t, cfg, "---\nsummary: n\n---\n# n\n", "put", "global/new.md"); code != 0 {
				t.Fatalf("put: code=%d errs=%q", code, errs)
			}
			if code, out, _ := runCLI(t, cfg, "", "cat", "global/new.md"); code != 0 || out != "---\nsummary: n\n---\n# n\n" {
				t.Errorf("cat: code=%d out=%q", code, out)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Error("a hook ran in the mirror")
			}
		})
	}
}

// TestQuotedNames checks the commands on pages pushed from a clone with names
// that git quotes unless -z is given: a newline, a control character, a
// double quote and a backslash.
func TestQuotedNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the file system does not allow these names")
	}
	cfg := setup(t)
	nl, ctl, q := "global/n\nl.md", "global/c\x01.md", "global/q\"\\.md"
	files := map[string]string{
		nl:  "---\nsummary: newline\nstatus: deprecated\ntags: [odd]\n---\n# nl\nlease [push](push.md)\n",
		ctl: "---\nsummary: control\ntags: [odd]\n---\n# ctl\nlease [push](push.md)\n",
		q:   "# q\nlease [push](push.md)\n",
	}
	pushFiles(t, cfg, files)
	enl, ectl := `global/n\x0al.md`, `global/c\x01.md`

	for _, args := range [][]string{{"lint", "--json"}, {"lint", "--json", "global"}} {
		_, out, _ := runCLI(t, cfg, "", args...)
		var res struct{ Items []struct{ Path, Code string } }
		mustUnmarshal(t, out, &res)
		codes := map[string][]string{}
		for _, it := range res.Items {
			codes[it.Path] = append(codes[it.Path], it.Code)
		}
		if !slices.Contains(codes[q], "missing_summary") || slices.Contains(codes[nl], "missing_summary") ||
			slices.Contains(codes[ctl], "missing_summary") || codes[nl] == nil || codes[ctl] == nil {
			t.Errorf("%v: %s", args, out)
		}
	}
	// Paths with control characters cannot be given on the command line.
	if _, out, errs := runCLI(t, cfg, "", "cat", "global/index.md", q); out != "---\nsummary: entry point\n---\n# global\n"+files[q] {
		t.Errorf("cat: %q %q", out, errs)
	}
	if _, out, errs := runCLI(t, cfg, "", "stat", q); !strings.Contains(out, "title: q\n") {
		t.Errorf("stat: %q %q", out, errs)
	}
	if _, out, _ := runCLI(t, cfg, "", "ls", "-l", "global"); strings.Contains(out, `n\x0al.md`) || !strings.Contains(out, `c\x01.md  control`) || !strings.Contains(out, `q"\.md    q`) {
		t.Errorf("ls -l must hide the deprecated page: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "ls", "-la", "global"); !strings.Contains(out, `n\x0al.md  newline`) || !strings.Contains(out, `c\x01.md   control`) || !strings.Contains(out, `q"\.md     q`) {
		t.Errorf("ls -la: %q", out)
	}
	if _, out, errs := runCLI(t, cfg, "", "find", "global", "-meta", "tags=odd"); out != ectl+"\n"+enl+"\n" {
		t.Errorf("find -meta: %q %q", out, errs)
	}
	if _, out, errs := runCLI(t, cfg, "", "links", "--in", "global/push.md"); out != "in\tmentions\t"+ectl+"\nin\tmentions\t"+enl+"\nin\tmentions\t"+q+"\n" {
		t.Errorf("links --in: %q %q", out, errs)
	}
	if _, out, errs := runCLI(t, cfg, "", "links", q); out != "out\tmentions\tglobal/push.md\n" {
		t.Errorf("links: %q %q", out, errs)
	}
	if _, out, errs := runCLI(t, cfg, "", "grep", "-l", "lease", "global"); out != ectl+"\n"+enl+"\nglobal/push.md\n"+q+"\n" {
		t.Errorf("grep -l: %q %q", out, errs)
	}
	for args, want := range map[string]string{
		"-n lease global": ectl + ":6:lease [push](push.md)\n" + enl + ":7:lease [push](push.md)\nglobal/push.md:7:use force-with-lease. [index](index.md)\n" + q + ":2:lease [push](push.md)\n",
		"-c lease global": ectl + ":1\n" + enl + ":1\nglobal/push.md:1\n" + q + ":1\n",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"grep"}, strings.Fields(args)...)...); code != 0 || out != want {
			t.Errorf("grep %s: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
	var res struct{ Items []struct{ Path string } }
	_, out, _ := runCLI(t, cfg, "", "grep", "--json", "lease", "global")
	if mustUnmarshal(t, out, &res); len(res.Items) != 4 || res.Items[0].Path != ctl || res.Items[1].Path != nl {
		t.Errorf("grep --json: %s", out)
	}
}

func TestGrepQuietAndErrors(t *testing.T) {
	cfg := setup(t)
	for _, c := range []struct {
		args []string
		code int
		errs string
	}{
		{[]string{"grep", "-q", "--json", "lease"}, ExitOK, ""},
		{[]string{"grep", "-q", "-L", "zzz", "global"}, ExitOK, ""},
		{[]string{"grep", "-q", "lease", "none", "global"}, ExitOK, "none: no such file or directory"},
		{[]string{"grep", "-E", "-F", "x"}, ExitUsage, "-E and -F cannot be combined"},
		{[]string{"grep", "-L", "--all-match", "-e", "a", "-e", "b"}, ExitUsage, "-L and --all-match cannot be combined"},
		{[]string{"grep", "[", "global"}, ExitUsage, "invalid pattern: '['"},
	} {
		code, out, errs := runCLI(t, cfg, "", c.args...)
		if code != c.code || out != "" || !strings.Contains(errs, c.errs) {
			t.Errorf("%v: code=%d out=%q errs=%q", c.args, code, out, errs)
		}
	}
}
