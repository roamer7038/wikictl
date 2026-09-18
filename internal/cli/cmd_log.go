package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/repo"
)

// logItem is one commit of log. Paths is filled in only with --follow, where
// a commit from before a rename held another path than the one asked for. It
// is a pointer so that the key is absent without --follow while every item of
// one run has the same shape, a commit that changed no path included.
type logItem struct {
	Commit     string
	AuthorDate string
	CommitDate string
	Author     string
	Subject    string
	Paths      *[]string
}

// MarshalJSON writes every key of the item: an author or a subject that is
// not valid UTF-8 goes to author_base64 or subject_base64, and paths[] is
// joined by a paths_base64[] of the same length and order when a path is not
// valid UTF-8; see jsonout.go. The commit sha and the two dates are written
// by git and hold ASCII only.
func (it logItem) MarshalJSON() ([]byte, error) {
	o := jsonObject(nil).add("commit", it.Commit).add("author_date", it.AuthorDate).
		add("commit_date", it.CommitDate).text("author", it.Author).text("subject", it.Subject)
	if it.Paths != nil {
		o = o.list("paths", *it.Paths)
	}
	return o.MarshalJSON()
}

// optString is a string flag that records whether it was given, so that a
// value that is empty is reported instead of read as the flag being absent.
// git aborts on an empty -S, and an empty --since would select every commit
// rather than say that the value is no time.
type optString struct {
	set   bool
	value string
}

func (s *optString) String() string { return s.value }

func (s *optString) Type() string { return "string" }

func (s *optString) Set(v string) error {
	s.set, s.value = true, v
	return nil
}

func logFlags(a *app, fs *pflag.FlagSet) {
	fs.IntVarP(&a.logMax, "max-count", "n", 20, "show at most `N` commits, 0 for all")
	fs.Var(&a.logSince, "since", "show the commits at or after `time`")
	fs.Var(&a.logUntil, "until", "show the commits at or before `time`")
	fs.VarP(&a.logChanged, "changed", "S", "show the commits where the count of `string` changed")
	fs.BoolVar(&a.logFollow, "follow", false, "follow the one path given across renames")
}

// checkLog validates the arguments before the configuration is read, so that
// a usage error never depends on the environment. git refuses --follow for
// any number of paths but one, and reads a bare --since or --until as that
// day at the time of the run, so both are normalised here instead.
func (a *app) checkLog(c *command, args []string) error {
	if a.logMax < 0 {
		return &usageError{c, "-n must not be negative; -n 0 shows every commit"}
	}
	// git fails with "--follow requires exactly one pathspec" for no path as
	// well as for several, and log is a command whose paths may be left out,
	// so "wikictl log --follow" is the error most easily made.
	if a.logFollow && len(args) != 1 {
		return &usageError{c, "--follow takes exactly one path, since that is all git allows"}
	}
	if a.logChanged.set && a.logChanged.value == "" {
		return &usageError{c, "-S needs a string to look for"}
	}
	for _, f := range []struct {
		name string
		v    *optString
		end  bool
	}{{"--since", &a.logSince, false}, {"--until", &a.logUntil, true}} {
		if !f.v.set {
			continue
		}
		norm, err := logTime(f.v.value, f.end)
		if err != nil {
			return &usageError{c, f.name + " " + err.Error()}
		}
		f.v.value = norm
	}
	return nil
}

// logTime validates a --since or --until value and returns the complete time
// to give git. git's approxidate reads a bare date as that day at the time of
// the run, so "--since 2024-01-02" would leave out the commits of that
// morning when it runs in the evening; a date is therefore read as a whole
// day in UTC, as fmtTime prints times in UTC. The end of the day is its last
// second rather than midnight of the next day, because git's --since and
// --until both include their bound.
func logTime(s string, end bool) (string, error) {
	if _, err := time.Parse(time.DateOnly, s); err == nil {
		if end {
			return s + "T23:59:59Z", nil
		}
		return s + "T00:00:00Z", nil
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return s, nil
	}
	return "", fmt.Errorf("must be a date such as 2024-01-02 or an RFC 3339 time with an offset such as 2024-01-02T15:04:05Z, not %q", s)
}

// cmdLog shows the commits that changed the files under the paths, newest
// first. git tells a path with no history from one that does not exist in no
// way, both being no commit and exit code 0, so the paths are checked against
// the wiki as the other reads check them: one that does not exist, or that is
// a submodule, is reported and the command exits with 1.
func (a *app) cmdLog(c *command, args []string) error {
	t, err := a.readTree(false)
	if err != nil {
		return err
	}
	var paths, missing []string
	for _, p := range args {
		if !t.subs[p] && (p == "." || t.isDir(p) || t.isFile(p)) {
			paths = append(paths, p)
		} else {
			missing = append(missing, p)
		}
	}
	items := []logItem{}
	// With no path left of those given, the log of the whole wiki is not what
	// was asked for, so nothing is read.
	if len(args) == 0 || len(paths) > 0 {
		commits, err := a.repo.Log(repo.LogOptions{Paths: paths, Max: a.logMax, Since: a.logSince.value,
			Until: a.logUntil.value, Changed: a.logChanged.value, Follow: a.logFollow})
		if err != nil {
			return &gitError{err}
		}
		for _, cm := range commits {
			it := logItem{Commit: cm.SHA, AuthorDate: fmtTime(cm.AuthorDate), CommitDate: fmtTime(cm.CommitDate),
				Author: cm.Author, Subject: cm.Subject}
			if a.logFollow {
				ps := cm.Paths
				if ps == nil {
					ps = []string{}
				}
				it.Paths = &ps
			}
			items = append(items, it)
		}
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			fields := []string{it.Commit, it.AuthorDate, it.CommitDate, escapeField(it.Author), escapeField(it.Subject)}
			if it.Paths != nil {
				for _, p := range *it.Paths {
					fields = append(fields, escapeField(p))
				}
			}
			fmt.Fprintln(w, strings.Join(fields, "\t"))
		}
	})
	return a.reportMissing(missing, t.pathMessage)
}
