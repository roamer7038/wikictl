package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestJSONContract checks the parts of the output that scripts and agents
// depend on: the exit code and the set of JSON keys of each command. Values,
// text output, and message wording are not compared. A change that alters an
// exit code or a key on purpose updates this table.
func TestJSONContract(t *testing.T) {
	cfg := setup(t)
	// init needs an empty repository; it shares the cache set by setup.
	d := t.TempDir()
	empty := filepath.Join(d, "empty.git")
	mustRun(t, "", "git", "init", "-q", "--bare", "-b", "main", empty)
	emptyCfg := filepath.Join(d, "empty.yaml")
	os.WriteFile(emptyCfg, []byte("repo: "+empty+"\nauthor: {name: a, email: a@a}\n"), 0o600)

	// The page links to an existing page and a missing one, so that get has
	// links and backlinks and lint has items.
	newPage := "---\nsummary: new\n---\n# New\n[push](push.md)\n[gone](gone.md)\n"
	errKeys := []string{"error", "message"}
	cases := []struct {
		name  string
		cfg   string // empty means cfg
		stdin string
		args  []string
		code  int
		keys  []string
	}{
		{"search", "", "", []string{"search", "--dirs", "global,projects/app", "lease"}, ExitOK,
			[]string{"items", "items[].matched", "items[].path", "items[].summary", "items[].title", "items[].updated"}},
		{"search/usage", "", "", []string{"search"}, ExitUsage, errKeys},
		{"ls", "", "", []string{"ls", "--dirs", "global"}, ExitOK,
			[]string{"items", "items[].path", "items[].summary", "items[].title", "items[].type", "items[].updated"}},
		{"context", "", "", []string{"context"}, ExitOK,
			[]string{"author", "branch", "config", "dirs", "machine", "mirror", "pages", "profile", "profile_source", "project", "remote", "repo"}},
		{"dirs", "", "", []string{"dirs"}, ExitOK, []string{"items", "items[].dir", "items[].pages", "items[].summary"}},
		{"put", "", newPage, []string{"put", "global/new.md"}, ExitOK, []string{"commit", "path", "sha"}},
		{"put/conflict", "", newPage, []string{"put", "global/new.md"}, ExitConflict,
			[]string{"content", "error", "message", "path", "reason", "sha"}},
		{"put/bad_path", "", newPage, []string{"put", "global/bad name.md"}, ExitInvalid, errKeys},
		{"get", "", "", []string{"get", "global/push.md"}, ExitOK,
			[]string{"backlinks", "backlinks[].path", "backlinks[].type", "body", "frontmatter", "links",
				"links[].note", "links[].target", "links[].type", "path", "sha", "title", "updated"}},
		{"get/not_found", "", "", []string{"get", "global/none.md"}, ExitError, errKeys},
		{"lint", "", "", []string{"lint", "--dirs", "global"}, ExitInvalid,
			[]string{"items", "items[].code", "items[].line", "items[].message", "items[].path"}},
		{"mv", "", "", []string{"mv", "global/new.md", "global/new2.md"}, ExitOK, []string{"commit", "path", "rewritten"}},
		{"mv/dir", "", "", []string{"mv", "projects/app/", "projects/app2/"}, ExitOK, []string{"commit", "moved", "path", "rewritten"}},
		{"rm", "", "", []string{"rm", "global/new2.md"}, ExitOK, []string{"commit", "path"}},
		{"init", emptyCfg, "", []string{"init"}, ExitOK, []string{"commit"}},
		{"init/exists", "", "", []string{"init"}, ExitError, errKeys},
		{"unknown", "", "", []string{"nope"}, ExitUsage, errKeys},
	}
	for _, c := range cases {
		cf := cfg
		if c.cfg != "" {
			cf = c.cfg
		}
		code, out, _ := runCLI(t, cf, c.stdin, append([]string{"--json"}, c.args...)...)
		if code != c.code {
			t.Errorf("%s: exit code %d, want %d", c.name, code, c.code)
		}
		keys, err := jsonKeys(out)
		if err != nil {
			t.Errorf("%s: stdout is not JSON: %v\n%s", c.name, err, out)
			continue
		}
		if !slices.Equal(keys, c.keys) {
			t.Errorf("%s: keys %q, want %q", c.name, keys, c.keys)
		}
	}
}

// opaqueKeys are objects whose keys are data rather than part of the output
// format: the page's frontmatter and the page counts per directory.
var opaqueKeys = map[string]bool{"frontmatter": true, "pages": true}

// jsonKeys returns the sorted key paths of a JSON document, such as
// "items[].path". Elements of an array share the path "<key>[]".
func jsonKeys(s string) ([]string, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, e := range v {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				set[p] = true
				if !opaqueKeys[p] {
					walk(p, e)
				}
			}
		case []any:
			for _, e := range v {
				walk(prefix+"[]", e)
			}
		}
	}
	walk("", v)
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys, nil
}
