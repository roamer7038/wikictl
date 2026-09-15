package cli

import (
	"os"
	"path/filepath"
	"runtime"
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
		{[]string{"mv", "global/push.md", "global/new/"}, ExitError, "global/new/: not a directory"},
		{[]string{"mv", "global/push.md", "global/index.md/x.md"}, ExitError, "global/index.md/x.md: not a directory"},
		{[]string{"mv", "-T", "global", "."}, ExitInvalid, "the root of the wiki cannot be replaced"},
		{[]string{"mv", "global/index.md", "index.md"}, ExitInvalid, "bad_path"},
		{[]string{"mv", "global/index.md"}, ExitUsage, "missing destination"},
		{[]string{"mv", "-t", "global", "-T", "projects/x.md"}, ExitUsage, "-t and -T cannot be combined"},
		{[]string{"mv", "-T", "global/index.md", "global/push.md", "projects"}, ExitUsage, "-T takes one source"},
	} {
		if code, _, errs := runCLI(t, cfg, "", c.args...); code != c.code || !strings.Contains(errs, c.errs) {
			t.Errorf("%v: code=%d errs=%q", c.args, code, errs)
		}
	}

	// A source already moved with an earlier source is reported as missing.
	code, _, errs = runCLI(t, cfg, "", "mv", "global", "global/push.md", "machines")
	if code != ExitError || errs != "wikictl: global/push.md: no such file or directory\n" {
		t.Errorf("mv of overlapping sources: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "stat", "machines/global/push.md", "machines/global/index.md"); code != 0 {
		t.Error("the directory was not moved with all its files")
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
		{[]string{"mv", "global/d.md/z.md", "global/d.md"}, "wikictl: global/d.md/z.md: not replacing\n"},
		{[]string{"put", "projects/app"}, "wikictl: projects/app: is a directory\n"},
		{[]string{"put", "global/push.md/child.png"}, "wikictl: global/push.md/child.png: global/push.md is a file\n"},
		{[]string{"mv", "global/index.md", "global/push.md/index.md"}, "wikictl: global/push.md/index.md: not a directory\n"},
	} {
		if code, _, errs := runCLI(t, cfg, "x", c.args...); code != ExitError || !strings.HasSuffix(errs, c.errs) {
			t.Errorf("%v: code=%d errs=%q", c.args, code, errs)
		}
	}
	if got := gitOut(t, "--git-dir", remote, "rev-parse", "main"); got != head {
		t.Errorf("a rejected write moved the remote branch: %s -> %s", head, got)
	}

	// A submodule is neither replaced nor turned into a directory.
	work := filepath.Join(filepath.Dir(cfg), "work")
	mustRun(t, work, "git", "pull", "-q", "origin", "main")
	mustRun(t, work, "git", "update-index", "--add", "--cacheinfo", "160000,"+head+",global/sub")
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "submodule")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	head = gitOut(t, "--git-dir", remote, "rev-parse", "main")
	for p, errs := range map[string]string{
		"global/sub":       "wikictl: global/sub: is a submodule\n",
		"global/sub/x.png": "wikictl: global/sub/x.png: global/sub is a file\n",
	} {
		if code, _, got := runCLI(t, cfg, "x", "put", p); code != ExitError || got != errs {
			t.Errorf("put %s: code=%d errs=%q", p, code, got)
		}
	}
	if got := gitOut(t, "--git-dir", remote, "rev-parse", "main"); got != head {
		t.Errorf("a rejected write over a submodule moved the remote branch: %s -> %s", head, got)
	}
}

