package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// TestEditTemporaryFileReplaced checks that an editor which replaces the
// temporary file with a symbolic link does not make edit read and commit the
// content the link points at, and that an editor which writes a new file and
// renames it into place, as vim does with backupcopy=no, keeps working.
func TestEditTemporaryFileReplaced(t *testing.T) {
	cfg := setup(t)
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	head := func() string { return gitOut(t, "--git-dir", remote, "rev-parse", "main") }

	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("---\nsummary: secret\n---\n# secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := head()
	editWith(t, "rm \"$1\"\nln -s "+shQuote(secret)+" \"$1\"\n")
	code, _, errs := runCLI(t, cfg, "", "edit", "global/push.md")
	if code != ExitError || !strings.Contains(errs, "symbolic link") {
		t.Errorf("editor replacing the file with a symlink: code=%d errs=%q", code, errs)
	}
	if _, after, ok := strings.Cut(errs, "kept in "); ok {
		p, _, _ := strings.Cut(after, "\n")
		os.Remove(p)
	}
	if head() != before {
		t.Error("the content the symlink points at was committed")
	}
	if _, out, _ := runCLI(t, cfg, "", "cat", "global/push.md"); strings.Contains(out, "secret") {
		t.Errorf("page after the symlink: %q", out)
	}

	// A file replaced by something that is not a regular file is named in words.
	editWith(t, "rm \"$1\"\nmkdir \"$1\"\n")
	code, _, errs = runCLI(t, cfg, "", "edit", "global/push.md")
	if code != ExitError || !strings.Contains(errs, "replaced the file with a directory") {
		t.Errorf("editor replacing the file with a directory: code=%d errs=%q", code, errs)
	}
	if _, after, ok := strings.Cut(errs, "kept in "); ok {
		p, _, _ := strings.Cut(after, "\n")
		os.Remove(p)
	}
	if head() != before {
		t.Error("a directory in place of the edited file created a commit")
	}

	// A file replaced by a named pipe is rejected instead of making the open
	// wait for a writer that never comes.
	editWith(t, "rm \"$1\"\nmkfifo \"$1\"\n")
	done := make(chan struct{})
	var fifoCode int
	var fifoErrs string
	go func() {
		defer close(done)
		var out, errb bytes.Buffer
		fifoCode = Main([]string{"--config", cfg, "edit", "global/push.md"}, strings.NewReader(""), &out, &errb)
		fifoErrs = errb.String()
	}()
	select {
	case <-done:
		if fifoCode != ExitError || !strings.Contains(fifoErrs, "replaced the file with a named pipe") {
			t.Errorf("editor replacing the file with a named pipe: code=%d errs=%q", fifoCode, fifoErrs)
		}
		if _, after, ok := strings.Cut(fifoErrs, "kept in "); ok {
			p, _, _ := strings.Cut(after, "\n")
			os.Remove(p)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("edit is waiting for a writer of the named pipe the editor left")
	}
	if head() != before {
		t.Error("a named pipe in place of the edited file created a commit")
	}

	editWith(t, "printf -- '---\\nsummary: renamed\\n---\\n# renamed\\n' > \"$1.new\"\nmv \"$1.new\" \"$1\"\n")
	if code, _, errs := runCLI(t, cfg, "", "edit", "global/push.md"); code != 0 {
		t.Fatalf("editor writing a new file and renaming it: code=%d errs=%q", code, errs)
	}
	if _, out, _ := runCLI(t, cfg, "", "cat", "global/push.md"); out != "---\nsummary: renamed\n---\n# renamed\n" {
		t.Errorf("page after the rename: %q", out)
	}
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
	// A name starting with a dot at the root is not a page, so edit creates it
	// as any other file.
	editWith(t, "printf x > \"$1\"\n")
	if code, out, errs := runCLI(t, cfg, "", "edit", "-v", ".newtop"); code != ExitOK || !strings.HasPrefix(out, ".newtop\t") {
		t.Errorf("edit of a dot name at the root: code=%d out=%q errs=%q", code, out, errs)
	}
	if code, _, errs := runCLI(t, cfg, "---\nsummary: a\n---\n# a\n", "put", "x.md/a.md"); code != 0 {
		t.Fatalf("put x.md/a.md: %s", errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "edit", "x.md"); code != ExitError || errs != "wikictl: x.md: is a directory\n" {
		t.Errorf("edit of a directory with a page name at the root: code=%d errs=%q", code, errs)
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
	work := cloneRemote(t, cfg)
	editWith(t, "printf 'mine\\n' > \"$1\"\ncd "+shQuote(work)+" && git pull -q origin main && printf 'theirs\\n' > projects/app/x.md && git -c user.name=t -c user.email=t@t commit -qam theirs && git push -q origin HEAD:main\n")
	code, out, errs := runCLI(t, cfg, "", "edit", "projects/app/x.md")
	if code != ExitConflict || out != "theirs\n" || keptFile(t, errs) != "mine\n" {
		t.Errorf("conflict: code=%d out=%q errs=%q", code, out, errs)
	}
}
