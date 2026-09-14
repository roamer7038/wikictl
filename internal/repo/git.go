// Package repo maintains a bare mirror of the wiki repository and performs
// all git operations: fetching, reading with plumbing commands, and writing
// commits without a working tree.
package repo

import (
	"bytes"
	"errors"
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

// Error shows the arguments and stderr with the credentials in URLs redacted
// by RedactURL.
func (e *GitError) Error() string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = RedactURL(a)
	}
	return fmt.Sprintf("git %s: %v: %s", strings.Join(args, " "), e.Err, redactText(strings.TrimSpace(e.Stderr)))
}

func (e *GitError) Unwrap() error { return e.Err }

// noResult reports whether err is a git command that exited with status 1
// and wrote nothing on stderr: "rev-parse --verify -q" for a name that does
// not exist, and grep with no match. Any other failure, such as grep exiting
// with 1 after "unable to read" an object, is an error.
func noResult(err error) bool {
	var ge *GitError
	var ee *exec.ExitError
	return errors.As(err, &ge) && errors.As(ge.Err, &ee) && ee.ExitCode() == 1 && strings.TrimSpace(ge.Stderr) == ""
}

// localEnvVars are the repository-local variables reported by
// "git rev-parse --local-env-vars", plus GIT_NAMESPACE, which prefixes the
// refs. Inherited from a git hook or alias, they would point the commands at
// the caller's repository instead of the mirror.
var localEnvVars = map[string]bool{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_CONFIG":                       true,
	"GIT_CONFIG_PARAMETERS":            true,
	"GIT_CONFIG_COUNT":                 true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_DIR":                          true,
	"GIT_WORK_TREE":                    true,
	"GIT_IMPLICIT_WORK_TREE":           true,
	"GIT_GRAFT_FILE":                   true,
	"GIT_INDEX_FILE":                   true,
	"GIT_NO_REPLACE_OBJECTS":           true,
	"GIT_REPLACE_REF_BASE":             true,
	"GIT_PREFIX":                       true,
	"GIT_SHALLOW_FILE":                 true,
	"GIT_COMMON_DIR":                   true,
	"GIT_NAMESPACE":                    true,
}

// baseEnv drops the repository-local variables, disables interactive prompts
// so that a missing credential fails instead of hanging, and fixes the locale
// so that messages can be matched.
func baseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !localEnvVars[k] {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
}

// Git runs git in the mirror and returns its stdout.
func (r *Repo) Git(args ...string) (string, error) { return r.run(nil, nil, args...) }

// GitIn runs git in the mirror with stdin and returns its stdout.
func (r *Repo) GitIn(stdin []byte, args ...string) (string, error) { return r.run(nil, stdin, args...) }

// gitStrict is Git that also fails when git writes on stderr. git grep
// reports an object it cannot read on stderr and still exits with 0 when
// another file matches.
func (r *Repo) gitStrict(args ...string) (string, error) { return r.runGit(true, nil, nil, args...) }

// run executes git in the mirror; see runGit.
func (r *Repo) run(extraEnv []string, stdin []byte, args ...string) (string, error) {
	return r.runGit(false, extraEnv, stdin, args...)
}

// runGit executes git in the mirror. With strict, output on stderr is a
// failure even at exit status 0. core.quotePath is turned off so that
// ls-tree, grep and log print non-ASCII paths verbatim instead of quoting
// them. --literal-pathspecs makes directory names containing '*', '?' or
// '[' match only themselves instead of acting as wildcards. --git-dir=.
// keeps git from searching parent directories for a repository.
func (r *Repo) runGit(strict bool, extraEnv []string, stdin []byte, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"--git-dir=.", "--literal-pathspecs", "-c", "core.quotePath=false"}, args...)...)
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
	if strict && strings.TrimSpace(errb.String()) != "" {
		return out.String(), &GitError{Args: args, Stderr: errb.String(), Err: errors.New("exit status 0 with errors")}
	}
	return out.String(), nil
}
