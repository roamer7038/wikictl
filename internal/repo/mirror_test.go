package repo

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRemoteInMirrorConfig checks that the origin remote written to the
// mirror's configuration holds the URL of the repository as it is, whatever
// characters it has, and the refspec that "git remote add" writes. The check
// that the mirror belongs to the configured repository reads the URL back, so
// a URL that is not stored verbatim would make every command fail.
func TestRemoteInMirrorConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, remote := range []string{
		"https://alice:ghp_secret@example.invalid/you/wiki.git",
		"git@example.invalid:you/wiki.git",
		"ssh://git@example.invalid:2222/srv/wiki.git",
		`/srv/a b#c"d\e/wiki.git`,
	} {
		// The branch is given, so that opening the mirror does not need the
		// remote, which does not exist.
		r, err := Open(filepath.Join(t.TempDir(), "m"), remote, "main")
		if err != nil {
			t.Errorf("Open(%q): %v", remote, err)
			continue
		}
		if out, err := r.Git("config", "--get", "remote.origin.url"); err != nil || strings.TrimSuffix(out, "\n") != remote {
			t.Errorf("remote.origin.url of %q = %q, %v", remote, out, err)
		}
		if out, err := r.Git("config", "--get", "remote.origin.fetch"); err != nil || strings.TrimSuffix(out, "\n") != "+refs/heads/*:refs/remotes/origin/*" {
			t.Errorf("remote.origin.fetch of %q = %q, %v", remote, out, err)
		}
	}
}
