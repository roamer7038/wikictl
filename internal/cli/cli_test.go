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
	if code, _, _ = runCLI(t, cfg, "# no fm\n", "put", "global/bad.md"); code != 4 {
		t.Errorf("missing summary code=%d", code)
	}
	if code, _, _ = runCLI(t, cfg, "---\nsummary: a\n---\n", "put", "global/Bad.md"); code != 4 {
		t.Errorf("bad slug code=%d", code)
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
	runCLI(t, cfg, "---\nsummary: b\n---\n# b\n[gone](gone.md)\n\n## Links\n- x\n", "put", "global/broken.md")
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
	if code, _, errs := runCLI(t, cfg, "", "mv", "projects/app2/", "global/x.md"); code != 2 || !strings.Contains(errs, "both arguments") {
		t.Errorf("mixed dir/page mv must be a usage error: code=%d errs=%q", code, errs)
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
