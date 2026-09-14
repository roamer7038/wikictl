package cli

import (
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
	if code, out, _ := runNoConfig(t, "find", "-h"); code != ExitOK || !strings.Contains(out, "Usage: wikictl find") {
		t.Errorf("find -h: code=%d out=%q", code, out)
	}
}
