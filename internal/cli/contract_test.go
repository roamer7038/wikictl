package cli

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestJSONContract checks the parts of the output that scripts and agents
// depend on: the exit code and the set of JSON keys of each command. Values,
// text output, and message wording are not compared. A change that alters an
// exit code or a key on purpose updates this table.
func TestJSONContract(t *testing.T) {
	cfg := setup(t)

	// The page links to an existing page and a missing one, so that links has
	// both directions and lint has items.
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
		{"grep", "", "", []string{"grep", "lease"}, ExitOK, []string{"items", "items[].line", "items[].path", "items[].text"}},
		{"grep/files", "", "", []string{"grep", "-l", "lease"}, ExitOK, []string{"items", "items[].path"}},
		{"grep/count", "", "", []string{"grep", "-c", "lease"}, ExitOK, []string{"items", "items[].count", "items[].path"}},
		{"grep/none", "", "", []string{"grep", "zzz-none"}, ExitError, []string{"items"}},
		{"grep/usage", "", "", []string{"grep"}, ExitUsage, errKeys},
		{"ls", "", "", []string{"ls", "-R"}, ExitOK,
			[]string{"items", "items[].kind", "items[].path", "items[].summary", "items[].title", "items[].type", "items[].updated"}},
		{"context", "", "", []string{"context"}, ExitOK,
			[]string{"author", "branch", "config", "mirror", "profile", "profile_source", "remote", "repo"}},
		{"tree", "", "", []string{"tree"}, ExitOK, []string{"directories", "files", "items", "items[].kind", "items[].path"}},
		{"find", "", "", []string{"find"}, ExitOK, []string{"items", "items[].kind", "items[].path"}},
		{"find/usage", "", "", []string{"find", "-bogus"}, ExitUsage, errKeys},
		{"put", "", newPage, []string{"put", "global/new.md"}, ExitOK, []string{"commit", "path", "sha"}},
		{"put/conflict", "", newPage, []string{"put", "global/new.md"}, ExitConflict,
			[]string{"content", "error", "message", "path", "reason", "sha"}},
		{"put/bad_path", "", newPage, []string{"put", "global/bad name.md"}, ExitInvalid, errKeys},
		{"edit/usage", "", "", []string{"edit", "global/push.md"}, ExitUsage, errKeys},
		{"cat", "", "", []string{"cat", "global/push.md"}, ExitOK, []string{"items", "items[].content", "items[].path", "items[].sha"}},
		{"stat", "", "", []string{"stat", "global/push.md"}, ExitOK,
			[]string{"items", "items[].aliases", "items[].path", "items[].sha", "items[].status", "items[].summary",
				"items[].tags", "items[].title", "items[].type", "items[].updated"}},
		{"links", "", "", []string{"links", "global/push.md"}, ExitOK,
			[]string{"items", "items[].direction", "items[].note", "items[].target", "items[].type"}},
		{"lint", "", "", []string{"lint"}, ExitInvalid,
			[]string{"items", "items[].code", "items[].line", "items[].message", "items[].path"}},
		{"lint/dir", "", "", []string{"lint", "projects"}, ExitOK, []string{"items"}},
		{"mv", "", "", []string{"mv", "global/new.md", "global/new2.md"}, ExitOK, []string{"commit", "moved", "moved[].from", "moved[].to", "rewritten"}},
		{"mv/dir", "", "", []string{"mv", "projects/app", "projects/app2"}, ExitOK, []string{"commit", "moved", "moved[].from", "moved[].to", "rewritten"}},
		{"mv/not_replacing", "", "", []string{"mv", "global/push.md", "global/index.md"}, ExitError, []string{"commit", "moved", "rewritten"}},
		{"rm", "", "", []string{"rm", "global/new2.md"}, ExitOK, []string{"commit", "paths"}},
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
// format: the page's frontmatter.
var opaqueKeys = map[string]bool{"frontmatter": true}

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
