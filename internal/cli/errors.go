package cli

import (
	"errors"
	"fmt"
	"strings"

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

// nearestCommands maps a name wikictl has no command for to the command that
// does its work, for the names taken from Unix and git that are reached for
// most often. An empty value is a name wikictl has no command for yet. A value
// may carry a flag, such as "cat --at"; its first word names the help topic.
var nearestCommands = map[string]string{
	"search":  "grep",
	"read":    "cat",
	"list":    "ls",
	"history": "log",
	"show":    "cat --at",
	"blame":   "",
	"diff":    "",
}

// unknownCommand returns the usage error of a name wikictl has no command for,
// with the way on from it on a second line. Running the name and asking help
// for it both report it, so that the two say the same thing.
func unknownCommand(name string) *usageError {
	msg := "unknown command: " + name + "\n"
	near, known := nearestCommands[name]
	switch {
	case near != "":
		topic, _, _ := strings.Cut(near, " ")
		return &usageError{msg: msg + "the nearest command is " + near + `; run "wikictl help ` + topic + `" for its usage`}
	case known:
		return &usageError{msg: msg + "wikictl has no " + name + ` command yet; run "wikictl help" for the list of commands`}
	}
	return &usageError{msg: msg + `run "wikictl help" for the list of commands`}
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

// movedError is a commit that every attempt failed to push because another
// push moved the remote branch first. Nothing was written, so it is reported
// as a conflict, with reason "moved", that cmd resolves by running again.
type movedError struct {
	mv  *repo.Moved
	cmd string
}

func (e *movedError) Error() string { return e.mv.Error() }

func (e *movedError) Unwrap() error { return e.mv }

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

// MarshalJSON puts a path or content that is not valid UTF-8 in base64 under
// another key; see jsonout.go.
func (c conflictOut) MarshalJSON() ([]byte, error) {
	return jsonObject(nil).add("error", c.Error).add("reason", c.Reason).text("path", c.Path).
		add("sha", c.SHA).text("content", c.Content).add("message", c.Message).MarshalJSON()
}

// movedOut is the conflict of a push that lost every race. It has no path,
// sha or content: no file was read, checked or written. detail is what git
// reported, so that the cause stays visible.
type movedOut struct {
	Error   string `json:"error"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

// exitCode returns the exit code and the "error" field of err. An error of no
// type defined here is a general error.
func exitCode(err error) (code int, kind string) {
	var (
		ue *usageError
		ie *invalidError
		ge *gitError
		ce *conflictError
		me *movedError
		es exitStatus
	)
	switch {
	case err == nil:
		return ExitOK, ""
	case errors.As(err, &es):
		return int(es), ""
	case errors.As(err, &ue):
		return ExitUsage, "usage"
	case errors.As(err, &ce), errors.As(err, &me):
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
		me *movedError
	)
	switch {
	case code == ExitOK || kind == "":
	case errors.As(err, &me):
		msg, detail := movedMessage(me.cmd), movedDetail(me.mv)
		if a.json {
			a.emit(movedOut{kind, "moved", msg, detail}, nil)
		} else {
			fmt.Fprintf(a.stderr, "wikictl: conflict (moved): %s\n", msg)
			if detail != "" {
				fmt.Fprintln(a.stderr, "  "+escapeMessage(detail))
			}
		}
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

// movedDetail returns what git reported for the rejection that ended the
// retries.
func movedDetail(mv *repo.Moved) string {
	if mv.Err == nil {
		return ""
	}
	return mv.Err.Error()
}

// movedMessage returns the "message" of a push that lost every race.
func movedMessage(cmd string) string {
	return "the remote branch moved while the change was being pushed; nothing was written, run " + cmd + " again"
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
