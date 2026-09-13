// Package config loads the user-side configuration file. The wiki itself
// holds no configuration.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config is the content of config.yaml with the selected profile applied.
type Config struct {
	Repo   string `yaml:"repo"`   // URL of the wiki repository
	Branch string `yaml:"branch"` // branch to use; detected from the remote when empty
	Author struct {
		Name  string `yaml:"name"`
		Email string `yaml:"email"`
	} `yaml:"author"` // commit author; falls back to git config user.*
	Machine  string            `yaml:"machine"`  // name for machines/<name>/; defaults to the hostname up to the first dot
	Dirs     []string          `yaml:"dirs"`     // fixed search directories instead of the defaults
	Projects map[string]string `yaml:"projects"` // remote name -> directory name under projects/

	DefaultProfile string              `yaml:"default_profile"` // profile used when no other rule selects one
	Profiles       map[string]*Profile `yaml:"profiles"`        // named overrides of the top-level keys

	Path          string `yaml:"-"` // file the configuration was read from
	Profile       string `yaml:"-"` // name of the selected profile; empty when none is selected
	ProfileSource string `yaml:"-"` // how the profile was selected: one of the Source* constants
}

// Profile overrides the top-level keys of Config. Empty keys inherit the
// top-level value.
type Profile struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"` // not inherited when the profile sets repo
	Author struct {
		Name  string `yaml:"name"`
		Email string `yaml:"email"`
	} `yaml:"author"`
	Machine  string            `yaml:"machine"`
	Dirs     []string          `yaml:"dirs"`     // replaces the top-level list
	Projects map[string]string `yaml:"projects"` // replaces the top-level map
	Match    Match             `yaml:"match"`
}

// Match selects a profile automatically from the current directory.
type Match struct {
	Remotes []string `yaml:"remotes"` // globs over the origin remote in host/path form, such as github.com/org/*
	Paths   []string `yaml:"paths"`   // directories; the current directory or any directory below them matches
}

// Values of Config.ProfileSource.
const (
	SourceFlag    = "flag"    // --profile
	SourceEnv     = "env"     // $WIKICTL_PROFILE
	SourceMatch   = "match"   // match.remotes or match.paths
	SourceDefault = "default" // default_profile
	SourceNone    = "none"    // no profile; top-level keys only
)

// Selector holds what profile selection depends on besides the file.
type Selector struct {
	Profile string // --profile; takes precedence over $WIKICTL_PROFILE
	Dir     string // current directory, compared with match.paths
	Remote  string // origin URL of the current directory, compared with match.remotes
}

// DefaultPath returns $XDG_CONFIG_HOME/wikictl/config.yaml, or
// ~/.config/wikictl/config.yaml.
func DefaultPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "wikictl", "config.yaml")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "wikictl", "config.yaml")
}

// Load reads the configuration from explicit, or $WIKICTL_CONFIG, or
// DefaultPath, and applies the profile chosen by sel: --profile, then
// $WIKICTL_PROFILE, then match, then default_profile.
func Load(explicit string, sel Selector) (*Config, error) {
	p := explicit
	if p == "" {
		p = os.Getenv("WIKICTL_CONFIG")
	}
	if p == "" {
		p = DefaultPath()
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("config file %s does not exist; create it or pass --config <path>", p)
	}
	if err != nil {
		return nil, fmt.Errorf("config file %s: %w", p, err)
	}
	c := &Config{Path: p}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if err := c.selectProfile(sel); err != nil {
		return nil, fmt.Errorf("config file %s: %w", p, err)
	}
	if c.Repo == "" {
		switch {
		case c.Profile != "":
			return nil, fmt.Errorf("config file %s: repo is not set for profile %s", p, c.Profile)
		case len(c.Profiles) > 0:
			return nil, fmt.Errorf("config file %s: repo is not set and no profile is selected; pass --profile, set WIKICTL_PROFILE, or add match or default_profile", p)
		}
		return nil, fmt.Errorf("config file %s: repo is not set", p)
	}
	return c, nil
}

