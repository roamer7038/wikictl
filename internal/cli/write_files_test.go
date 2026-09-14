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