// TestNonRegularFiles checks that writes keep the mode of executables and
// symbolic links, refuse to write over a symbolic link or to move a
// submodule, and delete both.
func TestNonRegularFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links")
	}
	cfg := setup(t)
	remote, work := filepath.Join(filepath.Dir(cfg), "remote.git"), filepath.Join(filepath.Dir(cfg), "work")
	os.MkdirAll(filepath.Join(work, "tools"), 0o755)
	os.WriteFile(filepath.Join(work, "tools", "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.Symlink("../global/push.md", filepath.Join(work, "tools", "push.md"))
	os.Symlink("push.md", filepath.Join(work, "global", "link.md"))
	os.Symlink("../projects", filepath.Join(work, "global", "linkdir"))
	os.WriteFile(filepath.Join(work, "global", "exec.md"), []byte("---\nsummary: e\n---\n# e\n[l](link.md)\n"), 0o755)
	mustRun(t, work, "git", "add", "-A")
	sub := "160000," + gitOut(t, "--git-dir", remote, "rev-parse", "main")
	mustRun(t, work, "git", "update-index", "--add", "--cacheinfo", sub+",global/sub")
	mustRun(t, work, "git", "update-index", "--add", "--cacheinfo", sub+",mods/lib/sub")
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "special")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	mode := func(p string) string {
		t.Helper()
		if f := strings.Fields(gitOut(t, "--git-dir", remote, "ls-tree", "main", "--", p)); len(f) > 0 {
			return f[0]
		}
		return ""
	}
	blob := func(p string) string { return gitOut(t, "--git-dir", remote, "cat-file", "-p", "main:"+p) }

	// A moved symbolic link keeps its mode and target, and links to it are
	// rewritten in pages that keep their mode.
	for _, args := range [][]string{{"mv", "global/link.md", "projects/link.md"}, {"mv", "tools", "bin"}} {
		if code, _, errs := runCLI(t, cfg, "", args...); code != 0 {
			t.Fatalf("%v: code=%d %s", args, code, errs)
		}
	}
	if m, target := mode("projects/link.md"), blob("projects/link.md"); m != "120000" || target != "push.md" {
		t.Errorf("moved symbolic link: mode %q, target %q", m, target)
	}
	if m, b := mode("global/exec.md"), blob("global/exec.md"); m != "100755" || !strings.Contains(b, "[l](../projects/link.md)") {
		t.Errorf("rewritten executable page: mode %q, content %q", m, b)
	}
	if m1, m2 := mode("bin/run.sh"), mode("bin/push.md"); m1 != "100755" || m2 != "120000" {
		t.Errorf("moved directory: modes %q and %q", m1, m2)
	}
	if code, _, errs := runCLI(t, cfg, "#!/bin/sh\necho hi\n", "put", "--base", shaOf(t, cfg, "bin/run.sh"), "bin/run.sh"); code != 0 || mode("bin/run.sh") != "100755" {
		t.Errorf("put over an executable: code=%d mode %q %s", code, mode("bin/run.sh"), errs)
	}

	for p, want := range map[string]string{
		"projects/link.md":    "wikictl: projects/link.md: is a symbolic link\n",
		"global/linkdir/x.md": "wikictl: global/linkdir/x.md: global/linkdir is a file\n",
	} {
		if code, _, errs := runCLI(t, cfg, "x", "put", p); code != ExitError || !strings.HasSuffix(errs, want) {
			t.Errorf("put %s: code=%d %q", p, code, errs)
		}
	}
	for _, args := range [][]string{{"mv", "global/sub", "global/sub2"}, {"mv", "mods", "machines"}} {
		if code, _, errs := runCLI(t, cfg, "", args...); code != ExitError || !strings.Contains(errs, "cannot move a submodule") {
			t.Errorf("%v: code=%d %q", args, code, errs)
		}
	}
	if m := mode("global/sub"); m != "160000" {
		t.Errorf("submodule after a refused mv: mode %q", m)
	}
	if code, _, errs := runCLI(t, cfg, "", "rm", "-r", "global/sub", "projects/link.md", "mods"); code != 0 || mode("global/sub")+mode("projects/link.md")+mode("mods/lib/sub") != "" {
		t.Errorf("rm of submodules and a symbolic link: code=%d %s", code, errs)
	}
}
