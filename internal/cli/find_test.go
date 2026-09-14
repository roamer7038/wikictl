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

	// Commit times have a resolution of one second.
	time.Sleep(1100 * time.Millisecond)
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

// TestFindTimes checks -mtime and -newer against files committed three days
// ago, one with a name that git log quotes, in a directory from which a file
// was deleted since: a deleted file does not change the time of a directory.
func TestFindTimes(t *testing.T) {
	cfg := setup(t)
	work := filepath.Join(filepath.Dir(cfg), "work")
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
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "gone")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
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
