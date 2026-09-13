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
		{"ls", "--dirs", "global"},
		{"search", "--dirs", "global", "lease"},
	} {
		_, out, _ := runCLI(t, cfg, "", args...)
		if strings.ContainsAny(out, "\x1b\x07\r") || !strings.Contains(out, "global/ctl.md\t"+escaped+"\n") {
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

	runCLI(t, cfg, "---\nsummary: \"\\e[2Kidx\"\n---\n", "put", "--base", getSha(t, cfg, "global/index.md"), "global/index.md")
	_, out, _ := runCLI(t, cfg, "", "dirs", "global")
	if strings.Contains(out, "\x1b") || !strings.Contains(out, `\x1b[2Kidx`) {
		t.Errorf("dirs: %q", out)
	}

	runCLI(t, cfg, "# only title\x1b[2K\n", "put", "global/notitle.md")
	_, out, _ = runCLI(t, cfg, "", "ls", "--dirs", "global")
	if !strings.Contains(out, "global/notitle.md\tonly title\\x1b[2K\n") {
		t.Errorf("ls title: %q", out)
	}

	_, out, _ = runCLI(t, cfg, "", "lint", "global/ctl.md")
	if strings.Contains(out, "\x1b") || !strings.Contains(out, `missing\x1b[2K.md`) {
		t.Errorf("lint: %q", out)
	}

	_, out, _ = runCLI(t, cfg, "", "get", "global/ctl.md")
	if !strings.Contains(out, "# t\x1b[31m") {
		t.Errorf("get body must be unchanged: %q", out)
	}
}
