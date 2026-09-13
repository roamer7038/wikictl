package cli

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// update rewrites the golden files instead of comparing with them. The flag
// is defined only in this package, so name the package when updating:
//
//	go test ./internal/cli -run Golden -update
//
// Review the resulting diff of testdata/golden before committing it.
var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// goldenDir is absolute because the golden tests change the working
// directory, so that the current directory has no origin remote.
var goldenDir = func() string {
	d, err := filepath.Abs(filepath.Join("testdata", "golden"))
	if err != nil {
		panic(err)
	}
	return d
}()

// goldenCase is one invocation whose exit code, stdout and stderr are
// compared with testdata/golden/<name>.golden.
type goldenCase struct {
	name  string
	stdin string
	// args may contain "<sha:path>", replaced by the blob sha of path.
	args []string
}

// goldenReadFiles extends seedFiles with pages that give every read command
// something to report. All pages are in one commit, so that the order of
// search results does not depend on commit times.
var goldenReadFiles = func() map[string]string {
	files := maps.Clone(seedFiles)
	files["global/broken.md"] = "---\nsummary: broken\n---\n# broken\n[gone](gone.md)\n\n## Links\n- x\n"
	files["global/Style_Name.md"] = "# style\n"
	files["projects/app/sub/z.md"] = "---\nsummary: z\ntype: note\n---\n# z\n[x](../x.md)\n"
	return files
}()

// goldenReadCases do not change the wiki and share one fixture.
var goldenReadCases = []goldenCase{
	{name: "search/json", args: []string{"search", "--json", "--dirs", "global,projects/app,machines/h1", "lease"}},
	{name: "search/text", args: []string{"search", "--dirs", "global,projects/app,machines/h1", "lease"}},
	{name: "search/all_json", args: []string{"search", "--json", "--all", "--dirs", "global,projects/app,machines/h1", "lease"}},
	{name: "search/any_json", args: []string{"search", "--json", "--any", "--dirs", ".", "lease", "push"}},
	{name: "search/no_match_json", args: []string{"search", "--json", "--dirs", "global", "zzz-none"}},
	{name: "search/no_match_text", args: []string{"search", "--dirs", "global", "zzz-none"}},
	{name: "search/usage_json", args: []string{"search", "--json"}},
	{name: "search/usage_text", args: []string{"search"}},
	{name: "search/bad_flag_text", args: []string{"search", "--bogus", "lease"}},

	{name: "get/json", args: []string{"get", "--json", "global/push.md"}},
	{name: "get/text", args: []string{"get", "global/push.md"}},
	{name: "get/backlinks_json", args: []string{"get", "--json", "global/index.md"}},
	{name: "get/backlinks_text", args: []string{"get", "global/index.md"}},
	{name: "get/not_found_json", args: []string{"get", "--json", "global/none.md"}},
	{name: "get/not_found_text", args: []string{"get", "global/none.md"}},
	{name: "get/usage_json", args: []string{"get", "--json"}},
	{name: "get/usage_text", args: []string{"get"}},

	{name: "ls/json", args: []string{"ls", "--json", "--dirs", "global"}},
	{name: "ls/text", args: []string{"ls", "--dirs", "global"}},
	{name: "ls/all_json", args: []string{"ls", "--json", "--all", "--dirs", "."}},
	{name: "ls/tag_json", args: []string{"ls", "--json", "--dirs", "global", "--tag", "git"}},
	{name: "ls/type_text", args: []string{"ls", "--dirs", ".", "--type", "note"}},
	{name: "ls/usage_text", args: []string{"ls", "extra"}},

	{name: "lint/json", args: []string{"lint", "--json", "--dirs", "global"}},
	{name: "lint/text", args: []string{"lint", "--dirs", "global"}},
	{name: "lint/clean_json", args: []string{"lint", "--json", "--dirs", "projects/app"}},
	{name: "lint/clean_text", args: []string{"lint", "--dirs", "projects/app"}},
	{name: "lint/path_text", args: []string{"lint", "global/broken.md"}},
	{name: "lint/not_found_json", args: []string{"lint", "--json", "global/none.md"}},
	{name: "lint/not_found_text", args: []string{"lint", "global/none.md"}},

	{name: "dirs/json", args: []string{"dirs", "--json"}},
	{name: "dirs/text", args: []string{"dirs"}},
	{name: "dirs/args_json", args: []string{"dirs", "--json", "projects", "machines/"}},
	{name: "dirs/no_match_json", args: []string{"dirs", "--json", "none"}},
	{name: "dirs/outside_json", args: []string{"dirs", "--json", "../x"}},
	{name: "dirs/outside_text", args: []string{"dirs", "../x"}},
	{name: "dirs/page_text", args: []string{"dirs", "global/index.md"}},

	{name: "context/json", args: []string{"context", "--json"}},
	{name: "context/text", args: []string{"context"}},
	{name: "context/dirs_json", args: []string{"context", "--json", "--dirs", "global,projects,.,projects/none"}},
	{name: "context/dirs_text", args: []string{"context", "--dirs", "global,projects,.,projects/none"}},
	{name: "context/usage_text", args: []string{"context", "extra"}},

	{name: "main/unknown_command_json", args: []string{"--json", "bogus"}},
	{name: "main/unknown_command_text", args: []string{"bogus"}},
	{name: "main/bad_flag_json", args: []string{"--json", "--bogus", "ls"}},
}

