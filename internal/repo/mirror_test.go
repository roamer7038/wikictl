package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// fetchTraceCount reads the number of "git fetch" invocations GIT_TRACE
// logged to trace since the last call, and empties the file, as
// TestFetchStaleLockNotRetried does inline.
func fetchTraceCount(t *testing.T, trace string) int {
	t.Helper()
	b, err := os.ReadFile(trace)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(trace); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(b), "built-in: git fetch ")
}

// TestFetchRecordsFetchedAt checks that FetchedAt is zero before the first
// Fetch and holds a recent time after one, so that "wikictl context" can show
// when the mirror was last fetched.
func TestFetchRecordsFetchedAt(t *testing.T) {
	remote := newRemote(t, true)
	r, err := Open(filepath.Join(t.TempDir(), "m"), remote, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.FetchedAt(); !got.IsZero() {
		t.Errorf("FetchedAt before Fetch = %v, want zero", got)
	}
	before := time.Now()
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	got := r.FetchedAt()
	if got.IsZero() || got.Before(before.Add(-time.Second)) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("FetchedAt after Fetch = %v, want close to %v", got, before)
	}
}

// TestFetchIfStale checks that a read within fetch_ttl of a recorded fetch
// skips the git fetch, that ttl <= 0 always fetches like Fetch, that a
// marker file whose modification time is in the future is not mistaken for
// a recent fetch, and that a fetch older than ttl runs again.
func TestFetchIfStale(t *testing.T) {
	remote := newRemote(t, true)
	r := openFetched(t, remote)
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)

	// ttl <= 0 disables the skip: it always fetches, as when fetch_ttl is not
	// configured.
	if err := r.FetchIfStale(0); err != nil {
		t.Fatal(err)
	}
	if n := fetchTraceCount(t, trace); n != 1 {
		t.Errorf("ttl<=0: git fetch ran %d times, want 1", n)
	}

	// Within ttl of the fetch just recorded: no fetch runs.
	if err := r.FetchIfStale(time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := fetchTraceCount(t, trace); n != 0 {
		t.Errorf("within ttl: git fetch ran %d times, want 0", n)
	}

	// A marker mtime in the future must not be read as "just fetched": with
	// time.Since negative, elapsed >= 0 is false, so the fetch still runs.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(r.fetchedMarker(), future, future); err != nil {
		t.Fatal(err)
	}
	if err := r.FetchIfStale(time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := fetchTraceCount(t, trace); n != 1 {
		t.Errorf("future mtime: git fetch ran %d times, want 1", n)
	}

	// A fetch older than ttl runs again.
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(r.fetchedMarker(), past, past); err != nil {
		t.Fatal(err)
	}
	if err := r.FetchIfStale(time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := fetchTraceCount(t, trace); n != 1 {
		t.Errorf("ttl expired: git fetch ran %d times, want 1", n)
	}
}

// TestFetchIfStaleNoTrackingRef checks that FetchIfStale always fetches when
// the tracking ref does not exist yet, even right after a fetch recorded
// FetchedAt: without the ref, Snapshot leaves reads finding no file at all,
// so a fetch must never be skipped on the strength of the marker alone.
func TestFetchIfStaleNoTrackingRef(t *testing.T) {
	remote := newRemote(t, false)
	r := openFetched(t, remote)
	if got, err := r.trackingHead(); err != nil || got != "" {
		t.Fatalf("trackingHead = %q, %v; want \"\", nil", got, err)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)
	if err := r.FetchIfStale(time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := fetchTraceCount(t, trace); n != 1 {
		t.Errorf("first call: git fetch ran %d times, want 1", n)
	}
	if err := r.FetchIfStale(time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := fetchTraceCount(t, trace); n != 1 {
		t.Errorf("second call: git fetch ran %d times, want 1", n)
	}
}

// lsRemoteTraceCount reads the number of "git ls-remote" invocations GIT_TRACE
// logged to trace since the last call, and empties the file, as
// fetchTraceCount does for git fetch.
func lsRemoteTraceCount(t *testing.T, trace string) int {
	t.Helper()
	b, err := os.ReadFile(trace)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(trace); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(b), "built-in: git ls-remote ")
}

// savedBranch returns the branch saved in the mirror's git configuration, or
// "" when none is saved.
func savedBranch(t *testing.T, r *Repo) string {
	t.Helper()
	out, _ := r.Git("config", "--get", "wikictl.branch")
	return strings.TrimSpace(out)
}

// TestOpenDoesNotSaveGivenBranch checks that a branch given to Open, which
// comes from the configuration, is used but never saved in the mirror. One
// mirror is shared by every profile and configuration file naming the same
// repository, so a saved branch would be read by the ones that name no
// branch, making them read and write a branch they never asked for.
func TestOpenDoesNotSaveGivenBranch(t *testing.T) {
	remote := newRemote(t, true)
	run(t, "", "git", "--git-dir", remote, "branch", "docs", "main")
	dir := filepath.Join(t.TempDir(), "m")

	r, err := Open(dir, remote, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "docs" {
		t.Errorf("Branch with branch=docs = %q, want docs", r.Branch)
	}
	if got := savedBranch(t, r); got != "" {
		t.Errorf("wikictl.branch = %q, want it unset: a branch given in the configuration must not be saved", got)
	}

	// The same mirror, opened without a branch as another profile would: the
	// remote HEAD is used, not the branch the previous Open was given.
	r, err = Open(dir, remote, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "main" {
		t.Errorf("Branch with no branch given = %q, want main (the remote HEAD)", r.Branch)
	}
}

// TestOpenSavesDetectedBranch checks that the branch detected from the remote
// HEAD is saved in the mirror, so that a later Open reads it instead of
// querying the remote again.
func TestOpenSavesDetectedBranch(t *testing.T) {
	remote := newRemote(t, true)
	dir := filepath.Join(t.TempDir(), "m")
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)

	r, err := Open(dir, remote, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "main" {
		t.Errorf("Branch of the first Open = %q, want main", r.Branch)
	}
	if n := lsRemoteTraceCount(t, trace); n != 1 {
		t.Errorf("first Open: git ls-remote ran %d times, want 1", n)
	}
	if got := savedBranch(t, r); got != "main" {
		t.Errorf("wikictl.branch = %q, want main", got)
	}

	r, err = Open(dir, remote, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "main" {
		t.Errorf("Branch of the second Open = %q, want main", r.Branch)
	}
	if n := lsRemoteTraceCount(t, trace); n != 0 {
		t.Errorf("second Open: git ls-remote ran %d times, want 0", n)
	}
}
