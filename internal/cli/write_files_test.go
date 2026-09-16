package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	code, out, errs := runCLI(t, cfg, "---\nsummary: v\n---\n# v\n", "put", "-v", "global/v.md")
	f := strings.Split(strings.TrimSuffix(out, "\n"), "\t")
	if code != 0 || len(f) != 3 || f[0] != "global/v.md" || len(f[1]) != 40 || len(f[2]) != 40 {
		t.Errorf("put -v: code=%d out=%q errs=%q", code, out, errs)
	}
}

func TestRm(t *testing.T) {
	cfg := setup(t)
	pushFiles(t, cfg, map[string]string{"README.md": "# wiki\n"})
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
	// As GNU rm does, -f without any path deletes nothing and succeeds, while
	// without -f a path is required.
	if code, out, errs := runCLI(t, cfg, "", "rm", "-f"); code != 0 || out != "" || errs != "" {
		t.Errorf("rm -f without paths: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, out, _ := runCLI(t, cfg, "", "rm", "--json", "-f"); code != 0 || out != `{"commit":"","paths":[]}`+"\n" {
		t.Errorf("rm -f without paths: code=%d out=%q", code, out)
	}
	if code, _, errs := runCLI(t, cfg, "", "rm"); code != ExitUsage || !strings.Contains(errs, "missing argument") {
		t.Errorf("rm without paths: code=%d errs=%q", code, errs)
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

	// A source whose target was already moved to by an earlier source is not replacing.
	code, _, errs = runCLI(t, cfg, "", "mv", "machines/h1/y.md", "projects/app/y.md", "projects")
	if code != ExitError || errs != "wikictl: projects/y.md: not replacing\n" {
		t.Errorf("mv of sources with the same name: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "stat", "projects/y.md", "projects/app/y.md"); code != 0 {
		t.Error("the first source was not moved, or the second was")
	}

	// A directory given after a file in it moves the other files, and its parent
	// given after them is reported as missing.
	code, _, errs = runCLI(t, cfg, "", "mv", "-t", "projects/app", "machines/global/index.md", "machines/global", "machines")
	if code != ExitError || errs != "wikictl: machines: no such file or directory\n" {
		t.Errorf("mv of directories after their files: code=%d errs=%q", code, errs)
	}
	if code, _, _ := runCLI(t, cfg, "", "stat", "projects/app/index.md", "projects/app/global/push.md", "projects/app/global/img/logo.png"); code != 0 {
		t.Error("the files were not moved")
	}
}

func TestCommitMessage(t *testing.T) {
	if got := (&app{}).commitMessage("rm", []string{"global/a.md", "global/b"}); got != "wikictl: rm global/a.md global/b" {
		t.Errorf("short: %q", got)
	}
	long := make([]string, 30)
	for i := range long {
		long[i] = "global/some-long-page-name.md"
	}
	if got := (&app{}).commitMessage("rm", long); got != "wikictl: rm global/some-long-page-name.md and 29 more" {
		t.Errorf("long: %q", got)
	}
}

// TestWriteOverDirectoryOrFile checks that a write never replaces a directory
// with a file or puts a file below a path that is a file.
func TestWriteOverDirectoryOrFile(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	for _, p := range []string{"global/d.md/z.md", "global/d.md/w.md", "x.md/a.md"} {
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
		{[]string{"put", "global"}, "wikictl: global: is a directory\n"},
		{[]string{"put", "global/d.md"}, "wikictl: global/d.md: is a directory\n"},
		{[]string{"put", "x.md"}, "wikictl: x.md: is a directory\n"},
		{[]string{"put", "global/push.md/child.png"}, "wikictl: global/push.md/child.png: global/push.md is a file\n"},
		{[]string{"mv", "global/index.md", "global/push.md/index.md"}, "wikictl: global/push.md/index.md: not a directory\n"},
	} {
		if code, _, errs := runCLI(t, cfg, "x", c.args...); code != ExitError || !strings.HasSuffix(errs, c.errs) {
			t.Errorf("%v: code=%d errs=%q", c.args, code, errs)
		}
	}
	// A path at the root that is not a directory is still a file at the root,
	// and a file that is not a page follows the name rules of pages.
	for _, p := range []string{"README.md", "newtop", "global/a b.txt", "global/.hidden"} {
		if code, _, errs := runCLI(t, cfg, "x", "put", p); code != ExitInvalid || !strings.Contains(errs, "bad_path: ") {
			t.Errorf("put %s: code=%d errs=%q", p, code, errs)
		}
	}
	// Content that put rejects keeps the rejection of the path.
	if code, _, errs := runCLI(t, cfg, "---\n: [\n---\n# x\n", "put", "x.md"); code != ExitInvalid || !strings.Contains(errs, "bad_path: ") {
		t.Errorf("put x.md with invalid frontmatter: code=%d errs=%q", code, errs)
	}
	if got := gitOut(t, "--git-dir", remote, "rev-parse", "main"); got != head {
		t.Errorf("a rejected write moved the remote branch: %s -> %s", head, got)
	}

	// A submodule is neither replaced nor turned into a directory.
	work := cloneRemote(t, cfg)
	mustRun(t, work, "git", "update-index", "--add", "--cacheinfo", "160000,"+head+",global/sub")
	commitAndPush(t, work)
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
	remote, work := filepath.Join(filepath.Dir(cfg), "remote.git"), cloneRemote(t, cfg)
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
	commitAndPush(t, work)
	mode := func(p string) string {
		t.Helper()
		if f := strings.Fields(gitOut(t, "--git-dir", remote, "ls-tree", "main", "--", p)); len(f) > 0 {
			return f[0]
		}
		return ""
	}
	blob := func(p string) string { return gitOut(t, "--git-dir", remote, "cat-file", "-p", "main:"+p) }
	object := func(p string) string { return gitOut(t, "--git-dir", remote, "rev-parse", "main:"+p) }
	linkObject, dirLinkObject := object("global/link.md"), object("tools/push.md")

	// A symbolic link moved to a new name keeps its mode and target, gets no
	// alias, and links to it are rewritten in pages that keep their mode.
	for _, args := range [][]string{{"mv", "global/link.md", "projects/link2.md"}, {"mv", "tools", "bin"}} {
		if code, _, errs := runCLI(t, cfg, "", args...); code != 0 {
			t.Fatalf("%v: code=%d %s", args, code, errs)
		}
	}
	if m, o := mode("projects/link2.md"), object("projects/link2.md"); m != "120000" || o != linkObject {
		t.Errorf("moved symbolic link: mode %q, object %s, want %s", m, o, linkObject)
	}
	if m, b := mode("global/exec.md"), blob("global/exec.md"); m != "100755" || !strings.Contains(b, "[l](../projects/link2.md)") {
		t.Errorf("rewritten executable page: mode %q, content %q", m, b)
	}
	if m1, m2, o := mode("bin/run.sh"), mode("bin/push.md"), object("bin/push.md"); m1 != "100755" || m2 != "120000" || o != dirLinkObject {
		t.Errorf("moved directory: modes %q and %q, link object %s, want %s", m1, m2, o, dirLinkObject)
	}
	if code, _, errs := runCLI(t, cfg, "#!/bin/sh\necho hi\n", "put", "--base", shaOf(t, cfg, "bin/run.sh"), "bin/run.sh"); code != 0 || mode("bin/run.sh") != "100755" {
		t.Errorf("put over an executable: code=%d mode %q %s", code, mode("bin/run.sh"), errs)
	}

	for p, want := range map[string]string{
		"projects/link2.md":   "wikictl: projects/link2.md: is a symbolic link\n",
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
	if code, _, errs := runCLI(t, cfg, "", "rm", "-r", "global/sub", "projects/link2.md", "mods"); code != 0 || mode("global/sub")+mode("projects/link2.md")+mode("mods/lib/sub") != "" {
		t.Errorf("rm of submodules and a symbolic link: code=%d %s", code, errs)
	}
}

// TestLinkTargetsNonRegular checks which link targets lint reports as broken
// when they are not regular files: symbolic links exist, and submodules, with
// or without their commit in the mirror, a directory, a path through a
// symbolic link to a directory and an absent path do not. A file at the root
// that is not a page exists. None of them is a failure of git, and put warns
// about the same targets as lint.
func TestLinkTargetsNonRegular(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links")
	}
	cfg := setup(t)
	remote, work := filepath.Join(filepath.Dir(cfg), "remote.git"), cloneRemote(t, cfg)
	refs := "---\nsummary: r\n---\n# r\n" +
		"[r](../README.md) [s](sub.md) [o](other.md) [l](link.md) [ld](linkdir.md) [d](linkdir.md/app/x.md) [p](dir.md) [m](missing.md) [i](index.md)\n"
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# wiki\n"), 0o644)
	os.Symlink("push.md", filepath.Join(work, "global", "link.md"))
	os.Symlink("../projects", filepath.Join(work, "global", "linkdir.md"))
	os.MkdirAll(filepath.Join(work, "global", "dir.md"), 0o755)
	os.WriteFile(filepath.Join(work, "global", "dir.md", "a.md"), []byte("---\nsummary: a\n---\n# a\n"), 0o644)
	os.WriteFile(filepath.Join(work, "global", "refs.md"), []byte(refs), 0o644)
	mustRun(t, work, "git", "add", "-A")
	mustRun(t, work, "git", "update-index", "--add", "--cacheinfo", "160000,"+gitOut(t, "--git-dir", remote, "rev-parse", "main")+",global/sub.md")
	mustRun(t, work, "git", "update-index", "--add", "--cacheinfo", "160000,1234567890123456789012345678901234567890,global/other.md")
	commitAndPush(t, work)
	want := []string{"global/sub.md", "global/other.md", "global/linkdir.md/app/x.md", "global/dir.md", "global/missing.md"}
	code, out, errs := runCLI(t, cfg, "", "--json", "lint", "global/refs.md")
	var res struct {
		Items []struct{ Code, Message string }
	}
	mustUnmarshal(t, out, &res)
	var broken []string
	for _, it := range res.Items {
		if it.Code == "broken_link" {
			broken = append(broken, strings.TrimPrefix(it.Message, "link target does not exist: "))
		}
	}
	if code != ExitInvalid || !slices.Equal(broken, want) {
		t.Errorf("lint: code=%d broken=%q errs=%q", code, broken, errs)
	}
	code, _, errs = runCLI(t, cfg, refs, "put", "global/refs2.md")
	var warned []string
	for l := range strings.Lines(errs) {
		if _, target, ok := strings.Cut(strings.TrimSuffix(l, "\n"), "broken_link: link target does not exist: "); ok {
			warned = append(warned, target)
		}
	}
	if code != ExitOK || !slices.Equal(warned, want) {
		t.Errorf("put: code=%d warned=%q errs=%q", code, warned, errs)
	}
}

// TestMoveQuotedName checks that mv moves pages whose names git quotes in
// output without -z, and rewrites the links to them.
func TestMoveQuotedName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`file names with " or \`)
	}
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	pushFiles(t, cfg, map[string]string{
		`global/q"t.md`:       "---\nsummary: q\n---\n# q\n",
		`global/b\s.md`:       "---\nsummary: q\n---\n# q\n",
		"projects/app/ref.md": "---\nsummary: r\n---\n# r\n[q](../../global/q\"t.md) [b](../../global/b\\s.md)\n",
	})
	for _, args := range [][]string{{"mv", `global/q"t.md`, "global/qt.md"}, {"mv", `global/b\s.md`, "global/bs.md"}} {
		if code, _, errs := runCLI(t, cfg, "", args...); code != 0 {
			t.Fatalf("%v: code=%d %s", args, code, errs)
		}
	}
	files := strings.Split(gitOut(t, "--git-dir", remote, "ls-tree", "-r", "-z", "--name-only", "main"), "\x00")
	for _, f := range []string{"global/qt.md", "global/bs.md"} {
		if !slices.Contains(files, f) {
			t.Errorf("%s does not exist after mv: %q", f, files)
		}
	}
	for _, f := range []string{`global/q"t.md`, `global/b\s.md`} {
		if slices.Contains(files, f) {
			t.Errorf("%s still exists after mv", f)
		}
	}
	if ref := gitOut(t, "--git-dir", remote, "cat-file", "-p", "main:projects/app/ref.md"); !strings.Contains(ref, "[q](../../global/qt.md) [b](../../global/bs.md)") {
		t.Errorf("referring page: %q", ref)
	}
}

