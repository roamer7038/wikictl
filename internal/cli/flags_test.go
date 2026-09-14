package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Words without a leading dash, such as "json" or "config", are arguments or
// flag values, not global flags.
func TestBareWordsAreNotGlobalFlags(t *testing.T) {
	cfg := setup(t)
	var res struct{ Items []struct{ Path string } }
	for _, args := range [][]string{
		{"search", "--json", "json"},
		{"search", "--json", "config", "file"},
		{"search", "--json", "version"},
		{"search", "--json", "-n", "5", "profile", "dirs"},
	} {
		code, out, errs := runCLI(t, cfg, "", args...)
		if code != 0 {
			t.Errorf("%v: code=%d %s", args, code, errs)
			continue
		}
		mustUnmarshal(t, out, &res)
	}
	// Search terms are ANDed, so "version" must be used as a term.
	code, sout, errs := runCLI(t, cfg, "", "search", "--json", "lease", "version")
	if code != 0 {
		t.Fatalf("search lease version: code=%d %s", code, errs)
	}
	res.Items = nil
	mustUnmarshal(t, sout, &res)
	if len(res.Items) != 0 {
		t.Errorf("search lease version: items=%v", res.Items)
	}
	if code, _, _ := runCLI(t, cfg, "", "ls", "json"); code != ExitUsage {
		t.Errorf("ls json: code=%d, want %d", code, ExitUsage)
	}
	code, _, errs = runCLI(t, cfg, "---\nsummary: k\n---\n# k\n", "put", "-m", "json", "global/k.md")
	if code != 0 {
		t.Fatalf("put -m json: code=%d %s", code, errs)
	}
	if got := lastCommitMessage(t, cfg); got != "json" {
		t.Errorf("commit message: %q", got)
	}
}

// Global and command flags may follow the positional arguments, and every
// argument after "--" is positional.
func TestFlagsAfterArguments(t *testing.T) {
	cfg := setup(t)
	var res struct{ Items []struct{ Path string } }
	code, out, errs := runCLI(t, cfg, "", "search", "lease", "--dirs", "global,projects/app", "--json", "-n", "1")
	if code != 0 {
		t.Fatalf("search with flags after the word: code=%d %s", code, errs)
	}
	mustUnmarshal(t, out, &res)
	if len(res.Items) != 1 {
		t.Errorf("search -n 1 after the word: %s", out)
	}
	code, _, errs = runCLI(t, cfg, "---\nsummary: k\n---\n# k\n", "put", "global/k.md", "--message", "after")
	if code != 0 {
		t.Fatalf("put with -m after the path: code=%d %s", code, errs)
	}
	if got := lastCommitMessage(t, cfg); got != "after" {
		t.Errorf("commit message: %q", got)
	}
	// "--any" after "--" is a search word that no page contains.
	res.Items = nil
	code, out, errs = runCLI(t, cfg, "", "search", "--json", "--dirs", "global", "--", "lease", "--any")
	if code != 0 {
		t.Fatalf("search -- lease --any: code=%d %s", code, errs)
	}
	mustUnmarshal(t, out, &res)
	if len(res.Items) != 0 {
		t.Errorf("search -- lease --any: %s", out)
	}
	for _, args := range [][]string{{"search", "-json", "lease"}, {"search", "lease", "--bogus"}} {
		if code, _, errs := runCLI(t, cfg, "", args...); code != ExitUsage || !strings.Contains(errs, "Usage: wikictl search") {
			t.Errorf("%v: code=%d errs=%q", args, code, errs)
		}
	}
	for _, args := range [][]string{{"search", "--number", "1"}, {"search", "-n1"}, {"search", "-n=1"}} {
		res.Items = nil
		code, out, errs := runCLI(t, cfg, "", append(args, "--json", "lease")...)
		if code != 0 {
			t.Fatalf("%v: code=%d %s", args, code, errs)
		}
		if mustUnmarshal(t, out, &res); len(res.Items) != 1 {
			t.Errorf("%v: %s", args, out)
		}
	}
}

// A --json that follows the flag in error still selects JSON for the usage
// error, unless it is the value of a flag or comes after "--".
func TestUsageErrorJSONAfterTheError(t *testing.T) {
	for _, args := range [][]string{
		{"search", "--bogus", "--json"},
		{"search", "-n", "0", "--json", "x"},
		{"--bogus", "--json", "search"},
		{"help", "--bogus", "--json"},
	} {
		if code, out, _ := runNoConfig(t, args...); code != ExitUsage || !strings.HasPrefix(out, `{"error":"usage"`) {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
	for _, args := range [][]string{
		{"put", "--bogus", "-m", "--json", "global/x.md"},
		{"search", "--bogus", "--", "--json"},
	} {
		if code, out, errs := runNoConfig(t, args...); code != ExitUsage || out != "" || !strings.Contains(errs, "Usage: wikictl") {
			t.Errorf("%v: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
}

func TestHelpAndVersionFlags(t *testing.T) {
	if code, out, _ := runNoConfig(t, "help", "search", "-h"); code != ExitOK || !strings.Contains(out, "Usage: wikictl search") {
		t.Errorf("help search -h: code=%d out=%q", code, out)
	}
	if code, out, _ := runNoConfig(t, "help", "-h"); code != ExitOK || !strings.Contains(out, "Usage: wikictl help") {
		t.Errorf("help -h: code=%d out=%q", code, out)
	}
	if code, out, _ := runNoConfig(t, "search", "x", "--version"); code != ExitOK || !strings.HasPrefix(out, "wikictl ") {
		t.Errorf("search x --version: code=%d out=%q", code, out)
	}
	if code, _, errs := runNoConfig(t, "put", "global/x.md", "-m"); code != ExitUsage || !strings.Contains(errs, "needs an argument") {
		t.Errorf("put -m without a value: code=%d errs=%q", code, errs)
	}
}

func lastCommitMessage(t *testing.T, cfg string) string {
	t.Helper()
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	out, err := exec.Command("git", "--git-dir", remote, "log", "-1", "--format=%s", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
