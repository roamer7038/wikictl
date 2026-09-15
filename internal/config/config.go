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
	"reflect"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// Config is the content of config.yaml with the selected profile applied.
type Config struct {
	Repo   string `yaml:"repo"`   // URL or path of the wiki repository; a relative local path is resolved against the directory of the config file
	Branch string `yaml:"branch"` // branch to use; detected from the remote when empty
	Author struct {
		Name  string `yaml:"name"`
		Email string `yaml:"email"`
	} `yaml:"author"` // commit author; falls back to git config user.*

	DefaultProfile string              `yaml:"default_profile"` // profile used when no other rule selects one
	Profiles       map[string]*Profile `yaml:"profiles"`        // named overrides of the top-level keys

	Path          string   `yaml:"-"` // file the configuration was read from
	Warnings      []string `yaml:"-"` // one line for each unknown key, which is ignored
	Profile       string   `yaml:"-"` // name of the selected profile; empty when none is selected
	ProfileSource string   `yaml:"-"` // how the profile was selected: one of the Source* constants
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
	Match Match `yaml:"match"`
}

// Match selects a profile automatically from the current directory.
type Match struct {
	Remotes []string `yaml:"remotes"` // globs over the origin remote in host/path form, such as github.com/org/*; a trailing /* matches any depth
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
// When the file is parsed but the configuration is invalid, the returned
// Config is not nil and holds the warnings, which may explain the error.
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
	f, err := parser.ParseBytes(b, 0)
	if err == nil {
		err = yaml.Unmarshal(b, c)
	}
	if err != nil {
		return nil, fmt.Errorf("config file %s: %s", p, yaml.FormatError(err, false, false))
	}
	a := anchors{}
	var body ast.Node
	if len(f.Docs) > 0 {
		body = f.Docs[0].Body
		ast.Walk(a, body)
	}
	// Unknown keys are ignored with a warning, so that a file written for
	// another version of wikictl still works.
	unknown, err := a.unknownKeys(body, reflect.TypeOf(*c), "")
	for _, k := range unknown {
		c.Warnings = append(c.Warnings, fmt.Sprintf("config file %s: unknown key %q is ignored", p, k))
	}
	if err != nil {
		return c, fmt.Errorf("config file %s: %w", p, err)
	}
	if err := c.selectProfile(sel); err != nil {
		return c, fmt.Errorf("config file %s: %w", p, err)
	}
	if c.Repo == "" {
		switch {
		case c.Profile != "":
			return c, fmt.Errorf("config file %s: repo is not set for profile %s", p, c.Profile)
		case len(c.Profiles) > 0:
			return c, fmt.Errorf("config file %s: repo is not set and no profile is selected; pass --profile, set WIKICTL_PROFILE, or add match or default_profile", p)
		}
		return c, fmt.Errorf("config file %s: repo is not set", p)
	}
	if isRelativeLocal(c.Repo) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return c, fmt.Errorf("config file %s: %w", p, err)
		}
		c.Repo = filepath.Join(filepath.Dir(abs), c.Repo)
	}
	return c, nil
}

// anchors maps the anchor names of a YAML file to the nodes they mark.
type anchors map[string]ast.Node

// Visit records n when it is an anchor; it makes anchors an ast.Visitor.
func (a anchors) Visit(n ast.Node) ast.Visitor {
	if an, ok := n.(*ast.AnchorNode); ok {
		a[an.Name.GetToken().Value] = an.Value
	}
	return a
}

// resolve follows anchors and aliases to the node that they stand for.
func (a anchors) resolve(n ast.Node) ast.Node {
	for {
		switch v := n.(type) {
		case *ast.AnchorNode:
			n = v.Value
		case *ast.AliasNode:
			n = a[v.Value.GetToken().Value]
		default:
			return n
		}
	}
}

// unknownKeys returns the keys of n, a YAML node, that the yaml tags of type t
// do not name, as sorted dotted paths below prefix. The keys that a merge key
// brings in count as keys of n. The error reports a key that is not a string
// where t is a struct, because goccy/go-yaml leaves such a struct empty
// without an error.
func (a anchors) unknownKeys(n ast.Node, t reflect.Type, prefix string) ([]string, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	m, ok := a.resolve(n).(ast.MapNode)
	if !ok {
		return nil, nil
	}
	fields := map[string]reflect.Type{}
	if t.Kind() == reflect.Struct {
		for i := range t.NumField() {
			f := t.Field(i)
			if name, _, _ := strings.Cut(f.Tag.Get("yaml"), ","); name != "" && name != "-" {
				fields[name] = f.Type
			}
		}
	}
	var out []string
	var first error
	add := func(keys []string, err error) {
		out = append(out, keys...)
		if first == nil {
			first = err
		}
	}
	for it := m.MapRange(); it.Next(); {
		if it.Key().IsMergeKey() {
			v := a.resolve(it.Value())
			values := []ast.Node{v}
			if s, ok := v.(*ast.SequenceNode); ok {
				values = s.Values
			}
			for _, v := range values {
				add(a.unknownKeys(v, t, prefix))
			}
			continue
		}
		var key any
		if err := yaml.NodeToValue(a.resolve(it.Key()), &key); err != nil {
			add(nil, err)
			continue
		}
		k, isString := key.(string)
		if !isString {
			k = fmt.Sprint(key)
			if key == nil {
				k = "null"
			}
		}
		switch t.Kind() {
		case reflect.Map:
			add(a.unknownKeys(it.Value(), t.Elem(), prefix+k+"."))
		case reflect.Struct:
			if ft, ok := fields[k]; !isString {
				add(nil, fmt.Errorf("key %q is not a string", prefix+k))
			} else if ok {
				add(a.unknownKeys(it.Value(), ft, prefix+k+"."))
			} else {
				add([]string{prefix + k}, nil)
			}
		}
	}
	sort.Strings(out)
	return out, first
}

// isRelativeLocal reports whether repo is a relative local path: not
// absolute, not starting with ~, not a URL with a scheme and not the
// scp-like form [user@]host:path, where a colon comes before the first slash.
func isRelativeLocal(repo string) bool {
	if filepath.IsAbs(repo) || repo == "~" || strings.HasPrefix(repo, "~/") || strings.Contains(repo, "://") {
		return false
	}
	if i := strings.Index(repo, ":"); i > 0 && !strings.ContainsAny(repo[:i], "/"+string(filepath.Separator)) {
		return false
	}
	return true
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
		p := strings.ToLower(strings.TrimSuffix(pat, "/"))
		ok, err := path.Match(p, remote)
		if err != nil {
			return false, fmt.Errorf("match.remotes %q: %w", pat, err)
		}
		if !ok && strings.HasSuffix(p, "/*") {
			ok = below(strings.TrimSuffix(p, "/*"), remote)
		}
		hit = hit || (ok && remote != "")
	}
	for _, p := range m.Paths {
		if p != "" && !filepath.IsAbs(expandHome(p)) {
			return false, fmt.Errorf("match.paths %q: must be absolute or start with ~", p)
		}
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

// below reports whether remote has more path elements than the glob parent
// and its leading elements match parent.
func below(parent, remote string) bool {
	n := strings.Count(parent, "/") + 1
	parts := strings.SplitN(remote, "/", n+1)
	if len(parts) <= n {
		return false
	}
	ok, _ := path.Match(parent, strings.Join(parts[:n], "/"))
	return ok
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
