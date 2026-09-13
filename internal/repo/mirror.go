package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Repo is the bare mirror of a wiki repository.
type Repo struct {
	Dir    string // mirror directory
	Remote string // URL of the wiki repository
	Branch string // branch read and written
}

// Open prepares the mirror at mirrorDir, creating it with "init --bare" and
// "remote add" when it does not exist. When branch is empty, the branch saved
// in the mirror's git config (wikictl.branch) is used; when that is empty too,
// the remote HEAD is queried with "ls-remote --symref" and the result saved.
func Open(mirrorDir, remote, branch string) (*Repo, error) {
	r := &Repo{Dir: mirrorDir, Remote: remote}
	if _, err := os.Stat(filepath.Join(mirrorDir, "HEAD")); err != nil {
		if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
			return nil, err
		}
		if _, err := r.Git("init", "-q", "--bare"); err != nil {
			return nil, err
		}
		if _, err := r.Git("remote", "add", "origin", remote); err != nil {
			return nil, err
		}
	}
	if out, _ := r.Git("rev-parse", "--is-shallow-repository"); strings.TrimSpace(out) == "true" {
		return nil, errors.New("mirror " + mirrorDir + " is a shallow repository; delete it and run the command again")
	}
	if out, _ := r.Git("config", "--get", "remote.origin.partialclonefilter"); strings.TrimSpace(out) != "" {
		return nil, errors.New("mirror " + mirrorDir + " is a partial clone; delete it and run the command again")
	}
	if branch == "" {
		out, _ := r.Git("config", "--get", "wikictl.branch")
		branch = strings.TrimSpace(out)
	}
	if branch == "" {
		out, err := r.Git("ls-remote", "--symref", remote, "HEAD")
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "ref: refs/heads/") {
				if f := strings.Fields(strings.TrimPrefix(l, "ref: refs/heads/")); len(f) > 0 {
					branch = f[0]
				}
			}
		}
		if branch == "" {
			branch = "main"
		}
	}
	// Save the branch only when it changed: concurrent processes writing the
	// same config file would fail to lock it.
	if out, _ := r.Git("config", "--get", "wikictl.branch"); strings.TrimSpace(out) != branch {
		if _, err := r.Git("config", "wikictl.branch", branch); err != nil {
			return nil, err
		}
	}
	r.Branch = branch
	return r, nil
}

func (r *Repo) trackingRef() string { return "refs/remotes/origin/" + r.Branch }

// Fetch updates the tracking ref of the branch with an explicit refspec.
// A remote that does not have the branch yet (an empty repository) is not an error.
func (r *Repo) Fetch() error {
	_, err := r.Git("fetch", "-q", "origin", "+refs/heads/"+r.Branch+":"+r.trackingRef())
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) && strings.Contains(ge.Stderr, "couldn't find remote ref") {
			return nil
		}
		return err
	}
	return nil
}

// Head returns the commit sha of the tracking ref, or "" when the branch does not exist yet.
func (r *Repo) Head() (string, error) {
	out, err := r.Git("rev-parse", "--verify", "-q", r.trackingRef())
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// lock takes an exclusive flock on the mirror so that concurrent wikictl
// processes on the same machine serialize their writes. The returned
// function releases the lock.
func (r *Repo) lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(r.Dir, "wikictl.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
