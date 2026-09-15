package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("WIKICTL_PROFILE", "")
	d := t.TempDir()
	p := filepath.Join(d, "c.yaml")
	os.WriteFile(p, []byte("repo: https://h/r.git\nauthor: {name: n, email: e}\n"), 0o600)
	c, err := Load(p, Selector{})
	if err != nil || c.Repo != "https://h/r.git" || c.Author.Name != "n" || c.Path != p || c.ProfileSource != SourceNone {
		t.Fatalf("%+v %v", c, err)
	}
	t.Setenv("WIKICTL_CONFIG", p)
	if c2, err := Load("", Selector{}); err != nil || c2.Repo != c.Repo {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(d, "none.yaml"), Selector{}); err == nil {
		t.Error("missing file must error")
	}
	os.WriteFile(p, []byte("author: {name: n}\n"), 0o600)
	if _, err := Load(p, Selector{}); err == nil {
		t.Error("missing repo must error")
	}
}

const profilesYAML = `repo: git@github.com:me/wiki.git
branch: trunk
author: {name: agent, email: me@home}
default_profile: personal
profiles:
  personal: {}
  work:
    repo: https://git.example.com/team/wiki.git
    author: {email: me@work}
    match:
      remotes: ["git.example.com/team/*"]
      paths: ["%s"]
  empty:
`

func writeProfiles(t *testing.T, workDir string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(p, []byte(strings.Replace(profilesYAML, "%s", workDir, 1)), 0o600)
	return p
}

func TestProfileSelection(t *testing.T) {
	t.Setenv("WIKICTL_PROFILE", "")
	work := t.TempDir()
	p := writeProfiles(t, work)

	c, err := Load(p, Selector{Dir: t.TempDir()})
	if err != nil || c.Profile != "personal" || c.ProfileSource != SourceDefault || c.Repo != "git@github.com:me/wiki.git" || c.Branch != "trunk" {
		t.Fatalf("default: %+v %v", c, err)
	}

	sub := filepath.Join(work, "sub")
	os.Mkdir(sub, 0o755)
	c, err = Load(p, Selector{Dir: sub})
	if err != nil || c.Profile != "work" || c.ProfileSource != SourceMatch {
		t.Fatalf("paths: %+v %v", c, err)
	}
	// Merge: repo replaces and drops the inherited branch, author merges per
	// field.
	if c.Repo != "https://git.example.com/team/wiki.git" || c.Branch != "" || c.Author.Name != "agent" || c.Author.Email != "me@work" {
		t.Errorf("merge: %+v", c)
	}

	c, err = Load(p, Selector{Remote: "git@GIT.example.com:team/app.git"})
	if err != nil || c.Profile != "work" || c.ProfileSource != SourceMatch {
		t.Errorf("remotes: %+v %v", c, err)
	}

	t.Setenv("WIKICTL_PROFILE", "personal")
	c, err = Load(p, Selector{Dir: work})
	if err != nil || c.Profile != "personal" || c.ProfileSource != SourceEnv {
		t.Errorf("env beats match: %+v %v", c, err)
	}
	c, err = Load(p, Selector{Profile: "work", Dir: t.TempDir()})
	if err != nil || c.Profile != "work" || c.ProfileSource != SourceFlag {
		t.Errorf("flag beats env: %+v %v", c, err)
	}
	t.Setenv("WIKICTL_PROFILE", "")

	c, err = Load(p, Selector{Profile: "empty"})
	if err != nil || c.Profile != "empty" || c.Repo != "git@github.com:me/wiki.git" {
		t.Errorf("null profile inherits everything: %+v %v", c, err)
	}
	if _, err := Load(p, Selector{Profile: "nope"}); err == nil || !strings.Contains(err.Error(), "--profile") {
		t.Errorf("unknown profile: %v", err)
	}
}

