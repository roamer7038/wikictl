package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// gitFault selects the git invocations that fail: those whose arguments,
// joined by spaces and surrounded by one space, contain match. The first skip
// of them run normally.
type gitFault struct {
	match string
	skip  int
	quiet bool // write nothing on stderr
}

// injectGitFault puts a git wrapper first on PATH that exits with status 128
// for the invocations selected by f and runs the real git for the others.
func injectGitFault(t *testing.T, f gitFault) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the git wrapper is a shell script")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	counter := shQuote(filepath.Join(dir, "count"))
	fail := "    echo 'fatal: injected failure' >&2\n"
	if f.quiet {
		fail = ""
	}
	script := "#!/bin/sh\n" +
		"case \" $* \" in\n" +
		"*" + shQuote(f.match) + "*)\n" +
		"  n=$(cat " + counter + " 2>/dev/null || echo 0)\n" +
		"  echo $((n + 1)) > " + counter + "\n" +
		"  if [ \"$n\" -ge " + strconv.Itoa(f.skip) + " ]; then\n" +
		fail +
		"    exit 128\n" +
		"  fi\n" +
		"  ;;\n" +
		"esac\n" +
		"exec " + shQuote(real) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// TestGitFailure checks that a command exits with 5 and prints only the error
// object when one of the git commands it runs fails, instead of printing a
// result that lacks what could not be read.
func TestGitFailure(t *testing.T) {
	const head = " rev-parse --verify -q refs/remotes/origin/main "
	newPage := "---\nsummary: new\n---\n# New\n[push](push.md)\n"
	cases := []struct {
		name  string
		fault gitFault
		stdin string
		args  []string
		code  int
	}{
		{"search/head", gitFault{match: head}, "", []string{"search", "lease"}, ExitGit},
		{"search/grep without stderr", gitFault{match: " grep -E -l ", quiet: true}, "", []string{"search", "lease"}, ExitGit},
		{"search/deprecated", gitFault{match: " grep -l -E "}, "", []string{"search", "lease"}, ExitGit},
		{"search/updated", gitFault{match: " log --format="}, "", []string{"search", "lease"}, ExitGit},
		{"get/head", gitFault{match: head}, "", []string{"get", "global/push.md"}, ExitGit},
		{"get/sha", gitFault{match: " ls-tree -z "}, "", []string{"get", "global/push.md"}, ExitGit},
		{"get/backlinks grep", gitFault{match: " grep -E -l "}, "", []string{"get", "global/index.md"}, ExitGit},
		{"get/backlinks cat", gitFault{match: " cat-file --batch ", skip: 1}, "", []string{"get", "global/index.md"}, ExitGit},
		{"get/updated", gitFault{match: " log --format="}, "", []string{"get", "global/push.md"}, ExitGit},
		{"ls/deprecated", gitFault{match: " grep -l -E "}, "", []string{"ls"}, ExitGit},
		{"ls/stat", gitFault{match: " cat-file --batch-check "}, "", []string{"ls"}, ExitGit},
		{"ls/cat", gitFault{match: " cat-file --batch "}, "", []string{"ls"}, ExitGit},
		{"ls/updated", gitFault{match: " log --format="}, "", []string{"ls"}, ExitGit},
		{"search/stat", gitFault{match: " cat-file --batch-check "}, "", []string{"search", "lease"}, ExitGit},
		{"get/stat", gitFault{match: " cat-file --batch-check "}, "", []string{"get", "global/push.md"}, ExitGit},
		{"dirs/stat", gitFault{match: " cat-file --batch-check "}, "", []string{"dirs"}, ExitGit},
		{"lint/stat", gitFault{match: " cat-file --batch-check "}, "", []string{"lint", "global/push.md"}, ExitGit},
		{"lint/cat", gitFault{match: " cat-file --batch "}, "", []string{"lint", "global/push.md"}, ExitGit},
		{"lint/link targets", gitFault{match: " cat-file --batch-check ", skip: 1}, "", []string{"lint", "global/push.md"}, ExitGit},
		{"dirs/head", gitFault{match: head}, "", []string{"dirs"}, ExitGit},
		{"rm/cat", gitFault{match: " cat-file --batch "}, "", []string{"rm", "global/push.md"}, ExitGit},
		{"mv/cat", gitFault{match: " cat-file --batch "}, "", []string{"mv", "global/push.md", "global/push2.md"}, ExitGit},
		{"mv/destination directory", gitFault{match: " -- projects/app2 "}, "", []string{"mv", "projects/app/", "projects/app2/"}, ExitGit},
		{"put/link targets", gitFault{match: " cat-file --batch-check "}, newPage, []string{"put", "global/new.md"}, ExitGit},
		{"put/head", gitFault{match: head}, newPage, []string{"put", "global/new.md"}, ExitGit},
		{"put/sha", gitFault{match: " ls-tree -z "}, newPage, []string{"put", "global/push.md"}, ExitGit},
		{"put/conflict content", gitFault{match: " cat-file -p "}, newPage, []string{"put", "global/push.md"}, ExitGit},
		// push itself moves the tracking ref to the pushed commit, so a failed
		// update-ref leaves nothing to report.
		{"put/update-ref", gitFault{match: " update-ref "}, newPage, []string{"put", "global/new.md"}, ExitOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := setup(t)
			injectGitFault(t, c.fault)
			code, out, errs := runCLI(t, cfg, c.stdin, append([]string{"--json"}, c.args...)...)
			if code != c.code {
				t.Fatalf("exit code %d, want %d\nstdout: %s\nstderr: %s", code, c.code, out, errs)
			}
			if c.code == ExitOK {
				return
			}
			var e errorOut
			mustUnmarshal(t, out, &e)
			keys, _ := jsonKeys(out)
			if e.Error != "git" || !slices.Equal(keys, []string{"error", "message"}) || !strings.Contains(e.Message, "exit status 128") {
				t.Errorf("stdout: %s", out)
			}
		})
	}
}

