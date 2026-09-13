// Package config loads the user-side configuration file. The wiki itself
// holds no configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// Config is the content of config.yaml.
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
	Path     string            `yaml:"-"`        // file the configuration was read from
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

// Load reads the configuration from explicit, or $WIKICTL_CONFIG, or DefaultPath.
func Load(explicit string) (*Config, error) {
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
	if c.Repo == "" {
		return nil, fmt.Errorf("config file %s: repo is not set", p)
	}
	return c, nil
}
