package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// logOut is the JSON of log. Paths is absent without --follow, so the field
// tells the two shapes apart.
type logOut struct {
	Items []struct {
		Commit     string   `json:"commit"`
		AuthorDate string   `json:"author_date"`
		CommitDate string   `json:"commit_date"`
		Author     string   `json:"author"`
		Subject    string   `json:"subject"`
		Paths      []string `json:"paths"`
	}
}

// subjects returns the subject of each item, newest first as log prints them.
func (l logOut) subjects() []string {
	var out []string
	for _, it := range l.Items {
		out = append(out, it.Subject)
	}
	return out
}

// gitAt runs git in work with the dates, the author and the committer fixed,
// so that the fixture has the same history and the same times whenever the
// tests run.
func gitAt(t *testing.T, work, date, author string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = work
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
		"GIT_AUTHOR_NAME="+author, "GIT_COMMITTER_NAME="+author,
		"GIT_AUTHOR_EMAIL=a@a", "GIT_COMMITTER_EMAIL=a@a")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeWiki writes files, keyed by path, in the clone work.
func writeWiki(t *testing.T, work string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		f := filepath.Join(work, p)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// commitAt stages everything in work and commits it with the date, the author
// and the subject given.
func commitAt(t *testing.T, work, date, author, subject string) {
	t.Helper()
	mustRun(t, work, "git", "add", "-A")
	gitAt(t, work, date, author, "commit", "-q", "-m", subject)
}

// setupLog builds a wiki whose history holds what log has to read: a merge
// whose side branch changed a page (history simplification drops it), a
// rename, commits at fixed times on either side of a day boundary, a page
// whose count of a string changes in one commit but not in the next, and a
// commit whose author and subject hold a tab.
//
// The main line is, oldest first: add pages, edit index, next day, merge side
// (merging "side edit", which changed the same page), rename page, add lease,
// touch x, tabbed.
func setupLog(t *testing.T) string {
	t.Helper()
	cfg := setupEmpty(t)
	work := cloneRemote(t, cfg)
	writeWiki(t, work, map[string]string{
		"global/index.md":   "---\nsummary: index\n---\n# index\nv1\n",
		"global/old.md":     "---\nsummary: old\n---\n# old\nalpha\n",
		"projects/app/x.md": "---\nsummary: x\n---\n# x\nlease\n",
	})
	commitAt(t, work, "2024-01-02T00:30:00Z", "agent", "add pages")
	base := gitOut(t, "-C", work, "rev-parse", "HEAD")
	line := gitOut(t, "-C", work, "rev-parse", "--abbrev-ref", "HEAD")

	writeWiki(t, work, map[string]string{"global/index.md": "---\nsummary: index\n---\n# index\nv2\n"})
	commitAt(t, work, "2024-01-02T23:00:00Z", "agent", "edit index")
	writeWiki(t, work, map[string]string{"global/index.md": "---\nsummary: index\n---\n# index\nv3\n"})
	commitAt(t, work, "2024-01-03T00:00:00Z", "agent", "next day")

	// The side branch changes the page the main line changed too, and the
	// merge keeps the main line's version, so that the commit of the side
	// branch and the merge itself are both TREESAME to a parent and are left
	// out unless --full-history is given.
	mustRun(t, work, "git", "checkout", "-q", "-b", "side", base)
	writeWiki(t, work, map[string]string{"global/index.md": "---\nsummary: index\n---\n# index\nside\n"})
	commitAt(t, work, "2024-01-04T00:00:00Z", "agent", "side edit")
	mustRun(t, work, "git", "checkout", "-q", line)
	gitAt(t, work, "2024-01-05T00:00:00Z", "agent", "merge", "-q", "--no-ff", "-X", "ours", "-m", "merge side", "side")

	gitAt(t, work, "2024-01-06T00:00:00Z", "agent", "mv", "global/old.md", "global/new.md")
	gitAt(t, work, "2024-01-06T00:00:00Z", "agent", "commit", "-q", "-m", "rename page")

	writeWiki(t, work, map[string]string{"projects/app/x.md": "---\nsummary: x\n---\n# x\nlease\nlease\n"})
	commitAt(t, work, "2024-01-08T00:00:00Z", "agent", "add lease")
	writeWiki(t, work, map[string]string{"projects/app/x.md": "---\nsummary: x\n---\n# x\nlease\nlease\ntail\n"})
	commitAt(t, work, "2024-01-09T00:00:00Z", "agent", "touch x")

	writeWiki(t, work, map[string]string{"global/tabs.md": "---\nsummary: tabs\n---\n# tabs\n"})
	commitAt(t, work, "2024-01-10T00:00:00Z", "a\tb", "sub\tject")

	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	return cfg
}

// TestLogFullHistory checks that log always passes --full-history, so that a
// commit history simplification drops is still reported, and that the newest
// commit log prints for a page is the one whose time stat shows as updated.
func TestLogFullHistory(t *testing.T) {
	cfg := setupLog(t)
	var got logOut
	code, out, errs := runCLI(t, cfg, "", "log", "--json", "-n", "0", "global/index.md")
	if code != ExitOK {
		t.Fatalf("log: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &got)
	want := []string{"merge side", "side edit", "next day", "edit index", "add pages"}
	if have := got.subjects(); !equalStrings(have, want) {
		t.Errorf("log subjects %q, want %q", have, want)
	}

	// The same history simplified: the commit of the side branch and the merge
	// are missing, which is what --full-history keeps.
	simplified := gitOut(t, "-C", cloneRemote(t, cfg), "log", "--format=%s", "--", "global/index.md")
	for _, s := range []string{"side edit", "merge side"} {
		if strings.Contains(simplified, s) {
			t.Fatalf("the simplified history still holds %q, so the fixture does not show the effect of --full-history:\n%s", s, simplified)
		}
	}

	if want := "2024-01-05T00:00:00Z"; got.Items[0].CommitDate != want {
		t.Errorf("newest commit_date %q, want %q", got.Items[0].CommitDate, want)
	}
	if got.Items[0].Author != "agent" {
		t.Errorf("author %q, want the name only", got.Items[0].Author)
	}
	// paths is returned only with --follow.
	if strings.Contains(out, `"paths"`) {
		t.Errorf("log without --follow must not return paths: %s", out)
	}

	// stat reads its update time from the same history, so the newest commit
	// log prints for a file is the commit that time comes from.
	var st struct {
		Items []struct{ Updated string }
	}
	var x logOut
	_, out, _ = runCLI(t, cfg, "", "log", "--json", "-n", "1", "projects/app/x.md")
	mustUnmarshal(t, out, &x)
	_, out, _ = runCLI(t, cfg, "", "stat", "--json", "projects/app/x.md")
	mustUnmarshal(t, out, &st)
	if len(st.Items) != 1 || len(x.Items) != 1 || st.Items[0].Updated != x.Items[0].CommitDate {
		t.Errorf("stat updated %+v, want the commit_date of the newest commit %+v", st.Items, x.Items)
	}

	// A merge is the one commit the two differ on: stat's update time comes
	// from the newest commit that lists the file, and a merge lists no file,
	// so the merge is the newest commit of log while stat shows the one before.
	_, out, _ = runCLI(t, cfg, "", "stat", "--json", "global/index.md")
	mustUnmarshal(t, out, &st)
	if st.Items[0].Updated != "2024-01-04T00:00:00Z" {
		t.Errorf("stat updated of a file whose newest commit is a merge: %q", st.Items[0].Updated)
	}
}

// TestLogLimit checks the default of -n and that -n 0 is no limit.
func TestLogLimit(t *testing.T) {
	cfg := setupEmpty(t)
	work := cloneRemote(t, cfg)
	for i := range 25 {
		writeWiki(t, work, map[string]string{"global/a.md": "---\nsummary: a\n---\n# a\n" + strings.Repeat("x\n", i+1)})
		commitAt(t, work, "2024-02-01T00:00:00Z", "agent", "commit "+string(rune('a'+i)))
	}
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")

	for _, c := range []struct {
		args []string
		want int
	}{
		{[]string{"log", "--json"}, 20},
		{[]string{"log", "--json", "global/a.md"}, 20},
		{[]string{"log", "--json", "-n", "0"}, 25},
		{[]string{"log", "--json", "-n", "0", "global/a.md"}, 25},
		{[]string{"log", "--json", "-n", "3"}, 3},
		{[]string{"log", "--json", "--max-count", "1"}, 1},
	} {
		var got logOut
		code, out, errs := runCLI(t, cfg, "", c.args...)
		if code != ExitOK {
			t.Fatalf("%v: code=%d errs=%q", c.args, code, errs)
		}
		mustUnmarshal(t, out, &got)
		if len(got.Items) != c.want {
			t.Errorf("%v: %d commits, want %d", c.args, len(got.Items), c.want)
		}
	}
	// A negative -n is a usage error, not a limit git would reject.
	if code, _, errs := runCLI(t, cfg, "", "log", "-n", "-1"); code != ExitUsage || !strings.Contains(errs, "Usage: wikictl log") {
		t.Errorf("log -n -1: code=%d errs=%q", code, errs)
	}
}

// TestLogFollow checks that --follow takes exactly one path, reports the
// other counts as usage errors before the configuration is read, and returns
// the path each commit held, so that cat --at can be given the right one.
func TestLogFollow(t *testing.T) {
	// The count of paths is checked before the configuration is read.
	for _, args := range [][]string{
		{"log", "--follow"},
		{"log", "--follow", "global/new.md", "projects/app/x.md"},
	} {
		code, _, errs := runNoConfig(t, args...)
		if code != ExitUsage || !strings.Contains(errs, "exactly one path") || !strings.Contains(errs, "Usage: wikictl log") {
			t.Errorf("%v: code=%d errs=%q", args, code, errs)
		}
	}

	cfg := setupLog(t)
	var got logOut
	code, out, errs := runCLI(t, cfg, "", "log", "--json", "-n", "0", "--follow", "global/new.md")
	if code != ExitOK {
		t.Fatalf("log --follow: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &got)
	want := []string{"rename page", "add pages"}
	if have := got.subjects(); !equalStrings(have, want) {
		t.Fatalf("log --follow subjects %q, want %q", have, want)
	}
	// The commit before the rename held the old name, which is the path to
	// pass to "cat --at" for that version.
	if !equalStrings(got.Items[0].Paths, []string{"global/new.md"}) || !equalStrings(got.Items[1].Paths, []string{"global/old.md"}) {
		t.Errorf("paths %q and %q, want [global/new.md] and [global/old.md]", got.Items[0].Paths, got.Items[1].Paths)
	}
	// The path of a commit is limited to the one asked for, although the
	// oldest commit added three files.
	for _, it := range got.Items {
		if len(it.Paths) != 1 {
			t.Errorf("commit %q has paths %q, want only the path given", it.Subject, it.Paths)
		}
	}
}

// TestLogTimeRange checks that --since and --until accept only a date and
// RFC 3339, and that a bare date is normalised to a whole UTC day, so that
// the result does not depend on the time the command runs.
func TestLogTimeRange(t *testing.T) {
	for _, s := range []string{"bogus", "2 weeks ago", "yesterday", "2024-1-2", "2024-01-02 00:00:00",
		"2024-01-02T00:00:00", "2024-13-01", "20240102", ""} {
		for _, flag := range []string{"--since", "--until"} {
			code, _, errs := runNoConfig(t, "log", flag, s)
			if code != ExitUsage || !strings.Contains(errs, "Usage: wikictl log") {
				t.Errorf("log %s %q: code=%d errs=%q", flag, s, code, errs)
			}
		}
	}

	cfg := setupLog(t)
	for _, c := range []struct {
		args []string
		want []string
	}{
		// The day of "add pages" (00:30) and "edit index" (23:00): both are in
		// the range only because a bare date becomes the whole UTC day.
		{[]string{"--since", "2024-01-02", "--until", "2024-01-02"}, []string{"edit index", "add pages"}},
		// --until keeps the last second of the day and leaves out the commit
		// at 00:00:00 of the next day.
		{[]string{"--until", "2024-01-02"}, []string{"edit index", "add pages"}},
		{[]string{"--until", "2024-01-03"}, []string{"next day", "edit index", "add pages"}},
		{[]string{"--since", "2024-01-03"}, []string{"merge side", "side edit", "next day"}},
		// RFC 3339 with an offset is passed through.
		{[]string{"--since", "2024-01-02T00:00:00Z", "--until", "2024-01-02T12:00:00Z"}, []string{"add pages"}},
		{[]string{"--since", "2024-01-02T10:00:00+09:00", "--until", "2024-01-03T00:00:00Z"}, []string{"next day", "edit index"}},
	} {
		var got logOut
		args := append(append([]string{"log", "--json", "-n", "0"}, c.args...), "global/index.md")
		code, out, errs := runCLI(t, cfg, "", args...)
		if code != ExitOK {
			t.Fatalf("%v: code=%d errs=%q", args, code, errs)
		}
		mustUnmarshal(t, out, &got)
		if have := got.subjects(); !equalStrings(have, c.want) {
			t.Errorf("%v: subjects %q, want %q", c.args, have, c.want)
		}
	}
}

// TestLogTimeNormalisation checks the values given to git for a bare date,
// which is what keeps the range from depending on the time of the run: git's
// approxidate reads a bare date as that day at the current time of day.
func TestLogTimeNormalisation(t *testing.T) {
	for _, c := range []struct {
		in         string
		end        bool
		want       string
		wantErr    bool
		wantErrMsg string
	}{
		{in: "2024-01-02", want: "2024-01-02T00:00:00Z"},
		{in: "2024-01-02", end: true, want: "2024-01-02T23:59:59Z"},
		{in: "2024-01-02T03:04:05Z", want: "2024-01-02T03:04:05Z"},
		{in: "2024-01-02T03:04:05Z", end: true, want: "2024-01-02T03:04:05Z"},
		{in: "2024-01-02T03:04:05+09:00", want: "2024-01-02T03:04:05+09:00"},
		{in: "2024-1-2", wantErr: true},
		{in: "yesterday", wantErr: true},
		{in: "", wantErr: true},
	} {
		got, err := logTime(c.in, c.end)
		if c.wantErr {
			if err == nil {
				t.Errorf("logTime(%q, %v) = %q, want an error", c.in, c.end, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("logTime(%q, %v) = %q, %v, want %q", c.in, c.end, got, err, c.want)
		}
	}
}

// TestLogChanged checks that -S keeps the commits that changed the number of
// times the string occurs and leaves out a commit that changed the file
// without changing that number.
func TestLogChanged(t *testing.T) {
	cfg := setupLog(t)
	var got logOut
	code, out, errs := runCLI(t, cfg, "", "log", "--json", "-n", "0", "-S", "lease", "projects/app/x.md")
	if code != ExitOK {
		t.Fatalf("log -S: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &got)
	want := []string{"add lease", "add pages"}
	if have := got.subjects(); !equalStrings(have, want) {
		t.Errorf("log -S lease subjects %q, want %q ("+`"touch x" changed the file without changing the count`+")", have, want)
	}
	// The long name is the same flag.
	_, out, _ = runCLI(t, cfg, "", "log", "--json", "-n", "0", "--changed", "lease", "projects/app/x.md")
	mustUnmarshal(t, out, &got)
	if have := got.subjects(); !equalStrings(have, want) {
		t.Errorf("log --changed lease subjects %q, want %q", have, want)
	}
	// An empty string is a usage error: git aborts on one.
	for _, args := range [][]string{{"log", "-S", ""}, {"log", "--changed", ""}} {
		if code, _, errs := runNoConfig(t, args...); code != ExitUsage || !strings.Contains(errs, "Usage: wikictl log") {
			t.Errorf("%v: code=%d errs=%q", args, code, errs)
		}
	}
}

// TestLogPathsAndDirectories checks that log checks the paths as the other
// reads do: a path that does not exist is reported with code 1 while the
// other paths are still read, and a directory stands for the files under it.
func TestLogPathsAndDirectories(t *testing.T) {
	cfg := setupLog(t)
	var got logOut
	code, out, errs := runCLI(t, cfg, "", "log", "--json", "-n", "0", "projects")
	if code != ExitOK {
		t.Fatalf("log of a directory: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &got)
	if want := []string{"touch x", "add lease", "add pages"}; !equalStrings(got.subjects(), want) {
		t.Errorf("log projects: subjects %q, want %q", got.subjects(), want)
	}

	// A path that does not exist is reported, and the commits of the path that
	// does exist are still printed.
	code, out, errs = runCLI(t, cfg, "", "log", "--json", "-n", "0", "global/gone.md", "projects/app/x.md")
	if code != ExitError || errs != "wikictl: global/gone.md: no such file or directory\n" {
		t.Errorf("log of a missing path: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &got)
	if want := []string{"touch x", "add lease", "add pages"}; !equalStrings(got.subjects(), want) {
		t.Errorf("log with a missing path: subjects %q, want %q", got.subjects(), want)
	}
	// Only missing paths: nothing is logged, so the whole wiki is not read.
	code, out, _ = runCLI(t, cfg, "", "log", "--json", "global/gone.md")
	if code != ExitError || out != `{"items":[]}`+"\n" {
		t.Errorf("log of only a missing path: code=%d out=%q", code, out)
	}
	// The whole wiki without a path.
	code, out, errs = runCLI(t, cfg, "", "log", "--json", "-n", "0")
	if code != ExitOK {
		t.Fatalf("log: code=%d errs=%q", code, errs)
	}
	mustUnmarshal(t, out, &got)
	if len(got.Items) != 9 {
		t.Errorf("log of the wiki: %d commits, want 9: %q", len(got.Items), got.subjects())
	}
}

// TestLogTextColumns checks that a tab in the author or the subject is
// escaped, so that the columns of the text output stay where they are.
func TestLogTextColumns(t *testing.T) {
	cfg := setupLog(t)
	code, out, errs := runCLI(t, cfg, "", "log", "global/tabs.md")
	if code != ExitOK {
		t.Fatalf("log: code=%d errs=%q", code, errs)
	}
	line := strings.TrimSuffix(out, "\n")
	f := strings.Split(line, "\t")
	if len(f) != 5 {
		t.Fatalf("log printed %d fields, want 5: %q", len(f), line)
	}
	if f[1] != "2024-01-10T00:00:00Z" || f[2] != "2024-01-10T00:00:00Z" {
		t.Errorf("dates %q and %q", f[1], f[2])
	}
	if f[3] != `a\x09b` || f[4] != `sub\x09ject` {
		t.Errorf("author %q and subject %q, want the tabs escaped", f[3], f[4])
	}
	// With --follow the paths follow the five fields.
	_, out, _ = runCLI(t, cfg, "", "log", "--follow", "global/tabs.md")
	f = strings.Split(strings.TrimSuffix(out, "\n"), "\t")
	if len(f) != 6 || f[5] != "global/tabs.md" {
		t.Errorf("log --follow printed %q, want the path as the sixth field", f)
	}
}

// TestCatAt checks that cat --at reads a past version, that only a commit the
// mirror holds is accepted, and that a ref name is refused with the way on.
func TestCatAt(t *testing.T) {
	cfg := setupLog(t)
	var got logOut
	_, out, _ := runCLI(t, cfg, "", "log", "--json", "-n", "0", "global/index.md")
	mustUnmarshal(t, out, &got)
	first := got.Items[len(got.Items)-1].Commit // "add pages"

	// The sha log printed reads the version of that commit.
	code, out, errs := runCLI(t, cfg, "", "cat", "--at", first, "global/index.md")
	if code != ExitOK || out != "---\nsummary: index\n---\n# index\nv1\n" {
		t.Errorf("cat --at: code=%d out=%q errs=%q", code, out, errs)
	}
	// An abbreviated sha resolves too.
	if code, out, _ := runCLI(t, cfg, "", "cat", "--at", first[:8], "global/index.md"); code != ExitOK || !strings.Contains(out, "v1") {
		t.Errorf("cat --at with an abbreviated sha: code=%d out=%q", code, out)
	}
	// Without --at the current version is read.
	if code, out, _ := runCLI(t, cfg, "", "cat", "global/index.md"); code != ExitOK || !strings.Contains(out, "v3") {
		t.Errorf("cat: code=%d out=%q", code, out)
	}
	// A tag resolves, since the mirror keeps tags.
	work := cloneRemote(t, cfg)
	gitAt(t, work, "2024-01-11T00:00:00Z", "agent", "tag", "v1", first)
	mustRun(t, work, "git", "push", "-q", "origin", "v1")
	if code, out, errs := runCLI(t, cfg, "", "cat", "--at", "v1", "global/index.md"); code != ExitOK || !strings.Contains(out, "v1\n") {
		t.Errorf("cat --at a tag: code=%d out=%q errs=%q", code, out, errs)
	}

	// A sha of an object that is not a commit, and a commit the mirror does
	// not have, are usage errors: "rev-parse --verify" alone accepts both.
	remote := filepath.Join(filepath.Dir(cfg), "remote.git")
	for _, rev := range []string{
		"0123456789012345678901234567890123456789",
		gitOut(t, "--git-dir", remote, "rev-parse", "main^{tree}"),
		gitOut(t, "--git-dir", remote, "rev-parse", "main:global/index.md"),
		"nosuchtag",
	} {
		code, out, errs := runCLI(t, cfg, "", "cat", "--at", rev, "global/index.md")
		if code != ExitUsage || out != "" || !strings.Contains(errs, "--at") {
			t.Errorf("cat --at %s: code=%d out=%q errs=%q", rev, code, out, errs)
		}
	}
	// A ref name the mirror does not keep is refused, and the message says
	// that the current version needs no --at at all.
	for _, rev := range []string{"HEAD", "main"} {
		code, _, errs := runCLI(t, cfg, "", "cat", "--at", rev, "global/index.md")
		if code != ExitUsage || !strings.Contains(errs, "omit --at") {
			t.Errorf("cat --at %s: code=%d errs=%q, want the way on to the current version", rev, code, errs)
		}
	}

	// The version decides what a path is, so a file that does not exist yet at
	// that commit is reported as missing, with code 1.
	code, _, errs = runCLI(t, cfg, "", "cat", "--at", first, "global/tabs.md")
	if code != ExitError || errs != "wikictl: global/tabs.md: no such file or directory\n" {
		t.Errorf("cat --at of a file added later: code=%d errs=%q", code, errs)
	}
	// The name before a rename reads at the older commit and not at the newer.
	if code, out, _ := runCLI(t, cfg, "", "cat", "--at", first, "global/old.md"); code != ExitOK || !strings.Contains(out, "alpha") {
		t.Errorf("cat --at of the old name: code=%d out=%q", code, out)
	}
	if code, _, errs := runCLI(t, cfg, "", "cat", "global/old.md"); code != ExitError || errs != "wikictl: global/old.md: no such file or directory\n" {
		t.Errorf("cat of the old name: code=%d errs=%q", code, errs)
	}
	// The sha of the past version is the blob of that version, not the current
	// one; "help cat" says it is not for "put --base".
	var cat struct {
		Items []struct{ Path, SHA, Content string }
	}
	_, out, _ = runCLI(t, cfg, "", "cat", "--json", "--at", first, "global/index.md")
	mustUnmarshal(t, out, &cat)
	_, cur, _ := runCLI(t, cfg, "", "cat", "--json", "global/index.md")
	var now struct {
		Items []struct{ SHA string }
	}
	mustUnmarshal(t, cur, &now)
	if len(cat.Items) != 1 || len(now.Items) != 1 || cat.Items[0].SHA == now.Items[0].SHA {
		t.Errorf("the sha of a past version must differ from the current one: %s %s", out, cur)
	}
}

// TestLogAndCatAtSubmodule checks that both report a submodule as the other
// reads do, instead of log answering for a path they call unreadable.
func TestLogAndCatAtSubmodule(t *testing.T) {
	cfg := setupLog(t)
	remote, work := filepath.Join(filepath.Dir(cfg), "remote.git"), cloneRemote(t, cfg)
	head := gitOut(t, "--git-dir", remote, "rev-parse", "main")
	mustRun(t, work, "git", "update-index", "--add", "--cacheinfo", "160000,"+head+",projects/subm")
	// The gitlink is only in the index, so "git add -A" would drop it again.
	gitAt(t, work, "2024-01-12T00:00:00Z", "agent", "commit", "-q", "-m", "add submodule")
	mustRun(t, work, "git", "push", "-q", "origin", "HEAD:main")
	withSub := gitOut(t, "--git-dir", remote, "rev-parse", "main")

	want := "wikictl: projects/subm: is a submodule\n"
	for _, args := range [][]string{
		{"log", "--json", "projects/subm"},
		{"log", "--json", "-n", "0", "--follow", "projects/subm"},
	} {
		if code, out, errs := runCLI(t, cfg, "", args...); code != ExitError || out != `{"items":[]}`+"\n" || errs != want {
			t.Errorf("%v: code=%d out=%q errs=%q", args, code, out, errs)
		}
	}
	if code, out, errs := runCLI(t, cfg, "", "cat", "--at", withSub, "projects/subm"); code != ExitError || out != "" || errs != want {
		t.Errorf("cat --at of a submodule: code=%d out=%q errs=%q", code, out, errs)
	}
	// The version --at names decides the report as well as the content: at the
	// commit before, the same path is not a submodule but absent.
	if code, _, errs := runCLI(t, cfg, "", "cat", "--at", head, "projects/subm"); code != ExitError ||
		errs != "wikictl: projects/subm: no such file or directory\n" {
		t.Errorf("cat --at before the submodule was added: code=%d errs=%q", code, errs)
	}
}

// equalStrings compares two lists, treating a nil list as an empty one, so
// that a command that returned nothing can be compared with an empty want.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
