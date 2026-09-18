package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
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

// TestUnknownCommandHint checks that a name wikictl has no command for names
// the command that does its work, in the same wording whether the name was run
// or given to help.
func TestUnknownCommandHint(t *testing.T) {
	for name, want := range map[string]string{
		"search":    "wikictl: unknown command: search\n  the nearest command is grep; run \"wikictl help grep\" for its usage\n",
		"read":      "wikictl: unknown command: read\n  the nearest command is cat; run \"wikictl help cat\" for its usage\n",
		"list":      "wikictl: unknown command: list\n  the nearest command is ls; run \"wikictl help ls\" for its usage\n",
		"history":   "wikictl: unknown command: history\n  the nearest command is log; run \"wikictl help log\" for its usage\n",
		"show":      "wikictl: unknown command: show\n  the nearest command is cat --at; run \"wikictl help cat\" for its usage\n",
		"blame":     "wikictl: unknown command: blame\n  wikictl has no blame command yet; run \"wikictl help\" for the list of commands\n",
		"diff":      "wikictl: unknown command: diff\n  wikictl has no diff command yet; run \"wikictl help\" for the list of commands\n",
		"nosuchcmd": "wikictl: unknown command: nosuchcmd\n  run \"wikictl help\" for the list of commands\n",
	} {
		for _, args := range [][]string{{name}, {"help", name}} {
			if code, out, errs := runNoConfig(t, args...); code != ExitUsage || out != "" || errs != want {
				t.Errorf("%v: code=%d out=%q errs=%q, want %q", args, code, out, errs, want)
			}
		}
	}
	// With --json the guidance is part of the message, newline and all.
	want := `{"error":"usage","message":"unknown command: search\nthe nearest command is grep; run \"wikictl help grep\" for its usage"}` + "\n"
	if code, out, errs := runNoConfig(t, "--json", "search"); code != ExitUsage || out != want || errs != "" {
		t.Errorf("--json search: code=%d out=%q errs=%q, want %q", code, out, errs, want)
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
	code, _, errs := runNoConfig(t, "stat")
	if code != ExitUsage || !strings.Contains(errs, "Usage: wikictl stat <path>...") {
		t.Errorf("stat: code=%d errs=%q", code, errs)
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
	if code, out, _ := runNoConfig(t, "put", "-h"); code != ExitOK || !strings.Contains(out, "--base <sha>") || !strings.Contains(out, "-m, --message <message>") {
		t.Errorf("put -h must list flags: code=%d out=%q", code, out)
	}
	// -h of links is --no-filename, as it is in grep, so its help needs --help.
	if code, out, _ := runNoConfig(t, "links", "--help"); code != ExitOK || !strings.Contains(out, "Usage: wikictl links [flags] [<path>...]") {
		t.Errorf("links --help: code=%d out=%q", code, out)
	}
	if code, out, _ := runNoConfig(t, "links", "-h"); code == ExitOK {
		t.Errorf("links -h must not print the help: code=%d out=%q", code, out)
	}
	// Every issue code that lint reports is described in its help.
	_, out, _ := runNoConfig(t, "help", "lint")
	for _, code := range []string{"missing_summary", "frontmatter_invalid", "links_syntax", "bad_path", "name_style", "case_collision",
		"unicode_collision", "page_too_large", "broken_link"} {
		if !strings.Contains(out, code) {
			t.Errorf("help lint must describe %s", code)
		}
	}
}

// TestHelpNotesFilesAddedWhileReading checks that mv and rm both say that a
// file added after the command read the tree is left where it is.
func TestHelpNotesFilesAddedWhileReading(t *testing.T) {
	for cmd, want := range map[string]string{
		"mv": "A file added under a directory after mv read it is not moved.",
		"rm": "A file added under a directory after rm read it is not deleted.",
	} {
		_, out, _ := runNoConfig(t, "help", cmd)
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Errorf("help %s must say %q", cmd, want)
		}
	}
}

// TestHelpPutDescribesConflicts checks that "help put", where put, edit, mv
// and rm send the reader, holds the output of a conflict and the default
// commit message, which the top-level help only points to.
func TestHelpPutDescribesConflicts(t *testing.T) {
	_, out, _ := runNoConfig(t, "help", "put")
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		`The commit message is -m, else "wikictl: <command> "`,
		`"<first path> and <n> more"`,
		`"wikictl: conflict (<reason>): <path> sha=<sha>"`,
		`{error, reason, path, sha, content, message}`,
		`reason is "exists"`,
		`or "changed"`,
		`reason "moved"`,
		`"wikictl: conflict (moved): <message>"`,
		`{error, reason, message, detail}`,
		`sha and content are empty when the file has been deleted`,
		`edit, mv and rm report a conflict the same way`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("help put must describe %s", want)
		}
	}
}