// TestMvDotDirMarkdown checks that mv moves a .md file below a directory
// starting with a dot, which is not a page, as it moves any other file: its
// content is kept, and links to it are left as written.
func TestMvDotDirMarkdown(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	tmpl := "# PR\n[x](../projects/app/x.md)\n"
	ref := "---\nsummary: r\n---\n# r\n[t](../.github/PULL_REQUEST_TEMPLATE.md)\n"
	pushFiles(t, cfg, map[string]string{
		".github/PULL_REQUEST_TEMPLATE.md": tmpl,
		".github/ISSUE_TEMPLATE/bug.md":    "# bug\n",
		".github/workflows/test.yml":       "on: push\n",
		"global/ref.md":                    ref,
	})
	code, out, errs := runCLI(t, cfg, "", "mv", "--json", ".github/PULL_REQUEST_TEMPLATE.md", "global/pr.md")
	if code != 0 || errs != "" {
		t.Fatalf("mv of a .md below a dot directory: code=%d out=%q errs=%q", code, out, errs)
	}
	var res struct {
		Moved     []movedFile `json:"moved"`
		Rewritten int         `json:"rewritten"`
		Commit    string      `json:"commit"`
	}
	mustUnmarshal(t, out, &res)
	if len(res.Moved) != 1 || res.Moved[0] != (movedFile{".github/PULL_REQUEST_TEMPLATE.md", "global/pr.md"}) || res.Rewritten != 0 || len(res.Commit) != 40 {
		t.Errorf("mv output: %q", out)
	}
	files := strings.Split(gitOut(t, "--git-dir", remote, "ls-tree", "-r", "-z", "--name-only", "main"), "\x00")
	if slices.Contains(files, ".github/PULL_REQUEST_TEMPLATE.md") || !slices.Contains(files, "global/pr.md") {
		t.Errorf("files after mv: %q", files)
	}
	if got := gitOut(t, "--git-dir", remote, "cat-file", "-p", "main:global/pr.md"); got != strings.TrimSuffix(tmpl, "\n") {
		t.Errorf("moved file: %q", got)
	}
	if got := gitOut(t, "--git-dir", remote, "cat-file", "-p", "main:global/ref.md"); got != strings.TrimSuffix(ref, "\n") {
		t.Errorf("page linking to the moved file: %q", got)
	}

	// A directory starting with a dot moves with every file below it.
	if code, _, errs := runCLI(t, cfg, "", "mv", ".github", "global/github"); code != 0 || errs != "" {
		t.Fatalf("mv of a dot directory: code=%d errs=%q", code, errs)
	}
	files = strings.Split(gitOut(t, "--git-dir", remote, "ls-tree", "-r", "-z", "--name-only", "main"), "\x00")
	for _, f := range []string{"global/github/ISSUE_TEMPLATE/bug.md", "global/github/workflows/test.yml"} {
		if !slices.Contains(files, f) {
			t.Errorf("%s does not exist after mv: %q", f, files)
		}
	}
	if slices.ContainsFunc(files, func(f string) bool { return strings.HasPrefix(f, ".github/") }) {
		t.Errorf("files left below .github: %q", files)
	}

	// A destination starting with a dot is still rejected.
	if code, _, errs := runCLI(t, cfg, "", "mv", "global/pr.md", ".github/pr.md"); code != ExitInvalid || !strings.Contains(errs, "bad_path") {
		t.Errorf("mv to a dot directory: code=%d errs=%q", code, errs)
	}
}

