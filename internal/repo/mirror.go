package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Repo is the bare mirror of a wiki repository.
type Repo struct {
	Dir    string // mirror directory
	Remote string // URL of the wiki repository
	Branch string // branch read and written
}

// Open prepares the mirror at mirrorDir, creating it with create when it does
// not exist. When branch is empty, the branch saved in the mirror's git config
// (wikictl.branch) is used; when that is empty too, the remote HEAD is queried with "ls-remote --symref" and the result saved.
func Open(mirrorDir, remote, branch string) (*Repo, error) {
	r := &Repo{Dir: mirrorDir, Remote: remote}
	if _, err := os.Stat(filepath.Join(mirrorDir, "HEAD")); err != nil {
		if err := r.create(); err != nil {
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
	// Save the branch only when it changed, under the mirror lock: concurrent
	// processes writing the same config file would fail to lock it.
	if out, _ := r.Git("config", "--get", "wikictl.branch"); strings.TrimSpace(out) != branch {
		unlock, err := r.lock()
		if err != nil {
			return nil, err
		}
		_, err = r.Git("config", "wikictl.branch", branch)
		unlock()
		if err != nil {
			return nil, err
		}
	}
	r.Branch = branch
	return r, nil
}

// create builds the mirror with "init --bare" and "remote add" while holding
// a lock on <mirror>.lock, so that concurrent processes initialize it once.
// The repository is built in a temporary directory next to the mirror and
// renamed into place, so that no process sees a mirror without its remote.
func (r *Repo) create() error {
	parent := filepath.Dir(r.Dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	unlock, err := lockFile(r.Dir + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(filepath.Join(r.Dir, "HEAD")); err == nil {
		return nil
	}
	tmp, err := os.MkdirTemp(parent, filepath.Base(r.Dir)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	t := &Repo{Dir: tmp}
	if _, err := t.Git("init", "-q", "--bare"); err != nil {
		return err
	}
	if _, err := t.Git("remote", "add", "origin", r.Remote); err != nil {
		return err
	}
	// A directory left at the mirror path makes the rename fail; remove it when empty.
	os.Remove(r.Dir)
	return os.Rename(tmp, r.Dir)
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
	return lockFile(filepath.Join(r.Dir, "wikictl.lock"))
}
