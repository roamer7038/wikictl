package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFind(t *testing.T) {
	cfg := setup(t)
	for args, want := range map[string]string{
		"":                         ".\nglobal\nglobal/index.md\nglobal/push.md\nmachines\nmachines/h1\nmachines/h1/y.md\nprojects\nprojects/app\nprojects/app/x.md\n",
		"global -name *.md":        "global/index.md\nglobal/push.md\n",
		"-maxdepth 1 -type d":      ".\nglobal\nmachines\nprojects\n",
		"-mindepth 1 -maxdepth 1":  "global\nmachines\nprojects\n",
		"-path projects/* -type f": "projects/app/x.md\n",
		"-path p*/*.md":            "projects/app/x.md\n",
		"-name [!i]*.md":           "global/push.md\nmachines/h1/y.md\nprojects/app/x.md\n",
		"global ! -name index.md":  "global\nglobal/push.md\n",
		"-meta tags=git":           "global/push.md\n",
		"-meta type=policy":        "global/push.md\n",
		"-meta status=deprecated":  "machines/h1/y.md\n",
		"-mtime -1 -type f":        "global/index.md\nglobal/push.md\nmachines/h1/y.md\nprojects/app/x.md\n",
		"-mtime +0":                "",
		"-newer global/push.md":    "",
		"/global/push.md machines": "global/push.md\nmachines\nmachines/h1\nmachines/h1/y.md\n",
		"-maxdepth 0 --no-fetch":   ".\n",
		"-name --x":                "",
		"--json -maxdepth 0":       `{"items":[{"path":".","kind":"dir"}]}` + "\n",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"find"}, strings.Fields(args)...)...); code != 0 || out != want {
			t.Errorf("find %s: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}

	// Commit times have a resolution of one second, so the new page is
	// committed an hour later than the others.
	date := time.Now().Add(time.Hour).Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", date)
	t.Setenv("GIT_COMMITTER_DATE", date)
	if code, _, errs := runCLI(t, cfg, "---\nsummary: n\n---\n# n\n", "put", "global/new.md"); code != 0 {
		t.Fatalf("put: %s", errs)
	}
	if _, out, _ := runCLI(t, cfg, "", "find", "-newer", "global/push.md"); out != ".\nglobal\nglobal/new.md\n" {
		t.Errorf("find -newer: %q", out)
	}

	for _, c := range []struct {
		args      []string
		code      int
		out, errs string
	}{
		{[]string{"find", "none", "global", "-maxdepth", "0"}, ExitError, "global\n", "wikictl: none: no such file or directory\n"},
		{[]string{"find", "-newer", "none"}, ExitError, "", "wikictl: none: no such file or directory\n"},
		{[]string{"find", "../x"}, ExitInvalid, "", "bad_path"},
	} {
		if code, out, errs := runCLI(t, cfg, "", c.args...); code != c.code || out != c.out || !strings.Contains(errs, c.errs) {
			t.Errorf("%v: code=%d out=%q errs=%q", c.args, code, out, errs)
		}
	}
}

// TestFindDashDash checks that "--" ends the flags of find, so a path that
// starts with - is searched instead of being read as a primary, matching
// cat and the top-level help, and that an argument spelled like a known
// primary is still read as a primary after it, which is the one limit the
// help names.
func TestFindDashDash(t *testing.T) {
	cfg := setup(t)
	pushFiles(t, cfg, map[string]string{
		"-dash.md": "---\nsummary: dash\n---\n# dash\n",
	})
	if code, out, errs := runCLI(t, cfg, "", "find", "--", "-dash.md"); code != 0 || out != "-dash.md\n" {
		t.Errorf("find -- -dash.md: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "find", "--json", "--", "-dash.md"); code != 0 || out != `{"items":[{"path":"-dash.md","kind":"file"}]}`+"\n" {
		t.Errorf("find --json -- -dash.md: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "find", "global", "--", "-dash.md"); code != 0 || out != "global\nglobal/index.md\nglobal/push.md\n-dash.md\n" {
		t.Errorf("find global -- -dash.md: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "find", "-dash.md"); code != ExitUsage || !strings.Contains(errs, "unknown primary: -dash.md") {
		t.Errorf("find -dash.md without --: code=%d errs=%q", code, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "find", "--", "--json"); code != ExitError || out != "" || !strings.Contains(errs, "--json: no such file or directory") {
		t.Errorf("find -- --json: code=%d out=%q errs=%q", code, out, errs)
	}
	// A path spelled like a known primary is still read as a primary after
	// "--"; -name is what matches such a file by name.
	if code, _, errs := runCLI(t, cfg, "", "find", "--", "-type"); code != ExitUsage || !strings.Contains(errs, "missing argument to -type") {
		t.Errorf("find -- -type: code=%d errs=%q", code, errs)
	}
}

func TestFindUsage(t *testing.T) {
	for args, msg := range map[string]string{
		"-bogus":               "unknown primary: -bogus",
		"-name":                "missing argument to -name",
		"-name [z-a]":          "invalid pattern: [z-a]",
		"-type x":              "-type must be f or d",
		"-maxdepth -1":         "-maxdepth must be a non-negative integer",
		"-mtime x":             "-mtime must be an integer",
		"-meta foo":            "-meta must be KEY=VALUE",
		"global -name x other": "paths must precede the expression: other",
		"!":                    "missing primary after !",
		"--bogus":              "unknown flag: --bogus",
	} {
		code, _, errs := runNoConfig(t, append([]string{"find"}, strings.Fields(args)...)...)
		if code != ExitUsage || !strings.Contains(errs, msg) || !strings.Contains(errs, "Usage: wikictl find") {
			t.Errorf("find %s: code=%d errs=%q", args, code, errs)
		}
	}
	if code, out, _ := runNoConfig(t, "find", "-name", "x", "--json", "-bogus"); code != ExitUsage || !strings.HasPrefix(out, `{"error":"usage"`) {
		t.Errorf("find --json with a usage error: code=%d out=%q", code, out)
	}
	if code, out, errs := runNoConfig(t, "find", "-name", "--json", "--bogus"); code != ExitUsage || out != "" || !strings.Contains(errs, "unknown flag: --bogus") {
		t.Errorf("find with --json as a pattern: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, _ := runNoConfig(t, "find", "-h"); code != ExitOK || !strings.Contains(out, "Usage: wikictl find") {
		t.Errorf("find -h: code=%d out=%q", code, out)
	}
}

// TestFindUsageHints checks that the syntax of GNU find that wikictl does not
// take reports the way to write it, and that a message which already says what
// to do is left as it is.
func TestFindUsageHints(t *testing.T) {
	for args, want := range map[string]string{
		"-iname A*": "wikictl: find: unknown primary: -iname\n" +
			"  -name matches the last element of the path; it is case-sensitive\n",
		"-name a -o -name b": "wikictl: find: unknown primary: -o\n" +
			"  the primaries must all be true; there is no OR\n" +
			"  run find once per pattern, or search the paths with grep\n",
		"-name a -or -name b": "wikictl: find: unknown primary: -or\n" +
			"  the primaries must all be true; there is no OR\n" +
			"  run find once per pattern, or search the paths with grep\n",
		"--type f -name x": "wikictl: find: unknown flag: --type\n" +
			"  a primary takes one dash: write -type, not --type\n",
		"--name x": "wikictl: find: unknown flag: --name\n" +
			"  a primary takes one dash: write -name, not --name\n",
		"-meta k=v projects": "wikictl: find: paths must precede the expression: projects\n",
	} {
		code, _, errs := runNoConfig(t, append([]string{"find"}, strings.Fields(args)...)...)
		if code != ExitUsage || !strings.HasPrefix(errs, want+"Usage: wikictl find") {
			t.Errorf("find %s: code=%d errs=%q, want the prefix %q", args, code, errs, want)
		}
	}
}

// TestFindTimes checks -mtime and -newer against files committed three days
// ago, one with a name that git log quotes, in a directory from which a file
// was deleted since: a deleted file does not change the time of a directory.
func TestFindTimes(t *testing.T) {
	cfg := setup(t)
	work := cloneRemote(t, cfg)
	os.MkdirAll(filepath.Join(work, "old"), 0o755)
	for _, name := range []string{"a.md", `b\`, "gone.md"} {
		os.WriteFile(filepath.Join(work, "old", name), []byte("x\n"), 0o644)
	}
	mustRun(t, work, "git", "add", "-A")
	commit := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "old")
	commit.Dir = work
	date := time.Now().Add(-73 * time.Hour).Format(time.RFC3339)
	commit.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	mustRun(t, work, "git", "rm", "-q", "old/gone.md")
	commitAndPush(t, work)
	for args, want := range map[string]string{
		"old -mtime 3":               "old\nold/a.md\nold/b\\\n",
		"-mtime +2 -type f":          "old/a.md\nold/b\\\n",
		"old -mtime -3":              "",
		"-maxdepth 1 -newer old/b\\": ".\nglobal\nmachines\nprojects\n",
		"-maxdepth 1 -newer old":     ".\nglobal\nmachines\nprojects\n",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"find"}, strings.Fields(args)...)...); code != 0 || out != want {
			t.Errorf("find %s: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
}

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		match         bool
	}{
		{"*.md", "a/b.md", true},
		{"?", "é", true},
		{`\*`, "*", true},
		{`\*`, "a", false},
		{`b\`, `b\`, false},
		{"[]]", "]", true},
		{"[!]]", "]", false},
		{"[!]]", "a", true},
		{`[\]]`, "]", true},
		{`[a\-z]`, "-", true},
		{`[a\-z]`, "b", false},
		{"[a-]", "-", true},
		{"[[:alpha:]]", "a", true},
		{"[[:alpha:]]", "1", false},
		{`[\d]`, "d", true},
		{`[\d]`, "1", false},
		{"[a", "[a", true},
	} {
		re, err := globRegexp(c.pattern)
		if err != nil || re.MatchString(c.name) != c.match {
			t.Errorf("%q against %q: err=%v, want %v", c.pattern, c.name, err, c.match)
		}
	}
}

// TestFindFrontmatter checks --frontmatter: the line of each key is counted
// from the start of the file, the keys can be narrowed, the values are the
// ones -meta compares against, a file that is not a page is covered too, and
// a flow mapping puts every key on one line.
func TestFindFrontmatter(t *testing.T) {
	cfg := setup(t)
	pushFiles(t, cfg, map[string]string{
		"fm/plain.md":   "---\nsummary: s\ntype: adr\ntags: [a, b]\n---\n# p\n",
		"fm/merge.md":   "---\ndefaults: &d\n  type: adr\n  status: draft\nsummary: m\n<<: *d\nstatus: final\n---\n# m\n",
		"fm/nums.md":    "---\nbig: .inf\nsmall: -.inf\nnan: .nan\n---\n# n\n",
		"fm/flow.md":    "---\n{a: 1, b: 2}\n---\n# f\n",
		"fm/empty.md":   "---\n---\n# e\n",
		"fm/none.md":    "# none\n",
		"docs/k8s.yaml": "---\nkind: Deployment\nname: web\n---\n",
	})
	for args, want := range map[string]string{
		// The frontmatter starts on line 2, after the "---" delimiter.
		"fm/plain.md --frontmatter":              "fm/plain.md\t2\t\"summary\"\t\"s\"\nfm/plain.md\t3\t\"type\"\t\"adr\"\nfm/plain.md\t4\t\"tags\"\t[\"a\",\"b\"]\n",
		"fm/plain.md --frontmatter=type":         "fm/plain.md\t3\t\"type\"\t\"adr\"\n",
		"fm/plain.md --frontmatter=type,missing": "fm/plain.md\t3\t\"type\"\t\"adr\"\n",
		"fm/plain.md --frontmatter=missing":      "",
		"fm/none.md --frontmatter":               "",
		"fm/empty.md --frontmatter":              "",
		// The --frontmatter written last wins, in either order.
		"fm/plain.md --frontmatter=type --frontmatter": "fm/plain.md\t2\t\"summary\"\t\"s\"\nfm/plain.md\t3\t\"type\"\t\"adr\"\nfm/plain.md\t4\t\"tags\"\t[\"a\",\"b\"]\n",
		"fm/plain.md --frontmatter --frontmatter=type": "fm/plain.md\t3\t\"type\"\t\"adr\"\n",
		// Every key of a flow mapping is written on the same line.
		"fm/flow.md --frontmatter": "fm/flow.md\t2\t\"a\"\t1\nfm/flow.md\t2\t\"b\"\t2\n",
		// A key that the merge key brought in has the line of the "<<", the
		// "<<" itself is not listed, and a key written at the top level keeps
		// its own line and value.
		"fm/merge.md --frontmatter":             "fm/merge.md\t2\t\"defaults\"\t{\"status\":\"draft\",\"type\":\"adr\"}\nfm/merge.md\t5\t\"summary\"\t\"m\"\nfm/merge.md\t6\t\"type\"\t\"adr\"\nfm/merge.md\t7\t\"status\"\t\"final\"\n",
		"fm/merge.md --frontmatter=status,type": "fm/merge.md\t6\t\"type\"\t\"adr\"\nfm/merge.md\t7\t\"status\"\t\"final\"\n",
		// A file that is not a page has its frontmatter read as well.
		"docs/k8s.yaml --frontmatter": "docs/k8s.yaml\t2\t\"kind\"\t\"Deployment\"\ndocs/k8s.yaml\t3\t\"name\"\t\"web\"\n",
		// The text output is a list of the frontmatter keys: a directory and a
		// file without the key are not printed at all.
		"fm --frontmatter=summary": "fm/merge.md\t5\t\"summary\"\t\"m\"\nfm/plain.md\t2\t\"summary\"\t\"s\"\n",
		// The values agree with what -meta compares against.
		"-meta status=final":  "fm/merge.md\n",
		"fm -meta type=adr":   "fm/merge.md\nfm/plain.md\n",
		"fm -meta status=adr": "",
	} {
		if code, out, errs := runCLI(t, cfg, "", append([]string{"find"}, strings.Fields(args)...)...); code != ExitOK || out != want {
			t.Errorf("find %s: code=%d out=%q, want %q, errs=%q", args, code, out, want, errs)
		}
	}

	// --json distinguishes a file without frontmatter, which has no
	// frontmatter field, from one whose frontmatter has no key, which has an
	// empty list. A number that is not finite is a string, so the output is
	// valid JSON. A directory never has the field.
	for args, want := range map[string]string{
		"fm/none.md":                     `{"items":[{"path":"fm/none.md","kind":"file"}]}`,
		"fm/empty.md":                    `{"items":[{"path":"fm/empty.md","kind":"file","frontmatter":[]}]}`,
		"fm/nums.md":                     `{"items":[{"path":"fm/nums.md","kind":"file","frontmatter":[{"key":"big","line":2,"value":".inf"},{"key":"small","line":3,"value":"-.inf"},{"key":"nan","line":4,"value":".nan"}]}]}`,
		"fm/plain.md --frontmatter=tags": `{"items":[{"path":"fm/plain.md","kind":"file","frontmatter":[{"key":"tags","line":4,"value":["a","b"]}]}]}`,
		"fm -maxdepth 0":                 `{"items":[{"path":"fm","kind":"dir"}]}`,
	} {
		argv := append([]string{"find", "--json"}, strings.Fields(args)...)
		if !strings.Contains(args, "--frontmatter") {
			argv = append(argv, "--frontmatter")
		}
		code, out, errs := runCLI(t, cfg, "", argv...)
		if code != ExitOK || out != want+"\n" {
			t.Errorf("%v: code=%d out=%q, want %q, errs=%q", argv, code, out, want, errs)
		}
	}

	// The keys follow an "=": a key written as a separate argument is a path,
	// and an "=" with no key is a usage error, so "--frontmatter" on its own is
	// the only way to print every key.
	if code, out, errs := runCLI(t, cfg, "", "find", "fm/plain.md", "--frontmatter", "status"); code != ExitError ||
		errs != "wikictl: status: no such file or directory\n" {
		t.Errorf("--frontmatter with a separate key: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, errs := runCLI(t, cfg, "", "find", "fm/plain.md", "--frontmatter="); code != ExitUsage ||
		!strings.Contains(errs, "a key must not be empty") {
		t.Errorf("--frontmatter with no key: code=%d out=%q errs=%q", code, out, errs)
	}
}

// TestFindFrontmatterErrors checks that a frontmatter that does not parse and
// a file over the size limit are reported as frontmatter_error with --json
// and as a warning in text output, and that neither changes the exit code.
func TestFindFrontmatterErrors(t *testing.T) {
	cfg := setup(t)
	pushFiles(t, cfg, map[string]string{
		"fm/bad.md": "---\nsummary: [\n---\n# b\n",
		"fm/ok.md":  "---\nsummary: ok\n---\n# ok\n",
		"fm/big.md": "---\nsummary: big\n---\n# big\n" + strings.Repeat("x", 1<<20),
	})
	code, out, errs := runCLI(t, cfg, "", "find", "fm", "--frontmatter=summary")
	if code != ExitOK || out != "fm/ok.md\t2\t\"summary\"\t\"ok\"\n" ||
		!strings.Contains(errs, "wikictl: warning: fm/bad.md:1: frontmatter_invalid: ") ||
		!strings.Contains(errs, "wikictl: warning: fm/big.md:0: page_too_large: ") {
		t.Errorf("text output: code=%d out=%q errs=%q", code, out, errs)
	}
	code, out, errs = runCLI(t, cfg, "", "find", "--json", "fm", "--frontmatter")
	var res struct {
		Items []struct {
			Path             string
			Frontmatter      []struct{ Key string }
			FrontmatterError *struct{ Code, Message string } `json:"frontmatter_error"`
		}
	}
	mustUnmarshal(t, out, &res)
	codes := map[string]string{}
	for _, it := range res.Items {
		if it.FrontmatterError != nil {
			codes[it.Path] = it.FrontmatterError.Code
			if it.Frontmatter != nil || it.FrontmatterError.Message == "" {
				t.Errorf("%s: %+v", it.Path, it)
			}
		}
	}
	if code != ExitOK || errs != "" || len(codes) != 2 ||
		codes["fm/bad.md"] != "frontmatter_invalid" || codes["fm/big.md"] != "page_too_large" {
		t.Errorf("json output: code=%d out=%q errs=%q codes=%v", code, out, errs, codes)
	}
}

func TestMetaHas(t *testing.T) {
	for _, c := range []struct {
		v     any
		want  string
		match bool
	}{
		{"x", "x", true},
		{true, "true", true},
		{1.5, "1.5", true},
		{1000000.0, "1000000", true},
		{uint64(7), "007", true},
		{uint64(7), "seven", false},
		{[]any{"a", uint64(1)}, "1", true},
		{[]any{[]any{"a"}}, "[a]", false},
		{map[string]any{"a": 1}, "map[a:1]", false},
		{nil, "<nil>", false},
	} {
		if got := metaHas(c.v, c.want); got != c.match {
			t.Errorf("metaHas(%#v, %q) = %v", c.v, c.want, got)
		}
	}
}
