package repo

import (
	"strconv"
	"strings"
	"time"
)

// Commit is one commit of Log.
type Commit struct {
	SHA        string
	AuthorDate time.Time
	CommitDate time.Time
	Author     string   // the author's name, as %an gives it
	Subject    string   // the first line of the message
	Paths      []string // the paths the commit changed inside the pathspec; filled in only with Follow
}

// LogOptions limits what Log returns. Since and Until are complete times the
// caller has already validated: git's approxidate reads a bare date as that
// day at the time of the run, so a date must be normalised before it is given
// here, or the same arguments would select different commits at another hour.
type LogOptions struct {
	Paths   []string
	Max     int    // at most this many commits; 0 for all of them
	Since   string // only the commits at or after this time
	Until   string // only the commits at or before this time
	Changed string // -S: only the commits that changed how often this string occurs
	Follow  bool   // follow the single path of Paths across renames and fill in Paths
}

// logFormat is one record per commit. \x01 starts the record, so that it can
// be told from the paths that --name-only writes after it, and \x1f separates
// the fields. The subject comes last, so that a \x1f in it cannot shift
// another field.
const logFormat = "--format=%x01%H%x1f%aI%x1f%cI%x1f%an%x1f%s"

// Log returns the commits that changed the paths, or any file of the wiki
// when Paths is empty, newest first, starting from the commit that reads use.
//
// --full-history is always given. The default simplification drops a commit
// whose change a merge did not keep, which would leave the newest commit of a
// file out of the log although Updated, which reads the history the same way,
// reports its time as the last update.
func (r *Repo) Log(opt LogOptions) ([]Commit, error) {
	if r.snapshot == "" {
		return nil, nil
	}
	args := []string{"log", "-z", "--full-history", logFormat}
	if opt.Max > 0 {
		args = append(args, "-n", strconv.Itoa(opt.Max))
	}
	if opt.Since != "" {
		args = append(args, "--since="+opt.Since)
	}
	if opt.Until != "" {
		args = append(args, "--until="+opt.Until)
	}
	if opt.Changed != "" {
		args = append(args, "-S", opt.Changed)
	}
	if opt.Follow {
		// The names tell which path a commit from before a rename held, which
		// is the path to read it under. git refuses --follow for any number of
		// paths but one, so the caller checks the count first.
		args = append(args, "--follow", "--name-only")
	}
	args = append(append(args, r.snapshot), pathspec(opt.Paths)...)
	out, err := r.Git(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// parseLog reads the output of Log: each commit is a record that starts with
// \x01 and ends with NUL, followed, when --name-only was given, by a newline
// and the paths of the commit, each ending with NUL as well. Only the first
// path of a commit carries that newline. Paths are not quoted, and none is
// empty.
func parseLog(out string) []Commit {
	var res []Commit
	first := false
	for rec := range strings.SplitSeq(out, "\x00") {
		if strings.HasPrefix(rec, "\x01") {
			f := strings.SplitN(rec[1:], "\x1f", 5)
			if len(f) < 5 {
				continue
			}
			c := Commit{SHA: f[0], Author: f[3], Subject: f[4]}
			c.AuthorDate, _ = time.Parse(time.RFC3339, f[1])
			c.CommitDate, _ = time.Parse(time.RFC3339, f[2])
			res = append(res, c)
			first = true
			continue
		}
		p := rec
		if first {
			p, first = strings.TrimPrefix(rec, "\n"), false
		}
		if p == "" || len(res) == 0 {
			continue
		}
		res[len(res)-1].Paths = append(res[len(res)-1].Paths, p)
	}
	return res
}
