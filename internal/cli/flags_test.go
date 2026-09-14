package cli

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitCommon(t *testing.T) {
	cmd := newFlagSet("put")
	putFlags(cmd)
	cmd.Bool("any", false, "")
	cases := []struct {
		in, rest, common []string
	}{
		{[]string{"json", "config", "x", "--json"}, []string{"json", "config", "x"}, []string{"--json"}},
		{[]string{"-m", "json", "a"}, []string{"-m", "json", "a"}, nil},
		{[]string{"-m", "--json", "a"}, []string{"-m", "--json", "a"}, nil},
		{[]string{"--base=s", "-m", "--dirs", "a", "--dirs", "x"}, []string{"--base=s", "-m", "--dirs", "a"}, []string{"--dirs", "x"}},
		{[]string{"--any", "--json", "a"}, []string{"--any", "a"}, []string{"--json"}},
		{[]string{"a", "-m", "--json"}, []string{"a", "-m"}, []string{"--json"}},
		{[]string{"-m"}, []string{"-m"}, nil},
		{[]string{"a", "--json", "b"}, []string{"a", "b"}, []string{"--json"}},
		{[]string{"--dirs", "x,y", "a", "-no-fetch"}, []string{"a"}, []string{"--dirs", "x,y", "-no-fetch"}},
		{[]string{"--config=c.yaml", "--version"}, nil, []string{"--config=c.yaml", "--version"}},
		{[]string{"--any", "--", "--json"}, []string{"--any", "--", "--json"}, nil},
		{[]string{"--dirs"}, []string{"--dirs"}, nil},
		{[]string{"a", "--profile", "work", "--profile=home"}, []string{"a"}, []string{"--profile", "work", "--profile=home"}},
	}
	for _, c := range cases {
		rest, common := splitCommon(c.in, cmd)
		if !reflect.DeepEqual(rest, c.rest) || !reflect.DeepEqual(common, c.common) {
			t.Errorf("%v: rest=%v common=%v", c.in, rest, common)
		}
	}
}

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
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	out, err := exec.Command("git", "--git-dir", remote, "log", "-1", "--format=%s", "main").Output()
	if err != nil || strings.TrimSpace(string(out)) != "json" {
		t.Errorf("commit message: %q %v", out, err)
	}
}

func TestWantsHelp(t *testing.T) {
	if !wantsHelp([]string{"x", "--help"}) || wantsHelp([]string{"--", "-h"}) || wantsHelp([]string{"x"}) {
		t.Error("wantsHelp")
	}
}
