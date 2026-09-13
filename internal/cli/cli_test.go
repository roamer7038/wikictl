package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (cfgPath string) {
	t.Helper()
	isolateGit(t)
	t.Setenv("WIKICTL_PROFILE", "")
	d := t.TempDir()
	remote := filepath.Join(d, "remote.git")
	mustRun(t, "", "git", "init", "-q", "--bare", "-b", "main", remote)
	work := filepath.Join(d, "work")
	mustRun(t, "", "git", "clone", "-q", remote, work)
	files := map[string]string{
		"global/index.md":   "---\nsummary: entry point\n---\n# global\n",
		"global/push.md":    "---\nsummary: how to push\ntype: policy\ntags: [git]\n---\n# push\nuse force-with-lease. [index](index.md)\n\n## Links\n- part_of: [index](index.md)\n",
		"projects/app/x.md": "---\nsummary: x\n---\n# x\nlease\n",
		"machines/h1/y.md":  "---\nsummary: y\nstatus: deprecated\n---\n# y\nlease\n",
	}
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(work, p)), 0o755)
		os.WriteFile(filepath.Join(work, p), []byte(c), 0o644)
	}
	mustRun(t, work, "git", "add", "-A")
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "seed")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(d, "cache"))
	cfgPath = filepath.Join(d, "config.yaml")
	os.WriteFile(cfgPath, []byte("repo: "+remote+"\nauthor: {name: agent, email: a@a}\nmachine: h1\n"), 0o600)
	return cfgPath
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

