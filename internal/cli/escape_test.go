package cli

import (
	"strings"
	"testing"
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

func TestTextOutputEscapesControl(t *testing.T) {
	cfg := setup(t)
	summary := "ok\x1b]52;c;ZWNobyBwd24=\x07\x1b[2K\rfake"
	content := "---\nsummary: \"ok\\e]52;c;ZWNobyBwd24=\\a\\e[2K\\rfake\"\n---\n# t\x1b[31m\nlease\n[x](missing\x1b[2K.md)\n"
	if code, _, errs := runCLI(t, cfg, content, "put", "global/ctl.md"); code != 0 {
		t.Fatalf("put: %d %s", code, errs)
	}
	escaped := `ok\x1b]52;c;ZWNobyBwd24=\x07\x1b[2K\x0dfake`
	for _, args := range [][]string{
		{"ls", "-l", "global"},
		{"search", "lease"},
	} {
		_, out, _ := runCLI(t, cfg, "", args...)
		if strings.ContainsAny(out, "\x1b\x07\r") || !strings.Contains(out, escaped+"\n") {
			t.Errorf("%v: %q", args, out)
		}
		_, out, _ = runCLI(t, cfg, "", append(args, "--json")...)
		var res struct {
			Items []struct{ Path, Summary string }
		}
		mustUnmarshal(t, out, &res)
		found := false
		for _, it := range res.Items {
			found = found || (it.Path == "global/ctl.md" && it.Summary == summary)
		}
		if !found {
			t.Errorf("%v --json must keep the summary: %s", args, out)
		}
	}

	runCLI(t, cfg, "# only title\x1b[2K\n", "put", "global/notitle.md")
	_, out, _ := runCLI(t, cfg, "", "ls", "-l", "global")
	if !strings.Contains(out, "notitle.md  only title\\x1b[2K\n") {
		t.Errorf("ls title: %q", out)
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