// goldenWriteCases each run on a new wiki created by setup.
var goldenWriteCases = []goldenCase{
	{name: "put/new_json", stdin: "---\nsummary: new page\n---\n# n\n", args: []string{"put", "--json", "global/new.md"}},
	{name: "put/new_text", stdin: "---\nsummary: new page\n---\n# n\n", args: []string{"put", "global/new.md"}},
	{name: "put/update_json", stdin: "---\nsummary: x2\n---\n# x\n", args: []string{"put", "--json", "--base", "<sha:projects/app/x.md>", "projects/app/x.md"}},
	{name: "put/warnings_json", stdin: "# w\n[g](gone.md)\n", args: []string{"put", "--json", "global/Warn_Name.md"}},
	{name: "put/warnings_text", stdin: "# w\n[g](gone.md)\n", args: []string{"put", "global/Warn_Name.md"}},
	{name: "put/conflict_exists_json", stdin: "---\nsummary: x\n---\n", args: []string{"put", "--json", "global/index.md"}},
	{name: "put/conflict_exists_text", stdin: "---\nsummary: x\n---\n", args: []string{"put", "global/index.md"}},
	{name: "put/conflict_changed_json", stdin: "---\nsummary: x\n---\n", args: []string{"put", "--json", "--base", "0123456789abcdef0123456789abcdef01234567", "global/index.md"}},
	{name: "put/conflict_changed_text", stdin: "---\nsummary: x\n---\n", args: []string{"put", "--base", "0123456789abcdef0123456789abcdef01234567", "global/index.md"}},
	{name: "put/bad_path_json", stdin: "---\nsummary: a\n---\n", args: []string{"put", "--json", "global/my page.md"}},
	{name: "put/bad_path_text", stdin: "---\nsummary: a\n---\n", args: []string{"put", "global/my page.md"}},
	{name: "put/frontmatter_invalid_json", stdin: "---\nsummary: [\n---\n", args: []string{"put", "--json", "global/bad.md"}},
	{name: "put/frontmatter_invalid_text", stdin: "---\nsummary: [\n---\n", args: []string{"put", "global/bad.md"}},
	{name: "put/usage_json", args: []string{"put", "--json"}},
	{name: "put/usage_text", args: []string{"put"}},
	{name: "put/bad_flag_text", args: []string{"put", "--bogus", "global/x.md"}},

	{name: "mv/page_json", args: []string{"mv", "--json", "global/index.md", "global/start.md"}},
	{name: "mv/page_text", args: []string{"mv", "global/index.md", "global/start.md"}},
	{name: "mv/style_name_text", args: []string{"mv", "global/push.md", "global/Push.md"}},
	{name: "mv/dir_json", args: []string{"mv", "--json", "global/", "shared/"}},
	{name: "mv/dir_text", args: []string{"mv", "global/", "shared/"}},
	{name: "mv/not_found_json", args: []string{"mv", "--json", "global/none.md", "global/other.md"}},
	{name: "mv/not_found_text", args: []string{"mv", "global/none.md", "global/other.md"}},
	{name: "mv/exists_json", args: []string{"mv", "--json", "global/push.md", "global/index.md"}},
	{name: "mv/exists_text", args: []string{"mv", "global/push.md", "global/index.md"}},
	{name: "mv/dir_not_found_text", args: []string{"mv", "none/", "other/"}},
	{name: "mv/dir_exists_json", args: []string{"mv", "--json", "projects/app/", "global/"}},
	{name: "mv/dir_exists_text", args: []string{"mv", "projects/app/", "global/"}},
	{name: "mv/bad_path_json", args: []string{"mv", "--json", "global/push.md", "global/a b.md"}},
	{name: "mv/bad_path_text", args: []string{"mv", "global/push.md", "global/a b.md"}},
	{name: "mv/dir_bad_path_text", args: []string{"mv", "../", "x/"}},
	{name: "mv/mixed_json", args: []string{"mv", "--json", "projects/app/", "global/x.md"}},
	{name: "mv/mixed_text", args: []string{"mv", "projects/app/", "global/x.md"}},
	{name: "mv/usage_text", args: []string{"mv", "global/push.md"}},

	{name: "rm/json", args: []string{"rm", "--json", "global/push.md"}},
	{name: "rm/text", args: []string{"rm", "global/push.md"}},
	{name: "rm/not_found_json", args: []string{"rm", "--json", "global/none.md"}},
	{name: "rm/not_found_text", args: []string{"rm", "global/none.md"}},
	{name: "rm/usage_text", args: []string{"rm"}},

	{name: "init/exists_json", args: []string{"init", "--json"}},
	{name: "init/exists_text", args: []string{"init"}},
}

