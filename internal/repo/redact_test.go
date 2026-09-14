package repo

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://alice:ghp_secret@github.com/you/wiki.git": "https://***@github.com/you/wiki.git",
		"https://ghp_secret@github.com/you/wiki.git":       "https://***@github.com/you/wiki.git",
		"http://a:b@host:8080":                             "http://***@host:8080",
		"HTTPS://a:p@ss@host/wiki?x=1@y":                   "HTTPS://***@host/wiki?x=1@y",
		"nope://alice:secret@host/wiki.git":                "nope://***@host/wiki.git",
		"ssh://git@server.example/srv/wiki.git":            "ssh://git@server.example/srv/wiki.git",
		"ssh://git:secret@server.example/srv/wiki.git":     "ssh://***@server.example/srv/wiki.git",
		"git+ssh://git:secret@server.example/wiki.git":     "git+ssh://***@server.example/wiki.git",
		"git@github.com:you/wiki.git":                      "git@github.com:you/wiki.git",
		"git:secret@github.com:you/wiki.git":               "***@github.com:you/wiki.git",
		"https://github.com/you/wiki.git":                  "https://github.com/you/wiki.git",
		"/srv/team@x/wiki.git":                             "/srv/team@x/wiki.git",
		`C:\wikis\a@b`:                                     `C:\wikis\a@b`,
		"refs/heads/main:refs/remotes/origin/main":         "refs/heads/main:refs/remotes/origin/main",
		"": "",
	} {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitErrorRedactsURL(t *testing.T) {
	r := &Repo{Dir: t.TempDir()}
	if _, err := r.Git("init", "-q", "--bare"); err != nil {
		t.Fatal(err)
	}
	_, err := r.Git("ls-remote", "--symref", "nope://alice:ghp_secret@example.com/wiki.git", "HEAD")
	var ge *GitError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want GitError", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "ghp_secret") || !strings.Contains(msg, "nope://***@example.com/wiki.git") {
		t.Errorf("message: %s", msg)
	}

	ge = &GitError{Args: []string{"fetch", "origin"}, Stderr: "fatal: unable to access 'https://alice:ghp_secret@example.com/wiki.git/'\n", Err: errors.New("exit status 128")}
	if msg := ge.Error(); strings.Contains(msg, "ghp_secret") || !strings.Contains(msg, "'https://***@example.com/wiki.git/'") {
		t.Errorf("stderr: %s", msg)
	}
}