func TestProfileErrors(t *testing.T) {
	t.Setenv("WIKICTL_PROFILE", "")
	d := t.TempDir()
	p := filepath.Join(d, "c.yaml")

	os.WriteFile(p, []byte("profiles:\n  a: {repo: r1, match: {paths: ["+d+"]}}\n  b: {repo: r2, match: {paths: ["+d+"]}}\n"), 0o600)
	if _, err := Load(p, Selector{Dir: d}); err == nil || !strings.Contains(err.Error(), "a, b") {
		t.Errorf("several matches must error: %v", err)
	}
	if c, err := Load(p, Selector{Dir: d, Profile: "b"}); err != nil || c.Repo != filepath.Join(d, "r2") {
		t.Errorf("--profile resolves several matches: %+v %v", c, err)
	}
	if _, err := Load(p, Selector{Dir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "no profile is selected") {
		t.Errorf("no selection without top-level repo must error: %v", err)
	}

	os.WriteFile(p, []byte("repo: r\ndefault_profile: x\n"), 0o600)
	if _, err := Load(p, Selector{}); err == nil || !strings.Contains(err.Error(), "default_profile") {
		t.Errorf("undefined default_profile: %v", err)
	}

	os.WriteFile(p, []byte("repo: r\nprofiles:\n  a: {match: {remotes: [\"[\"]}}\n"), 0o600)
	if _, err := Load(p, Selector{}); err == nil {
		t.Error("bad pattern must error")
	}

	for yml, keys := range map[string][]string{
		"repo: r\nprofiles:\n  a: {match: {remote: [\"h/*\"]}}\n": {"profiles.a.match.remote"},
		"repo: r\nprofile:\n  a: {repo: r2}\n":                    {"profile"},
		"repo: r\ndefault_profle: a\n":                            {"default_profle"},
		"repo: r\nauthor: {nmae: n}\n":                            {"author.nmae"},
		"repo: r\nmachine: m\nprojects: {a: b}\n":                 {"machine", "projects"},
		"repo: r\n\"1\": a\n":                                     {"1"},
	} {
		os.WriteFile(p, []byte(yml), 0o600)
		c, err := Load(p, Selector{})
		if err != nil || len(c.Warnings) != len(keys) {
			t.Errorf("unknown keys %v must be warnings: %+v %v", keys, c, err)
			continue
		}
		for i, k := range keys {
			if want := "config file " + p + ": unknown key \"" + k + "\" is ignored"; c.Warnings[i] != want {
				t.Errorf("warning %d: %q, want %q", i, c.Warnings[i], want)
			}
		}
	}
	os.WriteFile(p, []byte("repo: [r]\n"), 0o600)
	if _, err := Load(p, Selector{}); err == nil {
		t.Error("a value of the wrong type must error")
	}
	os.WriteFile(p, []byte("repo: r\nprofiles:\n  a: {match: {remotes: r}}\n"), 0o600)
	if _, err := Load(p, Selector{}); err == nil {
		t.Error("a profile value of the wrong type must error")
	}
	for yml, key := range map[string]string{
		"repo: r\n1: a\n":                             "1",
		"repo: r\ntrue: a\n":                          "true",
		"repo: r\nprofiles:\n  w: {repo: r2, 1: a}\n": "profiles.w.1",
	} {
		os.WriteFile(p, []byte(yml), 0o600)
		want := "config file " + p + ": key \"" + key + "\" is not a string"
		if _, err := Load(p, Selector{Profile: "w"}); err == nil || err.Error() != want {
			t.Errorf("a key that is not a string must error: %v, want %s", err, want)
		}
	}
	// A misspelled key is reported with the error that it causes.
	os.WriteFile(p, []byte("reop: r\n"), 0o600)
	if c, err := Load(p, Selector{}); err == nil || c == nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], `unknown key "reop"`) {
		t.Errorf("warnings must come with the error: %+v %v", c, err)
	}

	for _, rel := range []string{".", "work"} {
		os.WriteFile(p, []byte("repo: r\nprofiles:\n  a: {match: {paths: [\""+rel+"\"]}}\n"), 0o600)
		if _, err := Load(p, Selector{}); err == nil || !strings.Contains(err.Error(), "match.paths") {
			t.Errorf("relative path %q must error: %v", rel, err)
		}
	}
}

