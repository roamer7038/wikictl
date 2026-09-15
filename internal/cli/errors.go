package cli

import (
	"errors"
	"fmt"

	"github.com/roamer7038/wikictl/internal/repo"
)

// Exit codes. Each maps to the "error" field of the JSON error object.
const (
	ExitOK       = 0 // success
	ExitError    = 1 // general error, such as a missing page
	ExitUsage    = 2 // usage or configuration error
	ExitConflict = 3 // the file already exists, or changed or was deleted since it was read
	ExitInvalid  = 4 // the page violates the wiki format
	ExitGit      = 5 // a git command failed
)

// usageError is a usage or configuration error. When cmd is set, the message
// is about the arguments of cmd and text output adds its synopsis.
type usageError struct {
	cmd *command
	msg string
}

func (e *usageError) Error() string {
	if e.cmd != nil {
		return e.cmd.name + ": " + e.msg
	}
	return e.msg
}

// invalidError is a page or path that violates the wiki format. The message
// starts with the issue code, such as "bad_path: ".
type invalidError struct{ msg string }

func (e *invalidError) Error() string { return e.msg }

// gitError is a failed git command.
type gitError struct{ err error }

func (e *gitError) Error() string { return e.err.Error() }

func (e *gitError) Unwrap() error { return e.err }

// conflictError is an optimistic-lock failure of a commit. rerun names the
// command to run again; it is empty for put.
type conflictError struct {
	cf    *repo.Conflict
	rerun string
}

func (e *conflictError) Error() string { return e.cf.Error() }

func (e *conflictError) Unwrap() error { return e.cf }

// exitStatus ends the command with its value as the exit code after the
// command has written its output, such as lint with violations.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

type errorOut struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type conflictOut struct {
	Error   string `json:"error"`
	Reason  string `json:"reason"`
	Path    string `json:"path"`
	SHA     string `json:"sha"`
	Content string `json:"content"`
	Message string `json:"message"`
}

// exitCode returns the exit code and the "error" field of err. An error of no
// type defined here is a general error.
func exitCode(err error) (code int, kind string) {
	var (
		ue *usageError
		ie *invalidError
		ge *gitError
		ce *conflictError
		es exitStatus
	)
	switch {
	case err == nil:
		return ExitOK, ""
	case errors.As(err, &es):
		return int(es), ""
	case errors.As(err, &ue):
		return ExitUsage, "usage"
	case errors.As(err, &ce):
		return ExitConflict, "conflict"
	case errors.As(err, &ie):
		return ExitInvalid, "invalid"
	case errors.As(err, &ge):
		return ExitGit, "git"
	}
	return ExitError, "error"
}

// report writes err and returns its exit code. With --json the error object
// goes to stdout; otherwise the message goes to stderr. A conflict also
// carries the current content and sha of the page.
func (a *app) report(err error) int {
	code, kind := exitCode(err)
	var (
		ue *usageError
		ce *conflictError
	)
	switch {
	case code == ExitOK || kind == "":
	case errors.As(err, &ce):
		cf := ce.cf
		if a.json {
			a.emit(conflictOut{kind, cf.Reason, cf.Path, cf.SHA, string(cf.Content), conflictMessage(cf, ce.rerun)}, nil)
		} else {
			fmt.Fprintf(a.stderr, "wikictl: conflict (%s): %s sha=%s\n", cf.Reason, escapeControl(cf.Path), cf.SHA)
			a.stdout.Write(cf.Content)
		}
	case a.json:
		a.emit(errorOut{Error: kind, Message: err.Error()}, nil)
	default:
		fmt.Fprintln(a.stderr, "wikictl: "+escapeMessage(err.Error()))
		if errors.As(err, &ue) && ue.cmd != nil {
			fmt.Fprintln(a.stderr, "Usage: "+synopsis(ue.cmd))
			fmt.Fprintf(a.stderr, "Run \"wikictl help %s\" for details.\n", ue.cmd.name)
		}
	}
	return code
}

// conflictMessage returns the "message" of a conflict for its reason.
func conflictMessage(cf *repo.Conflict, rerun string) string {
	if rerun != "" {
		switch {
		case cf.Reason == "exists":
			return "the file was created since it was read; re-read the wiki and run " + rerun + " again with another path"
		case cf.SHA == "":
			return "the file was deleted since it was read; re-read the wiki and run " + rerun + " again"
		default:
			return "the file changed since it was read; re-read the wiki and run " + rerun + " again"
		}
	}
	switch {
	case cf.Reason == "exists":
		return "the file already exists; pass its sha with --base to replace it, or choose another path"
	case cf.SHA == "":
		return "the file was deleted since it was read"
	default:
		return "the file changed since it was read; re-read the current content and reapply the change"
	}
}