// TestWarningsOnlyWithACommit checks that the warnings about a page are
// printed when the commit succeeds and left out when the write fails, so that
// a failure reports only what stopped it.
func TestWarningsOnlyWithACommit(t *testing.T) {
	cfg := setup(t)
	body := "# Note\n"
	// A page without a summary whose name is not recommended is written with
	// both warnings.
	code, _, errs := runCLI(t, cfg, body, "put", "global/Note.md")
	if code != ExitOK || !strings.Contains(errs, "name_style") || !strings.Contains(errs, "missing_summary") {
		t.Errorf("put: code=%d errs=%q", code, errs)
	}
	// A put below a file, and a put that conflicts, print no warning.
	code, _, errs = runCLI(t, cfg, body, "put", "global/push.md/Note.md")
	if code != ExitError || strings.Contains(errs, "warning") {
		t.Errorf("put below a file: code=%d errs=%q", code, errs)
	}
	code, _, errs = runCLI(t, cfg, body, "put", "global/Note.md")
	if code != ExitConflict || strings.Contains(errs, "warning") {
		t.Errorf("put of a page that exists: code=%d errs=%q", code, errs)
	}
	// mv warns about its destination only when it moved something.
	code, _, errs = runCLI(t, cfg, "", "mv", "global/push.md", "projects/app/Note.md")
	if code != ExitOK || !strings.Contains(errs, "name_style") {
		t.Errorf("mv: code=%d errs=%q", code, errs)
	}
	code, _, errs = runCLI(t, cfg, "", "mv", "global/index.md", "projects/app/Note.md")
	if code != ExitError || !strings.Contains(errs, "not replacing") || strings.Contains(errs, "warning") {
		t.Errorf("mv to a page that exists: code=%d errs=%q", code, errs)
	}
	// A mv that moves one source and fails on another reports the failure
	// first: the warnings wait for the commit.
	code, _, errs = runCLI(t, cfg, "", "mv", "global/Note.md", "global/none.md", "machines/h1")
	failed := strings.Index(errs, "wikictl: global/none.md: no such file or directory")
	warned := strings.Index(errs, "wikictl: warning: machines/h1/Note.md:0: name_style")
	if code != ExitError || failed < 0 || warned < 0 || failed > warned {
		t.Errorf("mv of one source that fails: code=%d errs=%q", code, errs)
	}
}