// goldenInitCases each run on a new empty repository.
var goldenInitCases = []goldenCase{
	{name: "init/json", args: []string{"init", "--json"}},
	{name: "init/text", args: []string{"init"}},
}

func TestGoldenRead(t *testing.T) {
	chdirOutsideRepo(t)
	cfg := setupFiles(t, goldenReadFiles)
	for _, c := range goldenReadCases {
		t.Run(c.name, func(t *testing.T) { runGolden(t, cfg, c) })
	}
}

func TestGoldenWrite(t *testing.T) {
	chdirOutsideRepo(t)
	for _, c := range goldenWriteCases {
		t.Run(c.name, func(t *testing.T) { runGolden(t, setup(t), c) })
	}
}

func TestGoldenInit(t *testing.T) {
	chdirOutsideRepo(t)
	for _, c := range goldenInitCases {
		t.Run(c.name, func(t *testing.T) { runGolden(t, emptyWiki(t), c) })
	}
}

// chdirOutsideRepo moves to a new temporary directory that git does not treat
// as part of a repository, even when TMPDIR is inside one, so that the
// project and remote reported by context do not depend on where the tests run.
func chdirOutsideRepo(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(d))
	t.Chdir(d)
}

// emptyWiki creates an empty bare repository and returns the path of a config
// file that points to it.
func emptyWiki(t *testing.T) (cfgPath string) {
	t.Helper()
	isolateGit(t)
	t.Setenv("WIKICTL_PROFILE", "")
	d := t.TempDir()
	remote := filepath.Join(d, "remote.git")
	mustRun(t, "", "git", "init", "-q", "--bare", "-b", "main", remote)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(d, "cache"))
	cfgPath = filepath.Join(d, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("repo: "+remote+"\nauthor: {name: agent, email: a@a}\nmachine: h1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

var shaPlaceholder = regexp.MustCompile(`^<sha:(.+)>$`)

// runGolden runs c against the wiki of cfg and compares the normalized
// result with its golden file, or rewrites the file with -update.
func runGolden(t *testing.T, cfg string, c goldenCase) {
	t.Helper()
	args := make([]string, len(c.args))
	for i, a := range c.args {
		if m := shaPlaceholder.FindStringSubmatch(a); m != nil {
			a = getSha(t, cfg, m[1])
		}
		args[i] = a
	}
	code, stdout, stderr := runCLI(t, cfg, c.stdin, args...)
	got := normalizeGolden(renderGolden(c, code, stdout, stderr), filepath.Dir(cfg))
	file := filepath.Join(goldenDir, filepath.FromSlash(c.name)+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v; run \"go test ./internal/cli -run Golden -update\" to create it", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s; run \"go test ./internal/cli -run Golden -update\" if the change is intended\n--- got\n%s--- want\n%s", file, got, want)
	}
}

// renderGolden formats one invocation. Each section starts on its own line;
// output without a trailing newline runs into the next section header.
func renderGolden(c goldenCase, code int, stdout, stderr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$ wikictl %s\n", strings.Join(c.args, " "))
	if c.stdin != "" {
		b.WriteString("-- stdin --\n" + c.stdin)
	}
	fmt.Fprintf(&b, "-- exit %d --\n", code)
	b.WriteString("-- stdout --\n" + stdout)
	b.WriteString("-- stderr --\n" + stderr)
	return b.String()
}

var (
	goldenSHA  = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
	goldenTime = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\b`)
)

// normalizeGolden replaces the values that change between runs: the
// temporary directory tmp (also in the form used for mirror names), shas and
// timestamps.
func normalizeGolden(s, tmp string) string {
	mirrorName := strings.NewReplacer("/", "_", ":", "_", "@", "_", "\\", "_").Replace(tmp)
	s = strings.ReplaceAll(s, tmp, "<tmp>")
	s = strings.ReplaceAll(s, mirrorName, "<tmp>")
	s = goldenSHA.ReplaceAllString(s, "<sha>")
	return goldenTime.ReplaceAllString(s, "<time>")
}
