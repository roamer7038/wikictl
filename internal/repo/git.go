// Package repo maintains a bare mirror of the wiki repository and performs
// all git operations: fetching, reading with plumbing commands, and writing
// commits without a working tree.
package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitError is a failed git command.
type GitError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Stderr))
}

func (e *GitError) Unwrap() error { return e.Err }

// baseEnv disables interactive prompts so that a missing credential fails
// instead of hanging, and fixes the locale so that messages can be matched.
func baseEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
}

// Git runs git in the mirror and returns its stdout.
func (r *Repo) Git(args ...string) (string, error) { return r.run(nil, nil, args...) }

// GitIn runs git in the mirror with stdin and returns its stdout.
func (r *Repo) GitIn(stdin []byte, args ...string) (string, error) { return r.run(nil, stdin, args...) }

func (r *Repo) run(extraEnv []string, stdin []byte, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = r.Dir
	c.Env = append(baseEnv(), extraEnv...)
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return out.String(), &GitError{Args: args, Stderr: errb.String(), Err: err}
	}
	return out.String(), nil
}
