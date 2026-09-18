package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// setupTTL creates a wiki as setup does, with fetch_ttl added to the
// top-level configuration.
func setupTTL(t *testing.T, ttl int) string {
	t.Helper()
	cfg := setup(t)
	appendConfig(t, cfg, "fetch_ttl: "+strconv.Itoa(ttl)+"\n")
	return cfg
}

// appendConfig appends text to the configuration file at cfg.
func appendConfig(t *testing.T, cfg, text string) {
	t.Helper()
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(b, []byte(text)...), 0o600); err != nil {
		t.Fatal(err)
	}
}

// recordFetch puts a git wrapper first on PATH that appends a line to the
// returned file for every "git fetch -q origin ..." it runs, as recordGrep
// does for git grep.
func recordFetch(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "fetches")
	wrapGit(t, func(real string) string {
		return "*' fetch -q origin '*)\n" +
			"  echo run >> " + shQuote(log) + "\n" +
			"  exec " + real + " \"$@\"\n" +
			"  ;;\n"
	})
	return log
}

// fetches returns the number of git fetches recordFetch logged, and empties
// the log.
func fetches(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

// TestFetchTTLDefaultAlwaysFetches checks that without fetch_ttl in the
// configuration, every read still fetches, as before this feature existed.
func TestFetchTTLDefaultAlwaysFetches(t *testing.T) {
	cfg := setup(t)
	log := recordFetch(t)
	if code, _, errs := runCLI(t, cfg, "", "cat", "global/push.md"); code != ExitOK {
		t.Fatalf("cat: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 1 {
		t.Errorf("first cat: git fetch ran %d times, want 1", n)
	}
	if code, _, errs := runCLI(t, cfg, "", "cat", "global/push.md"); code != ExitOK {
		t.Fatalf("cat: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 1 {
		t.Errorf("second cat without fetch_ttl: git fetch ran %d times, want 1", n)
	}
}

// TestFetchTTLSkipsRead checks that a read within fetch_ttl of the last
// recorded fetch does not fetch again.
func TestFetchTTLSkipsRead(t *testing.T) {
	cfg := setupTTL(t, 3600)
	log := recordFetch(t)
	if code, _, errs := runCLI(t, cfg, "", "cat", "global/push.md"); code != ExitOK {
		t.Fatalf("cat: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 1 {
		t.Errorf("first cat: git fetch ran %d times, want 1", n)
	}
	if code, _, errs := runCLI(t, cfg, "", "stat", "global/push.md"); code != ExitOK {
		t.Fatalf("stat: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 0 {
		t.Errorf("second read within fetch_ttl: git fetch ran %d times, want 0", n)
	}
}

// TestFetchTTLWritesAlwaysFetch checks that put and rm always fetch even
// within fetch_ttl of the last recorded fetch: each writes twice, once from
// openRepo and once from Commit, so the count is 2, not 1.
func TestFetchTTLWritesAlwaysFetch(t *testing.T) {
	cfg := setupTTL(t, 3600)
	log := recordFetch(t)
	if code, _, errs := runCLI(t, cfg, "", "cat", "global/push.md"); code != ExitOK {
		t.Fatalf("cat: code=%d errs=%q", code, errs)
	}
	fetches(t, log) // drain the read's fetch

	newPage := "---\nsummary: n\n---\n# n\n"
	if code, _, errs := runCLI(t, cfg, newPage, "put", "global/new.md"); code != ExitOK {
		t.Fatalf("put: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 2 {
		t.Errorf("put within fetch_ttl: git fetch ran %d times, want 2 (openRepo and Commit)", n)
	}

	if code, _, errs := runCLI(t, cfg, "", "rm", "global/new.md"); code != ExitOK {
		t.Fatalf("rm: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 2 {
		t.Errorf("rm within fetch_ttl: git fetch ran %d times, want 2 (openRepo and Commit)", n)
	}
}

// TestFetchTTLNoTrackingRef checks that fetch_ttl is not applied while the
// tracking ref does not exist yet, such as right after the mirror of an
// empty repository was created: every read still fetches.
func TestFetchTTLNoTrackingRef(t *testing.T) {
	cfg := setupEmpty(t)
	appendConfig(t, cfg, "fetch_ttl: 3600\n")
	log := recordFetch(t)
	if code, _, errs := runCLI(t, cfg, "", "context"); code != ExitOK {
		t.Fatalf("context: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 1 {
		t.Errorf("first context on an empty repo: git fetch ran %d times, want 1", n)
	}
	if code, _, errs := runCLI(t, cfg, "", "context"); code != ExitOK {
		t.Fatalf("context: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 1 {
		t.Errorf("second context on an empty repo: git fetch ran %d times, want 1 (no tracking ref)", n)
	}
}

// TestFetchTTLProfileZeroOverridesTopLevel checks that a profile can set
// fetch_ttl to 0 to disable a top-level value, distinguishing "unset" from 0.
func TestFetchTTLProfileZeroOverridesTopLevel(t *testing.T) {
	cfg := setup(t)
	appendConfig(t, cfg, "fetch_ttl: 3600\nprofiles:\n  off:\n    fetch_ttl: 0\n")
	log := recordFetch(t)
	if code, _, errs := runCLI(t, cfg, "", "--profile", "off", "cat", "global/push.md"); code != ExitOK {
		t.Fatalf("cat: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 1 {
		t.Errorf("first cat with profile off: git fetch ran %d times, want 1", n)
	}
	if code, _, errs := runCLI(t, cfg, "", "--profile", "off", "cat", "global/push.md"); code != ExitOK {
		t.Fatalf("cat: code=%d errs=%q", code, errs)
	}
	if n := fetches(t, log); n != 1 {
		t.Errorf("second cat with profile off: git fetch ran %d times, want 1 (fetch_ttl disabled by the profile)", n)
	}
}

// TestContextShowsFetchTTLAndFetched checks that context reports fetch_ttl
// (0 when not set) and fetched: the last fetch time recorded in the mirror,
// empty before the mirror has ever been fetched, RFC 3339 UTC after.
func TestContextShowsFetchTTLAndFetched(t *testing.T) {
	cfg := setupTTL(t, 42)
	var res struct {
		FetchTTL int    `json:"fetch_ttl"`
		Fetched  string `json:"fetched"`
	}
	code, out, errs := runCLI(t, cfg, "", "--no-fetch", "context", "--json")
	if code != ExitOK {
		t.Fatalf("context: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &res)
	if res.FetchTTL != 42 || res.Fetched != "" {
		t.Errorf("context before any fetch: %+v", res)
	}

	code, out, errs = runCLI(t, cfg, "", "context", "--json")
	if code != ExitOK {
		t.Fatalf("context: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &res)
	if res.FetchTTL != 42 || res.Fetched == "" {
		t.Errorf("context after a fetch: %+v", res)
	}
	if _, err := time.Parse(time.RFC3339, res.Fetched); err != nil {
		t.Errorf("fetched = %q, not RFC3339: %v", res.Fetched, err)
	}

	// Without fetch_ttl in the configuration, context reports 0.
	code, out, errs = runCLI(t, setup(t), "", "context", "--json")
	if code != ExitOK {
		t.Fatalf("context: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &res)
	if res.FetchTTL != 0 {
		t.Errorf("context without fetch_ttl: %+v", res)
	}
}
