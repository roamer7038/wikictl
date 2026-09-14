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
		{"search/head", gitFault{match: head}, "", []string{"search", "--dirs", "global", "lease"}, ExitGit},
		{"search/grep without stderr", gitFault{match: " grep -E -l ", quiet: true}, "", []string{"search", "--dirs", "global", "lease"}, ExitGit},
		{"search/deprecated", gitFault{match: " grep -l -E "}, "", []string{"search", "--dirs", "global,machines/h1", "lease"}, ExitGit},
		{"search/updated", gitFault{match: " log --format="}, "", []string{"search", "--dirs", "global", "lease"}, ExitGit},
		{"get/head", gitFault{match: head}, "", []string{"get", "global/push.md"}, ExitGit},
		{"get/sha", gitFault{match: ":global/push.md "}, "", []string{"get", "global/push.md"}, ExitGit},
		{"get/backlinks grep", gitFault{match: " grep -E -l "}, "", []string{"get", "global/index.md"}, ExitGit},
		{"get/backlinks cat", gitFault{match: " cat-file --batch ", skip: 1}, "", []string{"get", "global/index.md"}, ExitGit},
		{"get/updated", gitFault{match: " log --format="}, "", []string{"get", "global/push.md"}, ExitGit},
		{"ls/deprecated", gitFault{match: " grep -l -E "}, "", []string{"ls", "--dirs", "global"}, ExitGit},
		{"ls/cat", gitFault{match: " cat-file --batch "}, "", []string{"ls", "--dirs", "global"}, ExitGit},
		{"ls/updated", gitFault{match: " log --format="}, "", []string{"ls", "--dirs", "global"}, ExitGit},
		{"lint/cat", gitFault{match: " cat-file --batch "}, "", []string{"lint", "global/push.md"}, ExitGit},
		{"dirs/head", gitFault{match: head}, "", []string{"dirs"}, ExitGit},
		{"context/head", gitFault{match: head}, "", []string{"context"}, ExitGit},
		{"rm/cat", gitFault{match: " cat-file --batch "}, "", []string{"rm", "global/push.md"}, ExitGit},
		{"mv/cat", gitFault{match: " cat-file --batch "}, "", []string{"mv", "global/push.md", "global/push2.md"}, ExitGit},
		{"mv/destination directory", gitFault{match: " -- projects/app2 "}, "", []string{"mv", "projects/app/", "projects/app2/"}, ExitGit},
		{"put/link targets", gitFault{match: " cat-file --batch "}, newPage, []string{"put", "global/new.md"}, ExitGit},
		{"put/head", gitFault{match: head}, newPage, []string{"put", "global/new.md"}, ExitGit},
		{"put/sha", gitFault{match: ":global/push.md "}, newPage, []string{"put", "global/push.md"}, ExitGit},
		{"put/conflict content", gitFault{match: " cat-file -p "}, newPage, []string{"put", "global/push.md"}, ExitGit},
		// push itself moves the tracking ref to the pushed commit, so a failed
		// update-ref leaves nothing to report.
		{"put/update-ref", gitFault{match: " update-ref "}, newPage, []string{"put", "global/new.md"}, ExitOK},
		{"init/head", gitFault{match: head}, "", []string{"init"}, ExitGit},
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

// TestSearchUnreadableObject removes the blob of a page from the mirror. git
// grep then exits with status 1, as it does for no match, and reports on
// stderr that it cannot read the object.
func TestSearchUnreadableObject(t *testing.T) {
	cfg := setup(t)
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
	out, err := exec.Command("git", "--git-dir="+mirror, "rev-parse", "refs/remotes/origin/main:global/push.md").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	if err := os.Remove(filepath.Join(mirror, "objects", sha[:2], sha[2:])); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLI(t, cfg, "", "--json", "--no-fetch", "search", "--dirs", "global", "lease")
	var e errorOut
	mustUnmarshal(t, stdout, &e)
	if code != ExitGit || e.Error != "git" || !strings.Contains(e.Message, "unable to read") {
		t.Errorf("code=%d stdout=%s", code, stdout)
	}
}