// selectProfile picks the profile and merges it into the top-level keys.
func (c *Config) selectProfile(sel Selector) error {
	name, source := sel.Profile, SourceFlag
	if name == "" {
		name, source = os.Getenv("WIKICTL_PROFILE"), SourceEnv
	}
	if name == "" {
		matched, err := c.matching(sel)
		if err != nil {
			return err
		}
		switch len(matched) {
		case 0:
		case 1:
			name, source = matched[0], SourceMatch
		default:
			return fmt.Errorf("the current directory matches profiles %s; pass --profile to choose one", strings.Join(matched, ", "))
		}
	}
	if name == "" {
		name, source = c.DefaultProfile, SourceDefault
	}
	if name == "" {
		c.ProfileSource = SourceNone
		return nil
	}
	pr, ok := c.Profiles[name]
	if !ok {
		from := map[string]string{SourceFlag: "--profile", SourceEnv: "WIKICTL_PROFILE", SourceDefault: "default_profile"}[source]
		return fmt.Errorf("profile %s given by %s is not defined", name, from)
	}
	c.Profile, c.ProfileSource = name, source
	if pr != nil {
		c.apply(pr)
	}
	return nil
}

// apply overrides the top-level keys with the non-empty keys of pr.
func (c *Config) apply(pr *Profile) {
	if pr.Repo != "" {
		c.Repo, c.Branch = pr.Repo, pr.Branch
	} else if pr.Branch != "" {
		c.Branch = pr.Branch
	}
	if pr.Author.Name != "" {
		c.Author.Name = pr.Author.Name
	}
	if pr.Author.Email != "" {
		c.Author.Email = pr.Author.Email
	}
	if pr.Machine != "" {
		c.Machine = pr.Machine
	}
	if len(pr.Dirs) > 0 {
		c.Dirs = pr.Dirs
	}
	if len(pr.Projects) > 0 {
		c.Projects = pr.Projects
	}
}

// matching returns the names of the profiles whose match rules accept the
// current directory, sorted.
func (c *Config) matching(sel Selector) ([]string, error) {
	remote := NormalizeRemote(sel.Remote)
	dir := resolvePath(sel.Dir)
	var out []string
	for name, pr := range c.Profiles {
		if pr == nil {
			continue
		}
		ok, err := pr.Match.accepts(remote, dir)
		if err != nil {
			return nil, fmt.Errorf("profile %s: %w", name, err)
		}
		if ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// accepts reports whether the normalized remote matches one of m.Remotes or
// dir lies under one of m.Paths.
func (m Match) accepts(remote, dir string) (bool, error) {
	hit := false
	for _, pat := range m.Remotes {
		ok, err := path.Match(strings.ToLower(strings.TrimSuffix(pat, "/")), remote)
		if err != nil {
			return false, fmt.Errorf("match.remotes %q: %w", pat, err)
		}
		hit = hit || (ok && remote != "")
	}
	if hit || dir == "" {
		return hit, nil
	}
	for _, p := range m.Paths {
		base := resolvePath(expandHome(p))
		if base == "" {
			continue
		}
		if dir == base || strings.HasPrefix(dir, strings.TrimSuffix(base, string(filepath.Separator))+string(filepath.Separator)) {
			return true, nil
		}
	}
	return false, nil
}

// NormalizeRemote rewrites a remote URL as host/path in lowercase, without
// scheme, user, port and trailing .git, so that the SSH and HTTPS forms of
// the same repository compare equal.
func NormalizeRemote(remote string) string {
	r := strings.TrimSpace(remote)
	if r == "" {
		return ""
	}
	if i := strings.Index(r, "://"); i >= 0 {
		if u, err := url.Parse(r); err == nil && u.Host != "" {
			r = u.Hostname() + u.Path
		} else {
			r = r[i+3:]
		}
	} else if i := strings.Index(r, ":"); i > 0 && !strings.Contains(r[:i], "/") {
		host := r[:i]
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
		r = host + "/" + strings.TrimPrefix(r[i+1:], "/")
	}
	r = strings.ToLower(strings.TrimSuffix(r, "/"))
	return strings.TrimSuffix(r, ".git")
}

// expandHome replaces a leading ~ with the home directory.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(h, p[1:])
}

// resolvePath returns p as an absolute path with symbolic links resolved
// when possible, or "" for an empty p.
func resolvePath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}
