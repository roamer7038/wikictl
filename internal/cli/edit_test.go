package cli

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// editWith makes edit take the test input as a terminal and run script as
// $EDITOR.
func editWith(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the editor is a shell script")
	}
	old := isTerminal
	isTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { isTerminal = old })
	p := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", p)
}

// keptFile returns the content of the edited file whose path edit printed on
// standard error, and removes the file.
func keptFile(t *testing.T, errs string) string {
	t.Helper()
	_, after, ok := strings.Cut(errs, "kept in ")
	p, _, _ := strings.Cut(after, "\n")
	if !ok {
		t.Fatalf("no kept file in %q", errs)
	}
	defer os.Remove(p)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEdit(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	head := func() string { return gitOut(t, "--git-dir", remote, "rev-parse", "main") }

	if code, _, errs := runCLI(t, cfg, "", "edit", "global/push.md"); code != ExitUsage || !strings.Contains(errs, "not a terminal") {
		t.Errorf("edit without a terminal: code=%d errs=%q", code, errs)
	}
	if devnull, err := os.Open(os.DevNull); err == nil {
		var out, errb strings.Builder
		code := Main([]string{"--config", cfg, "edit", "global/push.md"}, devnull, &out, &errb)
		devnull.Close()
		if code != ExitUsage || !strings.Contains(errb.String(), "not a terminal") {
			t.Errorf("edit < %s: code=%d errs=%q", os.DevNull, code, errb.String())
		}
	}

	// An unchanged file and a new file left empty create no commit.
	editWith(t, "exit 0\n")
	before := head()
	for _, p := range []string{"global/push.md", "global/new.md"} {
		if code, out, errs := runCLI(t, cfg, "", "edit", p); code != 0 || out != "" || errs != "" {
			t.Errorf("edit %s without a change: code=%d out=%q errs=%q", p, code, out, errs)
		}
	}
	if head() != before {
		t.Error("an edit without a change created a commit")
	}

	// The editor gets the current content in a file with the extension of the path.
	editWith(t, `case "$1" in *.md) ;; *) exit 9 ;; esac
grep -q force-with-lease "$1" || exit 8
printf -- '---\nsummary: edited\n---\n# push\n' > "$1"
`)
	if code, _, errs := runCLI(t, cfg, "", "edit", "-m", "tidy", "global/push.md"); code != 0 {
		t.Fatalf("edit: code=%d errs=%q", code, errs)
	}
	if _, out, _ := runCLI(t, cfg, "", "cat", "global/push.md"); out != "---\nsummary: edited\n---\n# push\n" {
		t.Errorf("edited page: %q", out)
	}
	if got := lastCommitMessage(t, cfg); got != "tidy" {
		t.Errorf("commit message: %q", got)
	}

	// VISUAL is used before EDITOR, an editor with arguments is run by the shell,
	// and a new file is committed with its directories.
	editWith(t, "exit 1\n")
	visual := filepath.Join(t.TempDir(), "visual")
	os.WriteFile(visual, []byte("[ \"$1\" = --flag ] || exit 5\nprintf png > \"$2\"\n"), 0o644)
	t.Setenv("VISUAL", "sh "+shQuote(visual)+" --flag")
	if code, out, errs := runCLI(t, cfg, "", "edit", "-v", "notes/img/a.png"); code != 0 || !strings.HasPrefix(out, "notes/img/a.png\t") {
		t.Errorf("edit of a new file: code=%d out=%q errs=%q", code, out, errs)
	}
	if got := lastCommitMessage(t, cfg); got != "wikictl: edit notes/img/a.png" {
		t.Errorf("default commit message: %q", got)
	}

	// A path below a file is rejected before the editor runs.
	editWith(t, "exit 7\n")
	if code, _, errs := runCLI(t, cfg, "", "edit", "global/push.md/x.md"); code != ExitError || errs != "wikictl: global/push.md/x.md: global/push.md is a file\n" {
		t.Errorf("edit below a file: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "edit", "global"); code != ExitError || errs != "wikictl: global: is a directory\n" {
		t.Errorf("edit of a directory at the root: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "edit", "newtop"); code != ExitInvalid || !strings.Contains(errs, "bad_path: ") {
		t.Errorf("edit of a file at the root: code=%d errs=%q", code, errs)
	}

	// A failing editor and content that put rejects keep the edited file.
	editWith(t, "printf draft > \"$1\"; exit 3\n")
	code, _, errs := runCLI(t, cfg, "", "edit", "global/index.md")
	if code != ExitError || keptFile(t, errs) != "draft" {
		t.Errorf("failing editor: code=%d errs=%q", code, errs)
	}
	editWith(t, "printf -- '---\\nsummary: [unclosed\\n---\\n# bad\\n' > \"$1\"\n")
	code, _, errs = runCLI(t, cfg, "", "edit", "global/index.md")
	if code != ExitInvalid || !strings.HasPrefix(keptFile(t, errs), "---\nsummary: [unclosed") {
		t.Errorf("invalid frontmatter: code=%d errs=%q", code, errs)
	}

	// A file changed by another writer while it is edited is a conflict.
	work := filepath.Join(filepath.Dir(cfg), "work")
	editWith(t, "printf 'mine\\n' > \"$1\"\ncd "+shQuote(work)+" && git pull -q origin main && printf 'theirs\\n' > projects/app/x.md && git -c user.name=t -c user.email=t@t commit -qam theirs && git push -q origin HEAD:main\n")
	code, out, errs := runCLI(t, cfg, "", "edit", "projects/app/x.md")
	if code != ExitConflict || out != "theirs\n" || keptFile(t, errs) != "mine\n" {
		t.Errorf("conflict: code=%d out=%q errs=%q", code, out, errs)
	}
}
