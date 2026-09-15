package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runNoConfig invokes Main with a config path that does not exist, so that
// anything it verifies works without configuration.
func runNoConfig(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("WIKICTL_CONFIG", "/nonexistent/wikictl/config.yaml")
	var out, errb bytes.Buffer
	code := Main(args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		code, out, _ := runNoConfig(t, args...)
		if code != ExitOK || !strings.HasPrefix(out, "wikictl ") {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
	code, out, _ := runNoConfig(t, "--json", "version")
	if code != ExitOK || !strings.HasPrefix(out, `{"version":"`) {
		t.Errorf("json: code=%d out=%q", code, out)
	}
	if Version() == "" {
		t.Error("Version must not be empty")
	}
}

func TestUsageErrorsNeedNoConfig(t *testing.T) {
	if code, _, errs := runNoConfig(t); code != ExitUsage || !strings.Contains(errs, "Usage: wikictl") {
		t.Errorf("no command: code=%d errs=%q", code, errs)
	}
	for _, args := range [][]string{{"bogus"}, {"help", "bogus"}} {
		if code, _, errs := runNoConfig(t, args...); code != ExitUsage || !strings.Contains(errs, "unknown command") {
			t.Errorf("%v: code=%d errs=%q", args, code, errs)
		}
	}
	code, _, errs := runNoConfig(t, "links")
	if code != ExitUsage || !strings.Contains(errs, "Usage: wikictl links [flags] <path>") {
		t.Errorf("links: code=%d errs=%q", code, errs)
	}
	code, _, errs = runNoConfig(t, "put", "--bogus", "global/x.md")
	if code != ExitUsage || !strings.Contains(errs, "-bogus") {
		t.Errorf("put --bogus: code=%d errs=%q", code, errs)
	}
	code, _, errs = runNoConfig(t, "mv", "only-one")
	if code != ExitUsage || !strings.Contains(errs, "Usage: wikictl mv") {
		t.Errorf("mv: code=%d errs=%q", code, errs)
	}
	code, _, errs = runNoConfig(t, "grep", "-i")
	if code != ExitUsage || !strings.Contains(errs, "missing pattern") || !strings.Contains(errs, "Usage: wikictl grep") {
		t.Errorf("grep without a pattern: code=%d errs=%q", code, errs)
	}
	for _, args := range [][]string{
		{"grep", "foo", "../x"},
		{"grep", "-e", "foo", "../x"},
		{"mv", "a/b.md", "../x"},
		{"mv", "../x", "a/"},
		{"mv", "-t", "../x", "a.md"},
	} {
		code, _, errs := runNoConfig(t, args...)
		if code != ExitInvalid || !strings.Contains(errs, "bad_path") {
			t.Errorf("%v: code=%d errs=%q", args, code, errs)
		}
	}
	code, _, errs = runNoConfig(t, "tree", "-L", "0")
	if code != ExitUsage || !strings.Contains(errs, "must be at least 1") || !strings.Contains(errs, "Usage: wikictl tree") {
		t.Errorf("tree -L 0: code=%d errs=%q", code, errs)
	}
	code, out, _ := runNoConfig(t, "--json", "cat")
	if code != ExitUsage || !strings.HasPrefix(out, `{"error":"usage"`) {
		t.Errorf("json usage error: code=%d out=%q", code, out)
	}
}

func TestEveryCommandHasHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		code, out, _ := runNoConfig(t, args...)
		if code != ExitOK || !strings.Contains(out, "Usage: wikictl") || !strings.Contains(out, "  grep ") {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
	for _, c := range commands {
		for _, args := range [][]string{{"help", c.name}, {c.name, "--help"}} {
			code, out, _ := runNoConfig(t, args...)
			if code != ExitOK || !strings.Contains(out, "Usage: wikictl "+c.name) || c.summary == "" || c.detail == "" {
				t.Errorf("%v: code=%d out=%q", args, code, out)
			}
		}
	}
	if _, out, _ := runNoConfig(t, "put", "-h"); !strings.Contains(out, "--base <sha>") || !strings.Contains(out, "-m, --message <message>") {
		t.Errorf("put help must list flags: %q", out)
	}
}