// looseMirror creates the mirror of cfg and unpacks its packs into loose
// objects, so that a test can delete single objects. It returns the path of
// the mirror.
func looseMirror(t *testing.T, cfg string) string {
	t.Helper()
	if code, _, errs := runCLI(t, cfg, "", "ls"); code != ExitOK {
		t.Fatalf("ls: code=%d %s", code, errs)
	}
	d := filepath.Dir(cfg)
	mirror := filepath.Join(d, "cache", "wikictl", mirrorName(filepath.Join(d, "remote.git")))
	packs, _ := filepath.Glob(filepath.Join(mirror, "objects", "pack", "*.pack"))
	for _, p := range packs {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		os.Remove(p)
		os.Remove(strings.TrimSuffix(p, ".pack") + ".idx")
		c := exec.Command("git", "--git-dir="+mirror, "unpack-objects", "-q")
		c.Stdin = bytes.NewReader(data)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("unpack-objects: %v\n%s", err, out)
		}
	}
	return mirror
}

// mirrorObject returns the sha of path at the tracking ref of mirror.
func mirrorObject(t *testing.T, mirror, path string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir="+mirror, "rev-parse", "refs/remotes/origin/main:"+path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// TestUnreadableObject deletes an object from the mirror, or points the
// tracking ref at a commit that does not exist. git then reports the object
// on stderr while grep may still exit with 0, cat-file --batch reports it as
// "missing" like an absent page, and "rev-parse <commit>:<path>" exits with 1
// like an absent path. Every command must exit with 5 instead of printing a
// partial result, "page not found" or a conflict.
func TestUnreadableObject(t *testing.T) {
	newPage := "---\nsummary: new\n---\n# New\n"
	cases := []struct {
		name      string
		remove    string // path whose object is deleted
		brokenRef string // content written to the tracking ref, if any
		stdin     string
		args      []string
	}{
		// machines/h1/y.md, projects/app/x.md and global/push.md match.
		{"search/grep with another match", "global/push.md", "", "", []string{"--no-fetch", "search", "lease"}},
		{"ls/deprecated with another match", "global/push.md", "", "", []string{"--no-fetch", "ls"}},
		{"get/backlinks with another match", "projects/app/x.md", "", "", []string{"--no-fetch", "get", "global/index.md"}},
		{"get/target blob", "global/push.md", "", "", []string{"--no-fetch", "get", "global/push.md"}},
		{"get/target tree", "global", "", "", []string{"--no-fetch", "get", "global/push.md"}},
		{"get/ref to a missing object", "", "1234567890123456789012345678901234567890\n", "", []string{"--no-fetch", "get", "global/push.md"}},
		{"get/ref with garbage", "", "garbage\n", "", []string{"--no-fetch", "get", "global/push.md"}},
		{"search/ref with garbage", "", "garbage\n", "", []string{"--no-fetch", "search", "lease"}},
		{"ls/ref with garbage", "", "garbage\n", "", []string{"--no-fetch", "ls"}},
		{"lint/ref with garbage", "", "garbage\n", "", []string{"--no-fetch", "lint"}},
		{"lint/target blob", "global/push.md", "", "", []string{"--no-fetch", "lint", "global/push.md"}},
		{"rm/target blob", "global/push.md", "", "", []string{"rm", "global/push.md"}},
		{"mv/source blob", "global/push.md", "", "", []string{"mv", "global/push.md", "global/push2.md"}},
		{"mv/destination blob", "global/push.md", "", "", []string{"mv", "global/index.md", "global/push.md"}},
		{"put/base with tree", "global", "", newPage, []string{"put", "--base", "BASE", "global/push.md"}},
		{"put/existence with tree", "global", "", newPage, []string{"put", "global/push.md"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := setup(t)
			mirror := looseMirror(t, cfg)
			base := mirrorObject(t, mirror, "global/push.md")
			if c.remove != "" {
				sha := mirrorObject(t, mirror, c.remove)
				if err := os.Remove(filepath.Join(mirror, "objects", sha[:2], sha[2:])); err != nil {
					t.Fatal(err)
				}
			}
			if c.brokenRef != "" {
				ref := filepath.Join(mirror, "refs", "remotes", "origin", "main")
				if err := os.WriteFile(ref, []byte(c.brokenRef), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			args := slices.Clone(c.args)
			for i, a := range args {
				if a == "BASE" {
					args[i] = base
				}
			}
			code, out, errs := runCLI(t, cfg, c.stdin, append([]string{"--json"}, args...)...)
			if code != ExitGit {
				t.Fatalf("exit code %d, want %d\nstdout: %s\nstderr: %s", code, ExitGit, out, errs)
			}
			var e errorOut
			mustUnmarshal(t, out, &e)
			keys, _ := jsonKeys(out)
			if e.Error != "git" || !slices.Equal(keys, []string{"error", "message"}) {
				t.Errorf("stdout: %s", out)
			}
		})
	}
}

// TestGitStderrNoise checks that output on stderr that is not an error, such
// as the trace GIT_TRACE enables or a warning about an unreadable attributes
// file, does not turn a successful read into exit code 5.
func TestGitStderrNoise(t *testing.T) {
	cfg := setup(t)
	reads := [][]string{
		{"search", "lease"},
		{"search", "zzz-none"},
		{"ls"},
		{"get", "global/push.md"},
	}
	check := func(t *testing.T, name string) {
		t.Helper()
		for _, args := range reads {
			if code, out, errs := runCLI(t, cfg, "", append([]string{"--json"}, args...)...); code != ExitOK {
				t.Errorf("%s: %v: code=%d out=%q errs=%.300q", name, args, code, out, errs)
			}
		}
	}
	t.Run("trace", func(t *testing.T) {
		t.Setenv("GIT_TRACE", "1")
		check(t, "GIT_TRACE=1")
	})
	t.Run("warning", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a file the user cannot read")
		}
		conf := t.TempDir()
		attr := filepath.Join(conf, "git", "attributes")
		if err := os.MkdirAll(filepath.Dir(attr), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(attr, []byte("* text\n"), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_CONFIG_HOME", conf)
		check(t, "unreadable attributes")
	})
}

// recordBatchInput puts a git wrapper first on PATH that appends the standard
// input of every "cat-file --batch" call, the call that reads contents, to the
// returned file.
func recordBatchInput(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the git wrapper is a shell script")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "batch-input")
	script := "#!/bin/sh\n" +
		"case \" $* \" in\n" +
		"*' cat-file --batch '*)\n" +
		"  tee -a " + shQuote(log) + " | " + shQuote(real) + " \"$@\"\n" +
		"  exit $?\n" +
		"  ;;\n" +
		"esac\n" +
		"exec " + shQuote(real) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// TestLargeBlobNotRead pushes a page over the size limit and checks that the
// read commands and lint report it as before without reading its content.
func TestLargeBlobNotRead(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	work := filepath.Join(t.TempDir(), "w")
	mustRun(t, "", "git", "clone", "-q", remote, work)
	huge := "---\nsummary: huge\n---\n# huge\n" + strings.Repeat("lorem ipsum\n", 100000) + "Äpfel\n[push](push.md)\n"
	files := map[string]string{
		"global/huge.md":  huge,
		"global/refer.md": "---\nsummary: refer\n---\n# refer\nsee [huge](huge.md)\n",
	}
	for p, c := range files {
		os.WriteFile(filepath.Join(work, p), []byte(c), 0o644)
	}
	mustRun(t, work, "git", "add", "-A")
	mustRun(t, work, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "huge")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	if code, _, errs := runCLI(t, cfg, "", "ls"); code != ExitOK {
		t.Fatalf("ls: code=%d errs=%q", code, errs)
	}
	sha := strings.TrimSpace(gitOut(t, "--git-dir", remote, "rev-parse", "main:global/huge.md"))
	log := recordBatchInput(t)

	var ls struct{ Items []lsItem }
	code, out, errs := runCLI(t, cfg, "", "--json", "ls")
	mustUnmarshal(t, out, &ls)
	if code != ExitOK || !slices.ContainsFunc(ls.Items, func(it lsItem) bool {
		return it.Path == "global/huge.md" && it.Title == "huge" && it.Summary == "" && it.Type == ""
	}) {
		t.Errorf("ls: code=%d out=%q errs=%q", code, out, errs)
	}
	var search struct{ Items []hit }
	code, out, errs = runCLI(t, cfg, "", "--json", "search", "--any", "äpfel", "zzz-none", "lorem")
	mustUnmarshal(t, out, &search)
	if code != ExitOK || len(search.Items) != 1 || search.Items[0].Path != "global/huge.md" || search.Items[0].Title != "huge" ||
		!slices.Equal(search.Items[0].Matched, []string{"äpfel", "lorem"}) {
		t.Errorf("search: code=%d out=%q errs=%q", code, out, errs)
	}
	var get getOut
	code, out, errs = runCLI(t, cfg, "", "--json", "get", "global/huge.md")
	mustUnmarshal(t, out, &get)
	if code != ExitOK || get.SHA != sha || get.Title != "huge" || get.Body != "" || len(get.Frontmatter) != 0 || len(get.Links) != 0 {
		t.Errorf("get: code=%d out=%q errs=%q", code, out, errs)
	}
	code, out, _ = runCLI(t, cfg, "", "lint", "global/huge.md", "global/refer.md")
	if code != ExitInvalid || out != "global/huge.md:0: page_too_large: page is larger than 1048576 bytes\n" {
		t.Errorf("lint: code=%d out=%q", code, out)
	}

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if in := string(b); strings.Contains(in, "global/huge.md") || strings.Contains(in, sha) {
		t.Errorf("the content of the large page was read; cat-file --batch input:\n%s", in)
	}

	injectGitFault(t, gitFault{match: " cat-file blob "})
	if code, out, errs := runCLI(t, cfg, "", "--json", "search", "lorem"); code != ExitGit {
		t.Errorf("search with a failing read of the large page: code=%d out=%q errs=%q", code, out, errs)
	}
}
