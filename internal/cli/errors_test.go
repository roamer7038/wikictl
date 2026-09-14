package cli

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/roamer7038/wikictl/internal/repo"
)

func TestErrorUnwrap(t *testing.T) {
	cf := &repo.Conflict{Path: "global/a.md", Reason: "exists"}
	var gotCf *repo.Conflict
	if !errors.As(&conflictError{cf, "mv"}, &gotCf) || gotCf != cf {
		t.Errorf("conflictError does not unwrap to its *repo.Conflict")
	}
	ge := &repo.GitError{Args: []string{"grep"}, Err: errors.New("exit status 128")}
	var gotGe *repo.GitError
	if !errors.As(&gitError{ge}, &gotGe) || gotGe != ge {
		t.Errorf("gitError does not unwrap to its *repo.GitError")
	}
}

func TestReport(t *testing.T) {
	links := lookup("links")
	cf := &repo.Conflict{Path: "global/a.md", Reason: "changed", SHA: "abc", Content: []byte("# a\n")}
	cases := []struct {
		name       string
		err        error
		code       int
		text, errs string // text output: stdout and stderr
		json       string
	}{
		{"nil", nil, ExitOK, "", "", ""},
		{"exit status", exitStatus(ExitInvalid), ExitInvalid, "", "", ""},
		{"general", errors.New("boom"), ExitError, "", "wikictl: boom\n",
			`{"error":"error","message":"boom"}` + "\n"},
		{"not found", &notFoundError{"global/a.md"}, ExitError, "", "wikictl: page not found: global/a.md\n",
			`{"error":"error","message":"page not found: global/a.md"}` + "\n"},
		{"usage", &usageError{msg: "bad flag"}, ExitUsage, "", "wikictl: bad flag\n",
			`{"error":"usage","message":"bad flag"}` + "\n"},
		{"command usage", &usageError{links, "missing argument"}, ExitUsage, "",
			"wikictl: links: missing argument\nUsage: wikictl links [flags] <path>\nRun \"wikictl help links\" for details.\n",
			`{"error":"usage","message":"links: missing argument"}` + "\n"},
		{"invalid", &invalidError{"bad_path: x"}, ExitInvalid, "", "wikictl: bad_path: x\n",
			`{"error":"invalid","message":"bad_path: x"}` + "\n"},
		{"git", &gitError{errors.New("git fetch: exit status 128")}, ExitGit, "", "wikictl: git fetch: exit status 128\n",
			`{"error":"git","message":"git fetch: exit status 128"}` + "\n"},
		{"wrapped git", fmt.Errorf("wrap: %w", &gitError{errors.New("x")}), ExitGit, "", "wikictl: wrap: x\n",
			`{"error":"git","message":"wrap: x"}` + "\n"},
		{"conflict", &conflictError{cf, "mv"}, ExitConflict, "# a\n", "wikictl: conflict (changed): global/a.md sha=abc\n",
			`{"error":"conflict","reason":"changed","path":"global/a.md","sha":"abc","content":"# a\n","message":"the page changed since it was read; re-read the wiki and run mv again"}` + "\n"},
	}
	for _, c := range cases {
		for _, js := range []bool{false, true} {
			var out, errb bytes.Buffer
			a := &app{json: js, stdout: &out, stderr: &errb}
			code := a.report(c.err)
			wantOut, wantErr := c.text, c.errs
			if js {
				wantOut, wantErr = c.json, ""
			}
			if code != c.code || out.String() != wantOut || errb.String() != wantErr {
				t.Errorf("%s (json=%v): code=%d stdout=%q stderr=%q, want code=%d stdout=%q stderr=%q",
					c.name, js, code, out.String(), errb.String(), c.code, wantOut, wantErr)
			}
		}
	}
}