// TestLongNames checks that a name over 255 bytes, which a clone cannot check
// out, is rejected as the destination of put, edit and mv, while rm still
// takes one, so that a page already in the wiki can be removed.
func TestLongNames(t *testing.T) {
	cfg := setup(t)
	longName := strings.Repeat("a", 253) // 256 bytes with .md
	longPage := "global/" + longName + ".md"
	longFile := strings.Repeat("c", 256) + ".png"
	longDir := strings.Repeat("b", 256)
	// Every path that is written names the segment that is too long, whether
	// it is a page, another file or a directory of the destination.
	for _, c := range []struct {
		args  []string
		stdin string
		want  string
	}{
		{[]string{"put", longPage}, "---\nsummary: s\n---\n", `bad_path: name "` + longName + `.md" is longer than 255 bytes`},
		{[]string{"put", "global/" + longFile}, "x", `bad_path: name "` + longFile + `" is longer than 255 bytes`},
		{[]string{"mv", "global/push.md", longPage}, "", `bad_path: name "` + longName + `.md" is longer than 255 bytes`},
		{[]string{"mv", "global", "projects/" + longDir}, "", `bad_path: name "` + longDir + `" is longer than 255 bytes`},
	} {
		if code, _, errs := runCLI(t, cfg, c.stdin, c.args...); code != ExitInvalid || !strings.Contains(errs, c.want) {
			t.Errorf("%v: code=%d errs=%q", c.args, code, errs)
		}
	}
	// A name of exactly 255 bytes is accepted.
	if code, _, errs := runCLI(t, cfg, "---\nsummary: s\n---\n", "put", "global/"+strings.Repeat("a", 252)+".md"); code != ExitOK {
		t.Errorf("a name of 255 bytes: code=%d errs=%q", code, errs)
	}
	// rm does not apply the limit, so such a path is only missing.
	if code, _, errs := runCLI(t, cfg, "", "rm", longPage); code != ExitError || !strings.Contains(errs, "no such file or directory") {
		t.Errorf("rm: code=%d errs=%q", code, errs)
	}
	// edit rejects it before running the editor.
	editWith(t, "exit 7\n")
	if code, _, errs := runCLI(t, cfg, "", "edit", longPage); code != ExitInvalid || !strings.Contains(errs, "bad_path") {
		t.Errorf("edit: code=%d errs=%q", code, errs)
	}
}