func TestMatchPaths(t *testing.T) {
	d := t.TempDir()
	real := filepath.Join(d, "real")
	os.MkdirAll(filepath.Join(real, "sub"), 0o755)
	link := filepath.Join(d, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	m := Match{Paths: []string{link}}
	for dir, want := range map[string]bool{
		real:                        true,
		filepath.Join(real, "sub"):  true,
		filepath.Join(link, "sub"):  true,
		real + "x":                  false,
		d:                           false,
		filepath.Join(d, "missing"): false,
	} {
		if ok, _ := m.accepts("", resolvePath(dir)); ok != want {
			t.Errorf("%s: %v", dir, ok)
		}
	}
	t.Setenv("HOME", d)
	if ok, _ := (Match{Paths: []string{"~/real"}}).accepts("", resolvePath(filepath.Join(real, "sub"))); !ok {
		t.Error("~ must expand")
	}
}

func TestMatchRemotes(t *testing.T) {
	for pat, cases := range map[string]map[string]bool{
		"h/team/*": {
			"h/team/app":     true,
			"h/team/sub/app": true,
			"h/team":         false,
			"h/teamx/app":    false,
			"":               false,
		},
		"h/*/app": {
			"h/team/app":     true,
			"h/team/sub/app": false,
		},
		"h/team/app": {
			"h/team/app":   true,
			"h/team/app/x": false,
		},
		"h/t*/*": {
			"h/team/sub/app": true,
			"h/x/team/app":   false,
		},
	} {
		for remote, want := range cases {
			if ok, err := (Match{Remotes: []string{pat}}).accepts(remote, ""); err != nil || ok != want {
				t.Errorf("%s ~ %s: %v %v", pat, remote, ok, err)
			}
		}
	}
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:Org/App.git":      "github.com/org/app",
		"https://github.com/org/app":      "github.com/org/app",
		"https://u:p@github.com/org/app/": "github.com/org/app",
		"ssh://git@h:2222/x/y.git":        "h/x/y",
		"git@h:/x/y.git":                  "h/x/y",
		"git@github.com:org/App.GIT":      "github.com/org/app",
		"/srv/git/wiki.git":               "/srv/git/wiki",
		"":                                "",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

// A local relative repo, at the top level or in a profile, is resolved
// against the directory of the config file, also when the config file is
// given by a relative path. URLs, the scp-like form, absolute paths and paths
// starting with ~ are kept as written.
func TestRelativeRepo(t *testing.T) {
	t.Setenv("WIKICTL_PROFILE", "")
	d := t.TempDir()
	sub := filepath.Join(d, "conf")
	os.MkdirAll(sub, 0o755)
	p := filepath.Join(sub, "c.yaml")
	abs := filepath.Join(d, "abs.git")
	for repo, want := range map[string]string{
		"./remote.git":                filepath.Join(sub, "remote.git"),
		"remote.git":                  filepath.Join(sub, "remote.git"),
		"../wiki/remote.git":          filepath.Join(d, "wiki", "remote.git"),
		abs:                           abs,
		"~/wiki.git":                  "~/wiki.git",
		"https://h/r.git":             "https://h/r.git",
		"file:///srv/r.git":           "file:///srv/r.git",
		"ssh://git@h:2222/x/y.git":    "ssh://git@h:2222/x/y.git",
		"git@github.com:you/wiki.git": "git@github.com:you/wiki.git",
		"host:wiki.git":               "host:wiki.git",
	} {
		os.WriteFile(p, []byte("repo: "+repo+"\n"), 0o600)
		c, err := Load(p, Selector{})
		if err != nil || c.Repo != want {
			t.Errorf("repo %s: got %q, want %q (%v)", repo, c.Repo, want, err)
		}
	}

	os.WriteFile(p, []byte("repo: https://h/r.git\nprofiles:\n  local: {repo: ./local.git}\n"), 0o600)
	c, err := Load(p, Selector{Profile: "local"})
	if err != nil || c.Repo != filepath.Join(sub, "local.git") {
		t.Errorf("profile repo: %+v %v", c, err)
	}

	t.Chdir(d)
	if c, err := Load(filepath.Join("conf", "c.yaml"), Selector{Profile: "local"}); err != nil || c.Repo != filepath.Join(sub, "local.git") {
		t.Errorf("relative --config: %+v %v", c, err)
	}
	t.Setenv("WIKICTL_CONFIG", filepath.Join("conf", "c.yaml"))
	if c, err := Load("", Selector{Profile: "local"}); err != nil || c.Repo != filepath.Join(sub, "local.git") {
		t.Errorf("relative WIKICTL_CONFIG: %+v %v", c, err)
	}
}
