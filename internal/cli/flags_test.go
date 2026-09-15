package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// Words without a leading dash, such as "json" or "config", are arguments or
// flag values, not global flags.
func TestBareWordsAreNotGlobalFlags(t *testing.T) {
	cfg := setup(t)
	for _, args := range [][]string{
		{"grep", "--json", "json"},
		{"grep", "--json", "config", "global"},
		{"grep", "--json", "-e", "version"},
		{"grep", "--json", "-n", "profile", "global"},
	} {
		code, out, errs := runCLI(t, cfg, "", args...)
		if code != ExitError || out != `{"items":[]}`+"\n" {
			t.Errorf("%v: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
	// A word after the pattern is a path.
	if code, _, errs := runCLI(t, cfg, "", "grep", "lease", "version"); code != ExitUsage || errs != "wikictl: version: no such file or directory\n" {
		t.Errorf("grep lease version: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runCLI(t, cfg, "", "ls", "json"); code != ExitError || errs != "wikictl: json: no such file\n" {
		t.Errorf("ls json: code=%d errs=%q", code, errs)
	}
	code, _, errs := runCLI(t, cfg, "---\nsummary: k\n---\n# k\n", "put", "-m", "json", "global/k.md")
	if code != 0 {
		t.Fatalf("put -m json: code=%d %s", code, errs)
	}
	if got := lastCommitMessage(t, cfg); got != "json" {
		t.Errorf("commit message: %q", got)
	}
}

// Global and command flags may follow the positional arguments, short flags
// may be combined, and every argument after "--" is positional.
func TestFlagsAfterArguments(t *testing.T) {
	cfg := setup(t)
	for _, args := range [][]string{
		{"grep", "lease", "global", "--json", "-l"},
		{"grep", "-il", "LEASE", "global"},
		{"grep", "--ignore-case", "--files-with-matches", "LEASE", "global"},
	} {
		code, out, errs := runCLI(t, cfg, "", args...)
		if code != 0 || !strings.Contains(out, "global/push.md") || strings.Contains(out, "projects/") {
			t.Errorf("%v: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
	code, _, errs := runCLI(t, cfg, "---\nsummary: k\n---\n# k\n", "put", "global/k.md", "--message", "after")
	if code != 0 {
		t.Fatalf("put with -m after the path: code=%d %s", code, errs)
	}
	if got := lastCommitMessage(t, cfg); got != "after" {
		t.Errorf("commit message: %q", got)
	}
	// "--nope" after "--" is the pattern, which no line contains.
	if code, out, errs := runCLI(t, cfg, "", "grep", "--json", "--", "--nope"); code != ExitError || out != `{"items":[]}`+"\n" {
		t.Errorf("grep -- --nope: code=%d out=%q errs=%q", code, out, errs)
	}
	for _, args := range [][]string{{"grep", "-json", "lease"}, {"grep", "lease", "--bogus"}} {
		if code, _, errs := runCLI(t, cfg, "", args...); code != ExitUsage || !strings.Contains(errs, "Usage: wikictl grep") {
			t.Errorf("%v: code=%d errs=%q", args, code, errs)
		}
	}
}

// A --json that follows the flag in error still selects JSON for the usage
// error, unless it is the value of a flag or comes after "--".
func TestUsageErrorJSONAfterTheError(t *testing.T) {
	for _, args := range [][]string{
		{"grep", "--bogus", "--json"},
		{"tree", "-L", "0", "--json"},
		{"--bogus", "--json", "grep"},
		{"help", "--bogus", "--json"},
	} {
		if code, out, _ := runNoConfig(t, args...); code != ExitUsage || !strings.HasPrefix(out, `{"error":"usage"`) {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
	for _, args := range [][]string{
		{"put", "--bogus", "-m", "--json", "global/x.md"},
		{"grep", "--bogus", "--", "--json"},
	} {
		if code, out, errs := runNoConfig(t, args...); code != ExitUsage || out != "" || !strings.Contains(errs, "Usage: wikictl") {
			t.Errorf("%v: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
}

func TestHelpAndVersionFlags(t *testing.T) {
	if code, out, _ := runNoConfig(t, "help", "grep", "-h"); code != ExitOK || !strings.Contains(out, "Usage: wikictl grep") {
		t.Errorf("help grep -h: code=%d out=%q", code, out)
	}
	if code, out, _ := runNoConfig(t, "grep", "x", "--help"); code != ExitOK || !strings.Contains(out, "Usage: wikictl grep") {
		t.Errorf("grep x --help: code=%d out=%q", code, out)
	}
	// -h of grep is --no-filename, as in GNU grep.
	if code, _, errs := runNoConfig(t, "grep", "-h"); code != ExitUsage || !strings.Contains(errs, "missing pattern") {
		t.Errorf("grep -h: code=%d errs=%q", code, errs)
	}
	if code, out, _ := runNoConfig(t, "-h", "grep"); code != ExitOK || !strings.Contains(out, "Usage: wikictl [global flags] <command>") {
		t.Errorf("-h grep: code=%d out=%q", code, out)
	}
	if code, out, _ := runNoConfig(t, "help", "-h"); code != ExitOK || !strings.Contains(out, "Usage: wikictl help") {
		t.Errorf("help -h: code=%d out=%q", code, out)
	}
	if code, out, _ := runNoConfig(t, "grep", "x", "--version"); code != ExitOK || !strings.HasPrefix(out, "wikictl ") {
		t.Errorf("grep x --version: code=%d out=%q", code, out)
	}
	if code, _, errs := runNoConfig(t, "put", "global/x.md", "-m"); code != ExitUsage || !strings.Contains(errs, "needs an argument") {
		t.Errorf("put -m without a value: code=%d errs=%q", code, errs)
	}
}

func lastCommitMessage(t *testing.T, cfg string) string {
	t.Helper()
	return gitOut(t, "--git-dir", filepath.Join(filepath.Dir(cfg), "remote.git"), "log", "-1", "--format=%s", "main")
}
