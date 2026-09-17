package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roamer7038/wikictl/internal/config"
	"github.com/roamer7038/wikictl/internal/page"
)

func TestEscapeControl(t *testing.T) {
	for in, want := range map[string]string{
		"plain\ttab":         "plain\ttab",
		"ok\x1b]52;c;eA==\a": `ok\x1b]52;c;eA==\x07`,
		"a\r\nb\x7f":         `a\x0d\x0ab\x7f`,
		"c1 \u009b2K":        `c1 \x9b2K`,
		"raw \x9b byte":      `raw \x9b byte`,
		"日本語":                "日本語",
		"invalid \xff stays": "invalid \xff stays",
	} {
		if got := escapeControl(in); got != want {
			t.Errorf("escapeControl(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeMessage(t *testing.T) {
	for in, want := range map[string]string{
		"one line":                   "one line",
		"a\nwikictl: b\n":            "a\n  wikictl: b",
		"x\x1b[2K\r\ny\n\n":          "x\\x1b[2K\\x0d\n  y",
		"path d/n\nl.md: not a file": "path d/n\n  l.md: not a file",
	} {
		if got := escapeMessage(in); got != want {
			t.Errorf("escapeMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextOutputEscapesControl(t *testing.T) {
	cfg := setup(t)
	summary := "ok\x1b]52;c;ZWNobyBwd24=\x07\x1b[2K\rfake"
	content := "---\nsummary: \"ok\\e]52;c;ZWNobyBwd24=\\a\\e[2K\\rfake\"\n---\n# t\x1b[31m\nlease\n[x](missing\x1b[2K.md)\n"
	if code, _, errs := runCLI(t, cfg, content, "put", "global/ctl.md"); code != 0 {
		t.Fatalf("put: %d %s", code, errs)
	}
	escaped := `ok\x1b]52;c;ZWNobyBwd24=\x07\x1b[2K\x0dfake`
	if _, out, _ := runCLI(t, cfg, "", "ls", "-l", "global"); strings.ContainsAny(out, "\x1b\x07\r") || !strings.Contains(out, escaped+"\n") {
		t.Errorf("ls -l: %q", out)
	}
	_, out, _ := runCLI(t, cfg, "", "ls", "--json", "global")
	var res struct {
		Items []struct{ Path, Summary string }
	}
	mustUnmarshal(t, out, &res)
	found := false
	for _, it := range res.Items {
		found = found || (it.Path == "global/ctl.md" && it.Summary == summary)
	}
	if !found {
		t.Errorf("ls --json must keep the summary: %s", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "grep", "31m", "global"); out != `global/ctl.md:# t\x1b[31m`+"\n" {
		t.Errorf("grep must escape the line: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "grep", "--json", "31m", "global"); !strings.Contains(out, `"text":"# t\u001b[31m"`) {
		t.Errorf("grep --json must keep the line: %s", out)
	}

	runCLI(t, cfg, "# only title\x1b[2K\n", "put", "global/notitle.md")
	runCLI(t, cfg, "---\ntype: \"\\e[2J\"\n---\n# typ\n", "put", "global/typ.md")
	_, out, _ = runCLI(t, cfg, "", "ls", "-l", "global")
	if !strings.Contains(out, "notitle.md  only title\\x1b[2K\n") {
		t.Errorf("ls title: %q", out)
	}
	if strings.Contains(out, "\x1b") || !strings.Contains(out, `\x1b[2J`) {
		t.Errorf("ls -l must escape the type: %q", out)
	}

	_, out, _ = runCLI(t, cfg, "", "lint", "global/ctl.md")
	if strings.Contains(out, "\x1b") || !strings.Contains(out, `missing\x1b[2K.md`) {
		t.Errorf("lint: %q", out)
	}

	_, out, _ = runCLI(t, cfg, "", "cat", "global/ctl.md")
	if out != content {
		t.Errorf("cat must print the file unchanged: %q", out)
	}
	_, out, _ = runCLI(t, cfg, "", "stat", "global/ctl.md")
	if strings.ContainsAny(out, "\x1b\x07\r") || !strings.Contains(out, "summary: "+escaped+"\n") || !strings.Contains(out, `title: t\x1b[31m`) {
		t.Errorf("stat: %q", out)
	}

	linkPage := "# t\n## Links\n- see_also: [x](missing\x1b[2K.md) | note\x07\n"
	code, _, errs := runCLI(t, cfg, linkPage, "put", "global/linkctl.md")
	if code != 0 {
		t.Fatalf("put linkctl: %d %s", code, errs)
	}
	if strings.ContainsAny(errs, "\x1b\x07\r") || !strings.Contains(errs, `missing\x1b[2K.md`) {
		t.Errorf("put warning: %q", errs)
	}
	_, out, _ = runCLI(t, cfg, "", "links", "global/linkctl.md")
	if out != "out\tsee_also\tglobal/missing\\x1b[2K.md\n" {
		t.Errorf("links must escape the target: %q", out)
	}
}

func TestPathOutputEscapesControl(t *testing.T) {
	cfg := setup(t)
	c1 := "global/c\u009b31mX.md"
	pushFiles(t, cfg, map[string]string{
		c1:                  "---\nsummary: \"t\\e[31m\\r\\n\"\n---\n# c\n",
		"global/d\x01/a.md": "---\nsummary: a\n---\n# a\n",
	})
	raw := "\u009b\x01\x1b\r"

	_, out, _ := runCLI(t, cfg, "", "lint")
	if strings.ContainsAny(out, raw) || !strings.Contains(out, `global/c\x9b31mX.md:0: bad_path: `) {
		t.Errorf("lint: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "lint", "--json"); !strings.Contains(out, `"path":"`+c1+`"`) {
		t.Errorf("lint --json must keep the path: %s", out)
	}

	if _, out, _ := runCLI(t, cfg, "", "ls", "-l", "global"); strings.ContainsAny(out, raw) || !strings.Contains(out, `t\x1b[31m\x0d\x0a`) || !strings.Contains(out, `d\x01/`) {
		t.Errorf("ls -l: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "find", "global", "-type", "d"); out != "global\nglobal/d\\x01\n" {
		t.Errorf("find: %q", out)
	}

	code, _, errs := runCLI(t, cfg, "", "mv", "global", "other")
	if code != ExitInvalid || strings.ContainsAny(errs, raw) || !strings.Contains(errs, `wikictl: bad_path: other/c\x9b31mX.md: `) {
		t.Errorf("mv: code=%d errs=%q", code, errs)
	}
	if _, out, _ := runCLI(t, cfg, "", "mv", "--json", "global", "other"); !strings.Contains(out, `"message":"bad_path: other/c`+"\u009b"+`31mX.md: `) {
		t.Errorf("mv --json must keep the message: %s", out)
	}

	var errb bytes.Buffer
	a := &app{stderr: &errb, cfg: &config.Config{}}
	a.warn(page.Issue{Path: "d/\x1b[2Kx.md", Line: 1, Code: "links_syntax", Message: "m\r"})
	a.flushWarnings()
	if got := errb.String(); got != `wikictl: warning: d/\x1b[2Kx.md:1: links_syntax: m\x0d`+"\n" {
		t.Errorf("warn: %q", got)
	}

	ctl := filepath.Join(t.TempDir(), "c\x1b[31m.yaml")
	b, _ := os.ReadFile(cfg)
	os.WriteFile(ctl, append(b, "extra: 1\n"...), 0o600)
	_, out, errs = runCLI(t, ctl, "", "--no-fetch", "context")
	if strings.ContainsAny(out+errs, raw) || !strings.Contains(errs, `c\x1b[31m.yaml: unknown key`) || !strings.Contains(out, `c\x1b[31m.yaml`+"\n") {
		t.Errorf("context: out=%q errs=%q", out, errs)
	}
	if _, out, _ := runCLI(t, ctl, "", "--no-fetch", "--json", "context"); !strings.Contains(out, `c\u001b[31m.yaml"`) {
		t.Errorf("context --json must keep the path: %s", out)
	}
}
