package cli

import (
	"os"
	"path/filepath"
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
	work := filepath.Join(filepath.Dir(cfg), "work")
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# wiki\n"), 0o644)
	mustRun(t, work, "git", "add", "-A")
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "readme")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	if code, _, errs := runCLI(t, cfg, "x", "put", "global/img/logo.png"); code != 0 {
		t.Fatalf("put: %s", errs)
	}
	// A file at the root is rejected; a name at the root that does not exist is only missing.
	if code, _, errs := runCLI(t, cfg, "", "rm", "README.md"); code != ExitInvalid || !strings.Contains(errs, "bad_path") {
		t.Errorf("rm of a root file: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "rm", "projcts"); code != ExitError || errs != "wikictl: projcts: no such file or directory\n" {
		t.Errorf("rm of a missing root name: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "rm", "-rf", "projcts"); code != 0 || errs != "" {
		t.Errorf("rm -rf of a missing root name: code=%d errs=%q", code, errs)
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
	if _, out, _ := runCLI(t, cfg, "", "ls"); out != "README.md\nmachines/\n" {
		t.Errorf("ls after rm -r: %q", out)
	}

	if code, out, _ := runCLI(t, cfg, "", "rm", "--json", "-f", "none/x.md"); code != 0 || out != `{"commit":"","paths":[]}`+"\n" {
		t.Errorf("rm -f of a missing path: code=%d out=%q", code, out)
	}
	if code, _, errs := runCLI(t, cfg, "", "rm", "machines"); code != ExitError || !strings.Contains(errs, "machines: is a directory") {
		t.Errorf("rm of a directory: code=%d errs=%q", code, errs)
	}
}

func TestCommitMessage(t *testing.T) {
	if got := commitMessage("rm", []string{"global/a.md", "global/b"}); got != "wikictl: rm global/a.md global/b" {
		t.Errorf("short: %q", got)
	}
	long := make([]string, 30)
	for i := range long {
		long[i] = "global/some-long-page-name.md"
	}
	if got := commitMessage("rm", long); got != "wikictl: rm global/some-long-page-name.md and 29 more" {
		t.Errorf("long: %q", got)
	}
}

// TestWriteOverDirectoryOrFile checks that a write never replaces a directory
// with a file or puts a file below a path that is a file.
func TestWriteOverDirectoryOrFile(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	for _, p := range []string{"global/d.md/z.md", "global/d.md/w.md"} {
		if code, _, errs := runCLI(t, cfg, "---\nsummary: d\n---\n# d\n", "put", p); code != 0 {
			t.Fatalf("put %s: %s", p, errs)
		}
	}
	head := gitOut(t, "--git-dir", remote, "rev-parse", "main")
	for _, c := range []struct {
		args []string
		errs string
	}{
		{[]string{"mv", "global/d.md/z.md", "global/d.md"}, "wikictl: global/d.md: is a directory\n"},
		{[]string{"put", "projects/app"}, "wikictl: projects/app: is a directory\n"},
		{[]string{"put", "global/push.md/child.png"}, "wikictl: global/push.md/child.png: global/push.md is a file\n"},
		{[]string{"mv", "global/index.md", "global/push.md/index.md"}, "wikictl: global/push.md/index.md: global/push.md is a file\n"},
	} {
		if code, _, errs := runCLI(t, cfg, "x", c.args...); code != ExitError || !strings.HasSuffix(errs, c.errs) {
			t.Errorf("%v: code=%d errs=%q", c.args, code, errs)
		}
	}
	if got := gitOut(t, "--git-dir", remote, "rev-parse", "main"); got != head {
		t.Errorf("a rejected write moved the remote branch: %s -> %s", head, got)
	}
}
