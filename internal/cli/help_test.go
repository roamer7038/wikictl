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

func TestHelpNeedsNoConfig(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		code, out, _ := runNoConfig(t, args...)
		if code != ExitOK || !strings.Contains(out, "Usage: wikictl") || !strings.Contains(out, "  search ") {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
	for _, args := range [][]string{{"help", "put"}, {"put", "--help"}, {"put", "-h"}} {
		code, out, _ := runNoConfig(t, args...)
		if code != ExitOK || !strings.Contains(out, "Usage: wikictl put") {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
	if code, out, _ := runNoConfig(t, "get", "-h"); code != ExitOK || !strings.Contains(out, "Usage: wikictl get <path>") {
		t.Errorf("get -h: code=%d out=%q", code, out)
	}
	_, out, _ := runNoConfig(t, "put", "--help")
	if !strings.Contains(out, "--base <sha>") || !strings.Contains(out, "-m <message>") {
		t.Errorf("put help must list flags: %q", out)
	}
	if code, _, errs := runNoConfig(t); code != ExitUsage || !strings.Contains(errs, "Usage: wikictl") {
		t.Errorf("no command: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runNoConfig(t, "help", "bogus"); code != ExitUsage || !strings.Contains(errs, "unknown command") {
		t.Errorf("help bogus: code=%d errs=%q", code, errs)
	}
	if code, _, errs := runNoConfig(t, "bogus"); code != ExitUsage || !strings.Contains(errs, "unknown command") {
		t.Errorf("bogus: code=%d errs=%q", code, errs)
	}
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
	code, _, errs := runNoConfig(t, "get")
	if code != ExitUsage || !strings.Contains(errs, "Usage: wikictl get <path>") {
		t.Errorf("get: code=%d errs=%q", code, errs)
	}
	code, _, errs = runNoConfig(t, "put", "--bogus", "global/x.md")
	if code != ExitUsage || !strings.Contains(errs, "-bogus") {
		t.Errorf("put --bogus: code=%d errs=%q", code, errs)
	}
	code, _, errs = runNoConfig(t, "mv", "only-one")
	if code != ExitUsage || !strings.Contains(errs, "Usage: wikictl mv") {
		t.Errorf("mv: code=%d errs=%q", code, errs)
	}
	code, _, errs = runNoConfig(t, "search", "-n", "0", "lease")
	if code != ExitUsage || !strings.Contains(errs, "Usage: wikictl search") {
		t.Errorf("search -n 0: code=%d errs=%q", code, errs)
	}
	code, out, _ := runNoConfig(t, "--json", "get")
	if code != ExitUsage || !strings.HasPrefix(out, `{"error":"usage"`) {
		t.Errorf("json usage error: code=%d out=%q", code, out)
	}
}

func TestHelpDescribesNameRules(t *testing.T) {
	_, out, _ := runNoConfig(t, "help", "lint")
	for _, w := range []string{"bad_path", "name_style", "case_collision", `" \ # ? : ( ) ` + "`"} {
		if !strings.Contains(out, w) {
			t.Errorf("help lint must mention %s: %q", w, out)
		}
	}
	for _, c := range []string{"put", "mv"} {
		_, out, _ := runNoConfig(t, "help", c)
		if strings.Contains(out, "slug") || !strings.Contains(out, "file name") {
			t.Errorf("help %s must describe the file name rules: %q", c, out)
		}
	}
}

func TestEveryCommandHasHelp(t *testing.T) {
	for _, c := range commands {
		code, out, _ := runNoConfig(t, "help", c.name)
		if code != ExitOK || !strings.Contains(out, "Usage: wikictl "+c.name) || c.summary == "" || c.detail == "" {
			t.Errorf("%s: code=%d out=%q", c.name, code, out)
		}
	}
}
