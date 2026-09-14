package cli

import (
	"strings"
	"testing"
)

func TestPutFiles(t *testing.T) {
	cfg := setup(t)
	png := "\x89PNG\r\n\x1a\n\x00binary"
	if code, out, errs := runCLI(t, cfg, png, "put", "global/img/logo.png"); code != 0 || out != "" || errs != "" {
		t.Errorf("put png: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, _ := runCLI(t, cfg, "", "cat", "global/img/logo.png"); code != 0 || out != png {
		t.Errorf("cat png: code=%d out=%q", code, out)
	}
	for _, p := range []string{"logo.png", "global/.hidden", "global/a b.txt"} {
		if code, _, errs := runCLI(t, cfg, "x", "put", p); code != ExitInvalid || !strings.Contains(errs, "bad_path") {
			t.Errorf("put %q: code=%d errs=%q", p, code, errs)
		}
	}
	code, out, errs := runCLI(t, cfg, "---\nsummary: v\n---\n# v\n", "put", "-v", "global/v.md")
	f := strings.Split(strings.TrimSuffix(out, "\n"), "\t")
	if code != 0 || len(f) != 3 || f[0] != "global/v.md" || len(f[1]) != 40 || len(f[2]) != 40 {
		t.Errorf("put -v: code=%d out=%q errs=%q", code, out, errs)
	}
}

func TestRm(t *testing.T) {
	cfg := setup(t)
	if code, _, errs := runCLI(t, cfg, "x", "put", "global/img/logo.png"); code != 0 {
		t.Fatalf("put: %s", errs)
	}
	// A directory without -r and a missing path are reported; the other paths are deleted.
	code, out, errs := runCLI(t, cfg, "", "rm", "global", "projects/app/x.md", "global/none.md")
	if code != ExitError || out != "" || errs != "wikictl: global: is a directory\nwikictl: global/none.md: no such file or directory\n" {
		t.Errorf("rm: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "projects/app/x.md"); code != ExitError {
		t.Error("projects/app/x.md was not deleted")
	}
	if code, _, _ := runCLI(t, cfg, "", "cat", "global/push.md"); code != 0 {
		t.Error("global was deleted without -r")
	}

	code, out, errs = runCLI(t, cfg, "", "rm", "-rfv", "global", "global/push.md", "none/x.md")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || errs != "" || len(lines) != 3 {
		t.Fatalf("rm -rfv: code=%d out=%q errs=%q", code, out, errs)
	}
	commit := strings.TrimPrefix(lines[0], "global/img/logo.png\t")
	if len(commit) != 40 || lines[1] != "global/index.md\t"+commit || lines[2] != "global/push.md\t"+commit {
		t.Errorf("rm -rfv: %q", out)
	}
	if got := lastCommitMessage(t, cfg); got != "wikictl: rm global global/push.md none/x.md" {
		t.Errorf("commit message: %q", got)
	}
	if _, out, _ := runCLI(t, cfg, "", "ls"); out != "machines/\n" {
		t.Errorf("ls after rm -r: %q", out)
	}

	if code, out, _ := runCLI(t, cfg, "", "rm", "--json", "-f", "none/x.md"); code != 0 || out != `{"commit":"","paths":[]}`+"\n" {
		t.Errorf("rm -f of a missing path: code=%d out=%q", code, out)
	}
	if code, _, errs := runCLI(t, cfg, "", "rm", "machines"); code != ExitError || !strings.Contains(errs, "machines: is a directory") {
		t.Errorf("rm of a directory: code=%d errs=%q", code, errs)
	}
}

func TestMvArguments(t *testing.T) {
	cfg := setup(t)
	for p, c := range map[string]string{
		"global/img/logo.png": "png",
		"projects/app/y.md":   "---\nsummary: y\n---\n# y\n[x](x.md)\n",
	} {
		if code, _, errs := runCLI(t, cfg, c, "put", p); code != 0 {
			t.Fatalf("put %s: %s", p, errs)
		}
	}
	// Several sources move into a directory, and files that are not pages move with theirs.
	code, out, errs := runCLI(t, cfg, "", "mv", "-v", "global/img", "projects/app/x.md", "machines")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || errs != "" || len(lines) != 2 {
		t.Fatalf("mv into a directory: code=%d out=%q errs=%q", code, out, errs)
	}
	commit := strings.TrimPrefix(lines[0], "global/img/logo.png\tmachines/img/logo.png\t")
	if len(commit) != 40 || lines[1] != "projects/app/x.md\tmachines/x.md\t"+commit {
		t.Errorf("mv -v: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "cat", "projects/app/y.md"); !strings.Contains(out, "[x](../../machines/x.md)") {
		t.Errorf("link to the moved page: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "cat", "machines/img/logo.png"); out != "png" {
		t.Errorf("moved file: %q", out)
	}

	// -T renames to a path that must not exist; -t names the directory first.
	if code, _, errs := runCLI(t, cfg, "", "mv", "-T", "machines/img", "machines/h1"); code != ExitError || errs != "wikictl: machines/h1: not replacing\n" {
		t.Errorf("mv -T onto a directory: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "mv", "-t", "global", "machines/img", "machines/x.md"); code != 0 {
		t.Errorf("mv -t: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "stat", "global/img/logo.png", "global/x.md"); code != 0 {
		t.Error("mv -t did not move the sources")
	}

	// A missing source is reported, and the other sources are still moved.
	code, _, errs = runCLI(t, cfg, "", "mv", "global/none.md", "global/x.md", "projects")
	if code != ExitError || errs != "wikictl: global/none.md: no such file or directory\n" {
		t.Errorf("mv with a missing source: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "stat", "projects/x.md"); code != 0 {
		t.Error("the other source was not moved")
	}

	for _, c := range []struct {
		args []string
		code int
		errs string
	}{
		{[]string{"mv", "global/index.md", "global/push.md", "global/new.md"}, ExitError, "global/new.md: not a directory"},
		{[]string{"mv", "global", "global/sub"}, ExitError, "global: cannot move a directory into itself"},
		{[]string{"mv", "global/index.md", "index.md"}, ExitInvalid, "bad_path"},
		{[]string{"mv", "global/index.md"}, ExitUsage, "missing destination"},
		{[]string{"mv", "-t", "global", "-T", "projects/x.md"}, ExitUsage, "-t and -T cannot be combined"},
		{[]string{"mv", "-T", "global/index.md", "global/push.md", "projects"}, ExitUsage, "-T takes one source"},
	} {
		if code, _, errs := runCLI(t, cfg, "", c.args...); code != c.code || !strings.Contains(errs, c.errs) {
			t.Errorf("%v: code=%d errs=%q", c.args, code, errs)
		}
	}
}
