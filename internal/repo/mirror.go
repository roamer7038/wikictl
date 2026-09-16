package repo

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Repo is the bare mirror of a wiki repository.
type Repo struct {
	Dir    string // mirror directory
	Remote string // URL of the wiki repository
	Branch string // branch read and written

	snapshot string // commit that reads use, fixed by Snapshot; "" when the branch did not exist
}

// Open prepares the mirror at mirrorDir, creating it with create when it does
// not exist. When branch is empty, the branch saved in the mirror's git config
// (wikictl.branch) is used; when that is empty too, the remote HEAD is queried
// with "ls-remote --symref" and the result saved.
func Open(mirrorDir, remote, branch string) (*Repo, error) {
	r := &Repo{Dir: mirrorDir, Remote: remote}
	if err := privateDir(filepath.Dir(mirrorDir)); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(mirrorDir, "HEAD")); err != nil {
		if err := r.create(); err != nil {
			return nil, err
		}
	}
	// An existing mirror whose configuration others can read is tightened, as
	// privateDir does for the cache directory.
	if err := privateConfig(mirrorDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// The URL is left out of the message because it may hold credentials.
	if out, _ := r.Git("config", "--get-all", "remote.origin.url"); strings.TrimSuffix(out, "\n") != remote {
		return nil, errors.New("mirror " + mirrorDir + " is for another repository (its remote.origin.url is not the configured repo); delete it and run the command again")
	}
	if out, _ := r.Git("rev-parse", "--is-shallow-repository"); strings.TrimSpace(out) == "true" {
		return nil, errors.New("mirror " + mirrorDir + " is a shallow repository; delete it and run the command again")
	}
	if out, _ := r.Git("config", "--get", "remote.origin.partialclonefilter"); strings.TrimSpace(out) != "" {
		return nil, errors.New("mirror " + mirrorDir + " is a partial clone; delete it and run the command again")
	}
	out, _ := r.Git("config", "--get", "wikictl.branch")
	saved := strings.TrimSpace(out)
	branch = cmp.Or(branch, saved)
	if branch == "" {
		// The remote is named, not spelled out, so that a URL holding
		// credentials is not passed as a command argument.
		out, err := r.Git("ls-remote", "--symref", "origin", "HEAD")
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
	if saved != branch {
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

// privateDir creates dir with mode 0700, or sets an existing dir to 0700, so
// that the mirrors and lock files in it cannot be reached by other users
// whatever the umask and the modes git gives to the files it creates.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if fi.Mode().Perm() != 0o700 {
		return os.Chmod(dir, 0o700)
	}
	return nil
}

// privateConfig sets the mirror's git configuration to mode 0600. It holds the
// URL of the repository, which may carry credentials, and git keeps the mode
// of the file when it rewrites it.
func privateConfig(mirrorDir string) error {
	p := filepath.Join(mirrorDir, "config")
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if fi.Mode().Perm() != 0o600 {
		return os.Chmod(p, 0o600)
	}
	return nil
}

// addRemote adds the origin remote to the git configuration of the mirror at
// dir by writing the file, instead of running "git remote add <url>", so that
// a URL holding credentials is never a command argument: the arguments of a
// running process can be read by every user on the machine. The configuration
// is made private first, so that the URL is not written to a file others can
// read even for a moment. The refspec is the one "git remote add" writes.
func addRemote(dir, remote string) error {
	if err := privateConfig(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "config"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "[remote \"origin\"]\n\turl = %s\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n", configValue(remote))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

var configEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\b", `\b`)

// configValue quotes a value for git's configuration file, where an unquoted
// value ends at a comment character and loses its surrounding spaces.
func configValue(s string) string { return `"` + configEscaper.Replace(s) + `"` }

// create builds the mirror with "init --bare" and adds the origin remote while
// holding a lock on <mirror>.lock, so that concurrent processes initialize it once.
// The repository is built in a temporary directory next to the mirror and
// renamed into place, so that no process sees a mirror without its remote.
// The temporary directory, and therefore the mirror, has mode 0700. No
// template is used, so that a template directory in the user's configuration
// adds no hooks or info/attributes to the mirror.
func (r *Repo) create() error {
	parent := filepath.Dir(r.Dir)
	unlock, err := lockFile(r.Dir + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(filepath.Join(r.Dir, "HEAD")); err == nil {
		return nil
	}
	// An empty directory left at the mirror path is replaced; anything else is
	// left for the user to remove, since it may hold files that are not ours.
	if fi, err := os.Lstat(r.Dir); err == nil {
		if !fi.IsDir() || os.Remove(r.Dir) != nil {
			return errors.New("mirror " + r.Dir + " is not a git repository; delete it and run the command again")
		}
	}
	tmp, err := os.MkdirTemp(parent, filepath.Base(r.Dir)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o700); err != nil {
		return err
	}
	t := &Repo{Dir: tmp}
	if _, err := t.Git("init", "-q", "--bare", "--template="); err != nil {
		return err
	}
	if err := addRemote(tmp, r.Remote); err != nil {
		return err
	}
	return os.Rename(tmp, r.Dir)
}

func (r *Repo) trackingRef() string { return "refs/remotes/origin/" + r.Branch }

// fetchAttempts is the number of times Fetch runs git fetch when the update
// of the tracking ref loses a race with another fetch.
const fetchAttempts = 10

// Fetch updates the tracking ref of the branch with an explicit refspec.
// A remote that does not have the branch yet (an empty repository) is not an error.
// git fetch updates the ref only if it still holds the value read when the
// fetch started, so a fetch that another process's fetch overtakes fails with
// "cannot lock ref ...: is at X but expected Y"; that fetch is run again. Other
// failures to lock the ref, such as a stale lock file, are not retried. The
// lock of the mirror is not used, so that a read does not wait for a write
// that is pushing.
func (r *Repo) Fetch() error {
	for attempt := 1; ; attempt++ {
		_, err := r.Git("fetch", "-q", "origin", "+refs/heads/"+r.Branch+":"+r.trackingRef())
		var ge *GitError
		switch {
		case err == nil:
			return nil
		case !errors.As(err, &ge):
			return err
		case strings.Contains(ge.Stderr, "couldn't find remote ref"):
			return nil
		case strings.Contains(ge.Stderr, "cannot lock ref") && strings.Contains(ge.Stderr, "but expected") && attempt < fetchAttempts:
			continue
		default:
			return err
		}
	}
}

// Snapshot fixes the commit that the reads of r use to the current commit of
// the tracking ref, so that a command reads one state of the wiki even when
// another process fetches in the meantime. Reads find no file before Snapshot
// is called, as when the branch does not exist. Commit still fetches and
// builds on the latest commit of the branch.
func (r *Repo) Snapshot() error {
	head, err := r.trackingHead()
	r.snapshot = head
	return err
}

// trackingHead returns the commit sha of the tracking ref, or "" when the
// branch does not exist yet. A failure of git is an error.
func (r *Repo) trackingHead() (string, error) {
	out, err := r.Git("rev-parse", "--verify", "-q", r.trackingRef())
	if noResult(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// lock takes an exclusive flock on the mirror so that concurrent wikictl
// processes on the same machine serialize their writes. The returned
// function releases the lock.
func (r *Repo) lock() (func(), error) {
	return lockFile(filepath.Join(r.Dir, "wikictl.lock"))
}
