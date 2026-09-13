// Package context derives the default search directories from the
// environment: the origin remote of the current directory and the hostname.
package context

import (
	"path"
	"strings"

	"github.com/roamer7038/wikictl/internal/config"
)

// ProjectName returns the last path element of a remote URL, without a
// trailing .git and in lowercase.
func ProjectName(remote string) string {
	r := strings.TrimSuffix(strings.TrimSpace(remote), "/")
	r = strings.TrimSuffix(r, ".git")
	if i := strings.LastIndexAny(r, "/:"); i >= 0 {
		r = r[i+1:]
	}
	return strings.ToLower(r)
}

// MachineName returns the hostname up to the first dot, in lowercase.
func MachineName(host string) string {
	if i := strings.Index(host, "."); i >= 0 {
		host = host[:i]
	}
	return strings.ToLower(host)
}

// DefaultDirs returns the search directories: global/, projects/<name>/ for
// the remote of the current directory (mapped through cfg.Projects), and
// machines/<name>/ for this host. cfg.Dirs replaces the whole list when set.
func DefaultDirs(cfg *config.Config, cwdRemote, host string) []string {
	if len(cfg.Dirs) > 0 {
		return cfg.Dirs
	}
	out := []string{"global"}
	if cwdRemote != "" {
		name := ProjectName(cwdRemote)
		if m, ok := cfg.Projects[name]; ok && m != "" {
			name = m
		}
		out = append(out, path.Join("projects", name))
	}
	m := cfg.Machine
	if m == "" {
		m = MachineName(host)
	}
	return append(out, path.Join("machines", m))
}
