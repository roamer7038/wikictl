//go:build unix

package repo

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s: mode %#o, want %#o", path, got, want)
	}
}

// TestMirrorPermissions checks that the cache directory, the mirror and the
// lock files are private to the owner whatever the umask, and that an
// existing cache directory readable by others is tightened.
func TestMirrorPermissions(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	remote := newRemote(t, true)
	for _, existing := range []bool{false, true} {
		cache := filepath.Join(t.TempDir(), "wikictl")
		if existing {
			if err := os.Mkdir(cache, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		mirror := filepath.Join(cache, "m")
		r, err := Open(mirror, remote, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Commit([]Change{{Path: "global/p.md", Content: []byte("---\nsummary: p\n---\n")}}, "put", Author{"a", "a@a"}); err != nil {
			t.Fatal(err)
		}
		assertPerm(t, cache, 0o700)
		assertPerm(t, mirror, 0o700)
		assertPerm(t, mirror+".lock", 0o600)
		assertPerm(t, filepath.Join(mirror, "wikictl.lock"), 0o600)
		// The configuration holds the URL of the repository, which may carry
		// credentials, so it is private to the owner; an existing mirror whose
		// configuration others can read is tightened when it is opened again.
		config := filepath.Join(mirror, "config")
		assertPerm(t, config, 0o600)
		if err := os.Chmod(config, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(mirror, remote, ""); err != nil {
			t.Fatal(err)
		}
		assertPerm(t, config, 0o600)
	}
}

// TestCommitIndexInMirror checks that the temporary index does not use the
// system temporary directory and is removed after the commit.
func TestCommitIndexInMirror(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	empty := ""
	if _, err := r.Commit([]Change{{Path: "global/p.md", Content: []byte("---\nsummary: p\n---\n"), Base: &empty}}, "put", Author{"a", "a@a"}); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "wikictl-index-") {
			t.Errorf("temporary index left in the mirror: %s", e.Name())
		}
	}
}