// TestTopHelpKeepsCommonRulesAndPoints checks that the top-level help keeps
// what every command shares and points to the help that holds the rest.
func TestTopHelpKeepsCommonRulesAndPoints(t *testing.T) {
	_, out, _ := runNoConfig(t, "help")
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		// Output stays at the top: it covers every command, not only writes.
		`{"error": "<kind>", "message": "..."}`,
		`control characters other than tab are printed as \xNN`,
		// Writes keeps the exit code and points to where the output is.
		`exits with code 3; "wikictl help put" lists the reasons and the output`,
		// A push that retrying would not fix exits with 5.
		`a hook that rejects it`,
		// Mirror keeps the path and points to where the rest is.
		`(~/.cache/wikictl)`,
		`"wikictl help context" for fetching, fetch_ttl`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("wikictl help must say %s", want)
		}
	}
}

// TestTopHelpShowsHowToReadManyFiles checks that the top-level help tells how
// to read many files with few commands, which is where readers of the help
// are sent before they read one file per command.
func TestTopHelpShowsHowToReadManyFiles(t *testing.T) {
	_, out, _ := runNoConfig(t, "help")
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		`Reading many files:`,
		// The cost of one command per file, and how to narrow first.
		`one command per file takes minutes for hundreds of them`,
		`grep -l, grep -c or find -meta KEY=VALUE`,
		// The commands that take many paths, and how to pass them.
		`cat, stat and ls -l take many paths at once`,
		`wikictl find global -name '*.md' | tr '\n' '\0' | xargs -0 -r wikictl stat`,
		// The commands that read many pages by themselves.
		`find --frontmatter=KEY,... reads the frontmatter of every file that has one`,
		`links takes several paths, a directory or none at all`,
		`pick the values out of items[] with jq`,
		// fetch_ttl for reading over time, --no-fetch for a single read.
		`set fetch_ttl in the configuration; --no-fetch skips the fetch of a single read`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("wikictl help must say %s", want)
		}
	}
	// The section follows the list of commands and comes before the rules on
	// paths and arguments, so that it is read before one file is read per command.
	list, reading, rules := strings.Index(out, "\nCommands:\n"), strings.Index(out, "\nReading many files:\n"), strings.Index(out, "Flags may come before or after the arguments.")
	if list < 0 || reading < list || rules < reading {
		t.Errorf("Reading many files must follow Commands and precede the rules on paths: %d %d %d", list, reading, rules)
	}
}

// TestHelpContextDescribesMirror checks that the details of the mirror, which
// the top-level help points to, are in "help context".
func TestHelpContextDescribesMirror(t *testing.T) {
	_, out, _ := runNoConfig(t, "help", "context")
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		`"git rev-parse --local-env-vars"`,
		`GIT_QUARANTINE_PATH`,
		`--no-fetch skips a single read regardless of fetch_ttl`,
		`put, edit, mv and rm always fetch when --no-fetch is not given`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("help context must describe %s", want)
		}
	}
}

// TestHelpLogAndCatDescribeHistory checks that the help holds what reading
// the history needs: why the whole history is read, how to see every commit,
// what --follow takes, which values --since and --until accept, the limits of
// the mirror, and that the sha of a past version is not a --base.
func TestHelpLogAndCatDescribeHistory(t *testing.T) {
	_, out, _ := runNoConfig(t, "help", "log")
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		`as git's --full-history does it`,
		`A merge commit lists no file of its own`,
		`-n 0 shows every one of them`,
		`--follow follows one file across renames and takes exactly one path`,
		`take a date (YYYY-MM-DD), read as a whole day in UTC`,
		`-S <string> keeps the commits that changed how often the string occurs`,
		`A tab inside a field is printed as \x09`,
		`A tag is neither updated nor deleted by a fetch`,
		`automatic gc (two weeks by default)`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("help log must say %s", want)
		}
	}
	_, out, _ = runNoConfig(t, "help", "cat")
	flat = strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		`With --at, every file is read as of that commit`,
		`a ref name such as HEAD or a branch name does not resolve`,
		`To read the current version, omit --at.`,
		`The sha of a past version cannot be passed to "put --base"`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("help cat must say %s", want)
		}
	}
}

// TestHelpFitsEightyColumns keeps every line of the help within 80 columns,
// so that it reads in a terminal of that width. The help is ASCII, so one
// rune is one column.
func TestHelpFitsEightyColumns(t *testing.T) {
	topics := []string{""}
	for _, c := range commands {
		topics = append(topics, c.name)
	}
	topics = append(topics, "help", "version")
	for _, topic := range topics {
		args := []string{"help"}
		if topic != "" {
			args = append(args, topic)
		}
		code, out, _ := runNoConfig(t, args...)
		if code != ExitOK {
			t.Fatalf("%v: code=%d", args, code)
		}
		for i, line := range strings.Split(out, "\n") {
			if n := utf8.RuneCountInString(line); n > 80 {
				t.Errorf("%v: line %d is %d columns: %s", args, i+1, n, line)
			}
		}
	}
}