func TestSearchGetLs(t *testing.T) {
	cfg := setup(t)
	code, out, errs := runCLI(t, cfg, "", "search", "--json", "--dirs", "global,projects/app,machines/h1", "lease")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errs)
	}
	var res struct {
		Items []struct {
			Path, Summary string
			Matched       []string
		}
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Items) != 2 {
		t.Errorf("%s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "search", "--json", "--all", "--dirs", "global,projects/app,machines/h1", "lease")
	json.Unmarshal([]byte(out), &res)
	if len(res.Items) != 3 {
		t.Errorf("--all: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "search", "--json", "--dirs", "global", "zzz-none")
	json.Unmarshal([]byte(out), &res)
	if !strings.Contains(out, `"items":[]`) {
		t.Errorf("empty: %s", out)
	}
	code, out, _ = runCLI(t, cfg, "", "get", "--json", "global/push.md")
	var g struct {
		Path, Sha, Title, Body string
		Links                  []struct{ Type, Target, Note string }
		Backlinks              []struct{ Path, Type string }
	}
	json.Unmarshal([]byte(out), &g)
	if code != 0 || len(g.Sha) != 40 || g.Title != "push" || len(g.Links) != 1 || g.Links[0].Target != "global/index.md" || strings.Contains(g.Body, "## Links") {
		t.Errorf("%s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "get", "--json", "global/index.md")
	json.Unmarshal([]byte(out), &g)
	if len(g.Backlinks) != 1 || g.Backlinks[0].Path != "global/push.md" || g.Backlinks[0].Type != "part_of" {
		t.Errorf("backlinks: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "ls", "--json", "--dirs", "global")
	var ls struct {
		Items []struct{ Path, Summary, Type string }
	}
	json.Unmarshal([]byte(out), &ls)
	if len(ls.Items) != 2 {
		t.Errorf("ls: %s", out)
	}
	code, out, _ = runCLI(t, cfg, "", "context", "--json")
	if code != 0 || !strings.Contains(out, "machines/h1") || !strings.Contains(out, `"machine":"h1"`) {
		t.Errorf("context: %s", out)
	}
	var cx struct{ Dirs []string }
	json.Unmarshal([]byte(out), &cx)
	if len(cx.Dirs) < 2 || cx.Dirs[0] != "global" || cx.Dirs[1] != "personal" {
		t.Errorf("context dirs: %v", cx.Dirs)
	}
	_, out, _ = runCLI(t, cfg, "", "ls", "--json", "--dirs", "global", "--tag", "git")
	json.Unmarshal([]byte(out), &ls)
	if len(ls.Items) != 1 || ls.Items[0].Path != "global/push.md" {
		t.Errorf("ls --tag: %s", out)
	}
	// Global flags may follow the command; "--" ends flag parsing.
	if code, out, _ := runCLI(t, cfg, "", "lint", "--", "global/push.md"); code != 0 || out != "" {
		t.Errorf("lint -- path: code=%d out=%q", code, out)
	}
	if code, _, _ := runCLI(t, cfg, "", "lint", "global/none.md"); code != 1 {
		t.Errorf("lint of a missing page must fail with 1, got %d", code)
	}
	if code, out, _ := runCLI(t, cfg, "", "--json", "bogus"); code != 2 || !strings.HasPrefix(out, `{"error":"usage"`) {
		t.Errorf("unknown command with --json: code=%d out=%q", code, out)
	}
	if code, _, _ = runCLI(t, cfg, "", "get", "global/none.md"); code != 1 {
		t.Errorf("missing get code=%d", code)
	}
	if code, _, _ = runCLI(t, cfg, "", "bogus"); code != 2 {
		t.Errorf("unknown command code=%d", code)
	}
}

func TestPersonalScope(t *testing.T) {
	cfg := setup(t)
	// personal/ is a default directory even before anything is put there,
	// so reads on a wiki without it must succeed with no matches.
	var res struct {
		Items []struct{ Path string }
	}
	code, out, errs := runCLI(t, cfg, "", "search", "--json", "conventional-commits")
	json.Unmarshal([]byte(out), &res)
	if code != 0 || len(res.Items) != 0 {
		t.Fatalf("search without personal/: code=%d out=%s %s", code, out, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "ls", "--json", "--dirs", "personal"); code != 0 || out != `{"items":[]}`+"\n" {
		t.Fatalf("ls without personal/: code=%d out=%q %s", code, out, errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: commit style\n---\n# commits\nconventional-commits\n", "put", "personal/commits.md"); code != 0 {
		t.Fatalf("put: code=%d %s", code, errs)
	}
	code, out, errs = runCLI(t, cfg, "", "search", "--json", "conventional-commits")
	if code != 0 {
		t.Fatalf("search: code=%d %s", code, errs)
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Items) != 1 || res.Items[0].Path != "personal/commits.md" {
		t.Errorf("search: %s", out)
	}
}

func TestPutRmInit(t *testing.T) {
	cfg := setup(t)
	code, out, _ := runCLI(t, cfg, "---\nsummary: new page\n---\n# n\n", "put", "--json", "global/new.md")
	var res struct{ Path, Sha, Commit string }
	json.Unmarshal([]byte(out), &res)
	if code != 0 || len(res.Sha) != 40 {
		t.Fatalf("code=%d %s", code, out)
	}
	code, out, _ = runCLI(t, cfg, "---\nsummary: x\n---\n", "put", "--json", "global/new.md")
	var cf struct{ Error, Reason, Sha, Content string }
	json.Unmarshal([]byte(out), &cf)
	if code != 3 || cf.Error != "conflict" || cf.Reason != "exists" || cf.Sha != res.Sha {
		t.Errorf("code=%d %s", code, out)
	}
	code, out, _ = runCLI(t, cfg, "---\nsummary: updated page\n---\n# n2\n", "put", "--json", "--base", res.Sha, "global/new.md")
	if code != 0 {
		t.Fatalf("%s", out)
	}
	code, out, _ = runCLI(t, cfg, "---\nsummary: z\n---\n", "put", "--json", "--base", res.Sha, "global/new.md")
	json.Unmarshal([]byte(out), &cf)
	if code != 3 || cf.Reason != "changed" || !strings.Contains(cf.Content, "updated page") {
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
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n", "put", "global/a`b.md"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("put name with '`': code=%d errs=%q", code, errs)
	}
	// A name outside the recommended form is only a warning.
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n", "put", "global/Bad_Name.md"); code != 0 || !strings.Contains(errs, "name_style") {
		t.Errorf("style name: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n", "put", "global/日本語.md"); code != 0 || !strings.Contains(errs, "name_style") {
		t.Errorf("non-ascii name: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "get", "global/日本語.md"); code != 0 {
		t.Errorf("non-ascii get code=%d", code)
	}
	// A broken link is only a warning; the write goes through.
	code, _, errs := runCLI(t, cfg, "---\nsummary: w\n---\n# w\n[g](gone.md)\n", "put", "global/warn.md")
	if code != 0 || !strings.Contains(errs, "warning") {
		t.Errorf("code=%d errs=%q", code, errs)
	}
	if code, _, _ = runCLI(t, cfg, "", "rm", "global/new.md"); code != 0 {
		t.Error("rm failed")
	}
	if code, _, _ := runCLI(t, cfg, "", "get", "global/new.md"); code != 1 {
		t.Error("still exists")
	}
}

// isolateGit keeps the user's global git configuration (hooks, gpgsign,
// templates) out of the repositories the tests create.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestInit(t *testing.T) {
	isolateGit(t)
	d := t.TempDir()
	remote := filepath.Join(d, "r.git")
	mustRun(t, "", "git", "init", "-q", "--bare", "-b", "main", remote)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(d, "cache"))
	cfg := filepath.Join(d, "c.yaml")
	os.WriteFile(cfg, []byte("repo: "+remote+"\nbranch: main\nauthor: {name: a, email: a@a}\n"), 0o600)
	if code, _, errs := runCLI(t, cfg, "", "init"); code != 0 {
		t.Fatalf("init code=%d %s", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "get", "global/index.md"); code != 0 {
		t.Error("index.md missing")
	}
	if code, _, _ := runCLI(t, cfg, "", "init"); code != 1 {
		t.Error("second init must fail with 1")
	}
}

func TestMvAndLint(t *testing.T) {
	cfg := setup(t)
	code, out, errs := runCLI(t, cfg, "", "mv", "--json", "global/push.md", "projects/app/push-notes.md")
	if code != 0 {
		t.Fatalf("mv code=%d %s %s", code, out, errs)
	}
	_, out, _ = runCLI(t, cfg, "", "get", "--json", "projects/app/push-notes.md")
	var g struct {
		Body        string
		Frontmatter map[string]any
		Links       []struct{ Target string }
	}
	json.Unmarshal([]byte(out), &g)
	if !strings.Contains(g.Body, "](../../global/index.md)") || len(g.Links) != 1 || g.Links[0].Target != "global/index.md" {
		t.Errorf("self links not rewritten: %s", out)
	}
	if al, _ := g.Frontmatter["aliases"].([]any); len(al) != 1 || al[0] != "push" {
		t.Errorf("aliases: %v", g.Frontmatter["aliases"])
	}
	runCLI(t, cfg, "---\nsummary: x\n---\n# x\n[p](push-notes.md)\n", "put", "--base", getSha(t, cfg, "projects/app/x.md"), "projects/app/x.md")
	if code, _, e := runCLI(t, cfg, "", "mv", "projects/app/push-notes.md", "global/push.md"); code != 0 {
		t.Fatalf("mv back: %s", e)
	}
	_, out, _ = runCLI(t, cfg, "", "get", "--json", "projects/app/x.md")
	if !strings.Contains(out, "](../../global/push.md)") {
		t.Errorf("referrer not rewritten: %s", out)
	}
	if code, _, _ := runCLI(t, cfg, "", "mv", "global/push.md", "global/index.md"); code != 1 {
		t.Error("mv onto existing must fail")
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/push.md", "global/a b.md"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv to bad path: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/push.md", "global/a:b.md"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv to name with ':': code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/push.md", "global/a`b.md"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv to name with '`': code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/push.md", "global/Push.md"); code != 0 || !strings.Contains(errs, "name_style") {
		t.Errorf("mv to style name: code=%d errs=%q", code, errs)
	}
	runCLI(t, cfg, "", "mv", "global/Push.md", "global/push.md")
	runCLI(t, cfg, "---\nsummary: b\n---\n# b\n[gone](gone.md)\n\n## Links\n- x\n* see_also: [i](index.md)\n  + index.md | untyped\n", "put", "global/broken.md")
	_, out, _ = runCLI(t, cfg, "", "get", "--json", "global/broken.md")
	var b struct {
		Links []struct{ Type, Target, Note string }
	}
	json.Unmarshal([]byte(out), &b)
	if len(b.Links) != 2 || b.Links[0].Type != "see_also" || b.Links[1].Type != "see_also" || b.Links[1].Target != "global/index.md" || b.Links[1].Note != "untyped" {
		t.Errorf("tolerant links: %s", out)
	}
	code, out, _ = runCLI(t, cfg, "", "lint", "--json", "--dirs", "global")
	var li struct {
		Items []struct {
			Path, Code string
			Line       int
		}
	}
	json.Unmarshal([]byte(out), &li)
	codes := map[string]int{}
	for _, it := range li.Items {
		codes[it.Code]++
	}
	if code != 4 || codes["broken_link"] != 1 || codes["links_syntax"] != 1 {
		t.Errorf("code=%d %s", code, out)
	}
	if code, _, _ := runCLI(t, cfg, "", "lint", "--dirs", "projects/app"); code != 0 {
		t.Error("clean dir must pass")
	}
}

// TestLinkExistence checks that put and lint agree on which link targets exist:
// any file in the tree counts, and a directory does not.
func TestLinkExistence(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	work := filepath.Join(t.TempDir(), "w")
	mustRun(t, "", "git", "clone", "-q", remote, work)
	os.MkdirAll(filepath.Join(work, "global/sub.md"), 0o755)
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# wiki\n"), 0o644)
	os.WriteFile(filepath.Join(work, "global/sub.md/a.md"), []byte("---\nsummary: a\n---\n"), 0o644)
	mustRun(t, work, "git", "add", "-A")
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "extra")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")

	code, _, errs := runCLI(t, cfg, "---\nsummary: r\n---\n# r\n[r](../README.md)\n", "put", "global/r.md")
	if code != 0 || strings.Contains(errs, "broken_link") {
		t.Errorf("put link to README.md: code=%d errs=%q", code, errs)
	}
	if code, out, _ := runCLI(t, cfg, "", "lint", "global/r.md"); code != 0 {
		t.Errorf("lint link to README.md: code=%d out=%q", code, out)
	}

	code, _, errs = runCLI(t, cfg, "---\nsummary: d\n---\n# d\n[d](sub.md) [i](index.md)\n", "put", "global/d.md")
	if code != 0 || !strings.Contains(errs, "broken_link") || !strings.Contains(errs, "global/sub.md") || strings.Contains(errs, "global/index.md") {
		t.Errorf("put link to a directory: code=%d errs=%q", code, errs)
	}
	code, out, _ := runCLI(t, cfg, "", "lint", "global/d.md")
	if code != 4 || strings.Count(out, "broken_link") != 1 || !strings.Contains(out, "global/sub.md") {
		t.Errorf("lint link to a directory: code=%d out=%q", code, out)
	}
}

func TestLintNames(t *testing.T) {
	cfg := setup(t)
	runCLI(t, cfg, "---\nsummary: i\n---\n", "put", "global/Index.md")
	runCLI(t, cfg, "---\nsummary: s\n---\n", "put", "global/style_name.md")
	runCLI(t, cfg, "---\nsummary: d\n---\n", "put", "projects/App/d.md")
	code, out, _ := runCLI(t, cfg, "", "lint", "--json", "--dirs", "global,projects")
	var li struct {
		Items []struct {
			Path, Code, Message string
		}
	}
	json.Unmarshal([]byte(out), &li)
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

func getSha(t *testing.T, cfg, p string) string {
	t.Helper()
	_, out, _ := runCLI(t, cfg, "", "get", "--json", p)
	var g struct{ Sha string }
	json.Unmarshal([]byte(out), &g)
	return g.Sha
}

func TestMvDir(t *testing.T) {
	cfg := setup(t)
	// Set up x.md linking to global/index.md and global/ref.md linking to x.md.
	runCLI(t, cfg, "---\nsummary: x\n---\n# x\n[i](../../global/index.md)\n", "put", "--base", getSha(t, cfg, "projects/app/x.md"), "projects/app/x.md")
	runCLI(t, cfg, "---\nsummary: z\n---\n# z\n[x](x.md)\n", "put", "projects/app/z.md")
	runCLI(t, cfg, "---\nsummary: ref\n---\n# ref\n\n## Links\n- see_also: [x](../projects/app/x.md)\n", "put", "global/ref.md")
	code, out, errs := runCLI(t, cfg, "", "mv", "--json", "projects/app/", "projects/app2/")
	if code != 0 {
		t.Fatalf("code=%d %s %s", code, out, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "get", "projects/app/x.md"); code != 1 {
		t.Error("old path must be gone")
	}
	_, out, _ = runCLI(t, cfg, "", "get", "--json", "projects/app2/x.md")
	if !strings.Contains(out, "](../../global/index.md)") {
		t.Errorf("moved page self link: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "get", "--json", "projects/app2/z.md")
	if !strings.Contains(out, "](x.md)") {
		t.Errorf("intra-dir link must stay relative: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "get", "--json", "global/ref.md")
	if !strings.Contains(out, `"target":"projects/app2/x.md"`) {
		t.Errorf("external referrer: %s", out)
	}
	if code, _, _ := runCLI(t, cfg, "", "lint", "--dirs", "global,projects"); code != 0 {
		t.Error("lint must pass after dir mv")
	}
	if code, _, _ := runCLI(t, cfg, "", "mv", "projects/app2/", "global/"); code != 1 {
		t.Error("mv onto dir with existing pages must fail")
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/app2/", "projects/new dir/"); code != 4 || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv dir to bad name: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/app2/", "projects/App3/"); code != 0 || !strings.Contains(errs, "name_style") {
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
	if code, _, _ := runCLI(t, cfg, "", "get", "global/index.md"); code != 0 {
		t.Error("rejected dir mv must not move pages")
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/app2/", "global/x.md"); code != 2 || !strings.Contains(errs, "both arguments") {
		t.Errorf("mixed dir/page mv must be a usage error: code=%d errs=%q", code, errs)
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
	code, out, errs := runCLI(t, cfg, "", "search", "--json", "--dirs", "global", "lease")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errs)
	}
	json.Unmarshal([]byte(out), &res)
	got := byPath(res.Items)
	if it := got["global/nosum.md"]; it.Summary != "" || it.Title != "Heading Title" {
		t.Errorf("search nosum: %+v", it)
	}
	if it := got["global/desc.md"]; it.Summary != "from description" || it.Title != "d" {
		t.Errorf("search desc: %+v", it)
	}
	if it := got["global/nofm.md"]; it.Summary != "" || it.Title != "nofm" {
		t.Errorf("search nofm: %+v", it)
	}
	if it := got["global/push.md"]; it.Summary != "how to push" || it.Title != "push" {
		t.Errorf("search push: %+v", it)
	}
	_, out, _ = runCLI(t, cfg, "", "search", "--dirs", "global", "lease")
	for _, want := range []string{"global/nosum.md\tHeading Title\n", "global/desc.md\tfrom description\n", "global/nofm.md\tnofm\n", "global/push.md\thow to push\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("search text missing %q in %q", want, out)
		}
	}
	_, out, _ = runCLI(t, cfg, "", "ls", "--json", "--dirs", "global")
	json.Unmarshal([]byte(out), &res)
	got = byPath(res.Items)
	if it := got["global/nosum.md"]; it.Summary != "" || it.Title != "Heading Title" {
		t.Errorf("ls nosum: %+v", it)
	}
	if it := got["global/desc.md"]; it.Summary != "from description" {
		t.Errorf("ls desc: %+v", it)
	}
	_, out, _ = runCLI(t, cfg, "", "ls", "--dirs", "global")
	for _, want := range []string{"global/nosum.md\tHeading Title\n", "global/nofm.md\tnofm\n", "global/index.md\tentry point\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls text missing %q in %q", want, out)
		}
	}
	// lint still reports the missing summary.
	code, out, _ = runCLI(t, cfg, "", "lint", "global/nosum.md", "global/nofm.md", "global/desc.md")
	if code != 4 || strings.Count(out, "missing_summary") != 2 || strings.Contains(out, "desc.md") {
		t.Errorf("lint: code=%d out=%q", code, out)
	}
}

func TestDirs(t *testing.T) {
	cfg := setup(t)
	runCLI(t, cfg, "---\nsummary: z\n---\n# z\n", "put", "projects/app/sub/z.md")
	type dirsRes struct {
		Items []struct {
			Dir, Summary string
			Pages        int
		}
	}
	decode := func(out string) dirsRes {
		t.Helper()
		var res dirsRes
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("unmarshal %q: %v", out, err)
		}
		return res
	}
	code, out, errs := runCLI(t, cfg, "", "dirs", "--json")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errs)
	}
	res := decode(out)
	want := []struct {
		dir, summary string
		pages        int
	}{{"global/", "entry point", 2}, {"machines/h1/", "", 1}, {"projects/app/", "", 1}, {"projects/app/sub/", "", 1}}
	if len(res.Items) != len(want) {
		t.Fatalf("dirs: %s", out)
	}
	for i, w := range want {
		it := res.Items[i]
		if it.Dir != w.dir || it.Summary != w.summary || it.Pages != w.pages {
			t.Errorf("item %d: got %+v want %+v", i, it, w)
		}
	}
	_, out, _ = runCLI(t, cfg, "", "dirs", "--json", "projects")
	res = decode(out)
	if len(res.Items) != 2 || res.Items[0].Dir != "projects/app/" || res.Items[1].Dir != "projects/app/sub/" {
		t.Errorf("dirs projects: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "dirs", "--json", "global/", "machines")
	res = decode(out)
	if len(res.Items) != 2 || res.Items[0].Dir != "global/" || res.Items[1].Dir != "machines/h1/" {
		t.Errorf("dirs with two args: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "dirs", "--json", "--dirs", "global")
	if res = decode(out); len(res.Items) != len(want) {
		t.Errorf("dirs must ignore --dirs: %s", out)
	}
	if code, out, _ := runCLI(t, cfg, "", "dirs", "--json", "none"); code != 0 || !strings.Contains(out, `"items":[]`) {
		t.Errorf("dirs none: code=%d out=%s", code, out)
	}
	for _, arg := range []string{"../x", "/abs", "global/index.md"} {
		if code, _, errs := runCLI(t, cfg, "", "dirs", arg); code != ExitUsage {
			t.Errorf("dirs %s: code=%d errs=%q", arg, code, errs)
		}
	}
	code, out, _ = runCLI(t, cfg, "", "dirs")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if code != 0 || len(lines) != 4 || !strings.HasPrefix(lines[0], "global/") || !strings.HasSuffix(lines[0], "entry point") || !strings.HasSuffix(lines[1], "(no index)") {
		t.Errorf("dirs text: code=%d out=%q", code, out)
	}
	if f := strings.Fields(lines[0]); len(f) < 3 || f[1] != "2" {
		t.Errorf("dirs text count: %q", lines[0])
	}

	// An index.md without a summary, committed directly to the wiki repository.
	cfgData, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	remote := strings.TrimPrefix(strings.SplitN(string(cfgData), "\n", 2)[0], "repo: ")
	work := filepath.Join(t.TempDir(), "work")
	mustRun(t, "", "git", "clone", "-q", remote, work)
	os.WriteFile(filepath.Join(work, "projects/app/index.md"), []byte("# app\n"), 0o644)
	mustRun(t, work, "git", "add", "-A")
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "index")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	_, out, _ = runCLI(t, cfg, "", "dirs", "--json", "projects/app")
	res = decode(out)
	if len(res.Items) != 2 || res.Items[0].Dir != "projects/app/" || res.Items[0].Pages != 2 || res.Items[0].Summary != "" {
		t.Errorf("index without summary: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "dirs", "projects/app")
	lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "(no index)") || !strings.HasSuffix(lines[1], "(no index)") {
		t.Errorf("index without summary text: %q", out)
	}
}

func TestContextPages(t *testing.T) {
	cfg := setup(t)
	code, out, _ := runCLI(t, cfg, "", "context", "--json", "--dirs", "global,projects/app,machines/h1,projects/none")
	var res struct {
		Dirs  []string
		Pages map[string]int
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	want := map[string]int{"global": 2, "projects/app": 1, "machines/h1": 1, "projects/none": 0}
	if code != 0 || len(res.Dirs) != 4 || len(res.Pages) != 4 {
		t.Fatalf("context: code=%d %s", code, out)
	}
	for d, n := range want {
		if res.Pages[d] != n {
			t.Errorf("pages[%s]=%d want %d: %s", d, res.Pages[d], n, out)
		}
	}
	_, out, _ = runCLI(t, cfg, "", "context", "--dirs", "global,projects/app,projects/none")
	if !strings.Contains(out, "dirs: global (2 pages), projects/app (1 page), projects/none (0 pages)\n") {
		t.Errorf("context text: %s", out)
	}
}

func TestContextPagesRecursive(t *testing.T) {
	cfg := setup(t)
	code, out, _ := runCLI(t, cfg, "", "context", "--json", "--dirs", "projects,.,./,projects/,./global,projects/none")
	var res struct {
		Pages map[string]int
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if code != 0 {
		t.Fatalf("context: code=%d %s", code, out)
	}
	want := map[string]int{"projects": 1, ".": 4, "./": 4, "projects/": 1, "./global": 2, "projects/none": 0}
	for d, n := range want {
		if res.Pages[d] != n {
			t.Errorf("pages[%s]=%d want %d: %s", d, res.Pages[d], n, out)
		}
	}
	_, out, _ = runCLI(t, cfg, "", "context", "--dirs", ".")
	if !strings.Contains(out, "dirs: . (4 pages)\n") {
		t.Errorf("context text: %s", out)
	}
}

func TestDirsDotCoversWholeWiki(t *testing.T) {
	cfg := setup(t)
	var ls struct {
		Items []struct{ Path string }
	}
	_, out, _ := runCLI(t, cfg, "", "ls", "--json", "--all", "--dirs", ".")
	if err := json.Unmarshal([]byte(out), &ls); err != nil || len(ls.Items) != 4 {
		t.Errorf("ls --dirs .: %s", out)
	}
	_, out, _ = runCLI(t, cfg, "", "search", "--json", "--all", "--dirs", ".", "lease")
	ls.Items = nil
	if err := json.Unmarshal([]byte(out), &ls); err != nil || len(ls.Items) != 3 {
		t.Errorf("search --dirs .: %s", out)
	}
	runCLI(t, cfg, "---\nsummary: bad\n---\n# bad\n[x](none.md)\n", "put", "projects/app/bad.md")
	if code, out, _ := runCLI(t, cfg, "", "lint", "--json", "--dirs", "."); code != ExitInvalid || !strings.Contains(out, "projects/app/bad.md") {
		t.Errorf("lint --dirs .: code=%d out=%s", code, out)
	}
}

func TestProfile(t *testing.T) {
	cfg := setup(t)
	b, _ := os.ReadFile(cfg)
	remote := strings.TrimPrefix(strings.SplitN(string(b), "\n", 2)[0], "repo: ")
	os.WriteFile(cfg, []byte("author: {name: agent, email: a@a}\nmachine: h1\nprofiles:\n  home: {repo: "+remote+"}\n  work: {repo: /nonexistent/work.git}\n"), 0o600)

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
machine: h1
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
		json.Unmarshal([]byte(out), &raw)
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
	if code, _, errs := runCLI(t, cfg, "", "context"); code != ExitUsage || !strings.Contains(errs, `unknown field "remote"`) {
		t.Errorf("unknown key: code=%d %s", code, errs)
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
machine: h1
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
		json.Unmarshal([]byte(out), &c)
		return c.Mirror
	}
	if w, p := mirror("work"), mirror("private"); w == p || filepath.Dir(w) != filepath.Join(d, "cache", "wikictl") {
		t.Errorf("mirrors: work %q, private %q", w, p)
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
	if code, _, errs := runCLI(t, cfg, "", "get", "global/index.md"); code != 0 {
		t.Fatalf("get code=%d %s", code, errs)
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
