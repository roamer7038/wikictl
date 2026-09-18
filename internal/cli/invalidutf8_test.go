package cli

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// badByte is a byte that is not valid UTF-8 on its own, so that every value
// holding it is printed in base64 under another key by --json.
const badByte = "\xff"

// setupInvalidUTF8 creates a wiki whose file names, content, link target,
// commit author and commit subject all hold badByte, so that every value
// --json prints in base64 has a case here. The page bad/content.md has a
// valid name and holds the byte in a line and in a link target; the other
// page has the byte in its name and valid content.
func setupInvalidUTF8(t *testing.T) (cfgPath string) {
	t.Helper()
	cfgPath = setupEmpty(t)
	work := cloneRemote(t, cfgPath)
	files := map[string]string{
		"bad/" + badByte + "name.md": "---\nsummary: bad name\n---\n# bad name\nzzname\n",
		"bad/content.md": "---\nsummary: bad content\n---\n# bad content\nzzmark " + badByte + " here\n" +
			"\n## Links\n- see_also: [name](" + badByte + "name.md)\n",
	}
	for p, c := range files {
		f := filepath.Join(work, p)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, work, "git", "add", "-A")
	// "git commit" takes an author name or a message that is not valid UTF-8
	// for Latin-1 and re-encodes it, which would leave nothing invalid to
	// check, so the commit carrying those bytes is written as an object of
	// its own.
	tree := gitCapture(t, work, "", "write-tree")
	commit := gitCapture(t, work, "tree "+tree+"\n"+
		"author a"+badByte+"gent <a@a> 1700000000 +0000\n"+
		"committer a"+badByte+"gent <a@a> 1700000000 +0000\n\n"+
		"zz"+badByte+"subject\n", "hash-object", "-w", "-t", "commit", "--stdin")
	mustRun(t, work, "git", "update-ref", "refs/heads/main", commit)
	mustRun(t, work, "git", "push", "-q", "origin", "refs/heads/main:main")
	return cfgPath
}

// gitCapture runs git in dir with stdin and returns its standard output
// without the trailing newline.
func gitCapture(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Stdin = strings.NewReader(stdin)
	out, err := c.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// badName is the path of the page whose name is not valid UTF-8, and
// badContent the content of the page whose content is not.
var (
	badName    = "bad/" + badByte + "name.md"
	badContent = "---\nsummary: bad content\n---\n# bad content\nzzmark " + badByte + " here\n" +
		"\n## Links\n- see_also: [name](" + badByte + "name.md)\n"
)

// jsonOut runs the command with --json, checks its exit code and returns the
// object it printed.
func jsonOut(t *testing.T, cfg, stdin string, code int, args ...string) map[string]any {
	t.Helper()
	got, out, errs := runCLI(t, cfg, stdin, append([]string{"--json"}, args...)...)
	if got != code {
		t.Fatalf("%v: code=%d, want %d\n%s%s", args, got, code, out, errs)
	}
	var m map[string]any
	mustUnmarshal(t, out, &m)
	return m
}

// jsonItems returns the items[] of such an object.
func jsonItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	list, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("items is missing: %v", m)
	}
	out := make([]map[string]any, len(list))
	for i, e := range list {
		if out[i], ok = e.(map[string]any); !ok {
			t.Fatalf("items[%d] is not an object: %v", i, e)
		}
	}
	return out
}

// wantBase64 checks that o holds key+"_base64" with the base64 of want, and
// not key itself: the reader's rule is to use the base64 key when the plain
// one is absent.
func wantBase64(t *testing.T, o map[string]any, key, want string) {
	t.Helper()
	if v, ok := o[key]; ok {
		t.Errorf("%s is present as %q, want only %s_base64", key, v, key)
	}
	got, ok := o[key+"_base64"].(string)
	if !ok {
		t.Fatalf("%s_base64 is missing: %v", key, o)
	}
	b, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("%s_base64 %q: %v", key, got, err)
	}
	if string(b) != want {
		t.Errorf("%s_base64 decodes to %q, want %q", key, b, want)
	}
}

// wantPlain checks that o holds key with want and no base64 key beside it.
func wantPlain(t *testing.T, o map[string]any, key, want string) {
	t.Helper()
	if v, ok := o[key+"_base64"]; ok {
		t.Errorf("%s_base64 is present as %q, want only %s", key, v, key)
	}
	got, ok := o[key].(string)
	if !ok {
		t.Fatalf("%s is missing: %v", key, o)
	}
	if got != want {
		t.Errorf("%s is %q, want %q", key, got, want)
	}
}

// strList returns the strings of a JSON array.
func strList(t *testing.T, v any, key string) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("%s is not an array: %v", key, v)
	}
	out := make([]string, len(list))
	for i, e := range list {
		if out[i], ok = e.(string); !ok {
			t.Fatalf("%s[%d] is not a string: %v", key, i, e)
		}
	}
	return out
}

// TestInvalidUTF8Cat checks that cat prints a path and a content that are not
// valid UTF-8 in base64, and each of the two that is valid as it is.
func TestInvalidUTF8Cat(t *testing.T) {
	cfg := setupInvalidUTF8(t)
	items := jsonItems(t, jsonOut(t, cfg, "", ExitOK, "cat", badName))
	if len(items) != 1 {
		t.Fatalf("cat %q: %d items, want 1", badName, len(items))
	}
	wantBase64(t, items[0], "path", badName)
	wantPlain(t, items[0], "content", "---\nsummary: bad name\n---\n# bad name\nzzname\n")

	items = jsonItems(t, jsonOut(t, cfg, "", ExitOK, "cat", "bad/content.md"))
	if len(items) != 1 {
		t.Fatalf("cat bad/content.md: %d items, want 1", len(items))
	}
	wantPlain(t, items[0], "path", "bad/content.md")
	wantBase64(t, items[0], "content", badContent)
}

// TestInvalidUTF8Grep checks the path and the matched text of grep.
func TestInvalidUTF8Grep(t *testing.T) {
	cfg := setupInvalidUTF8(t)
	items := jsonItems(t, jsonOut(t, cfg, "", ExitOK, "grep", "zzmark"))
	if len(items) != 1 {
		t.Fatalf("grep zzmark: %d items, want 1", len(items))
	}
	wantPlain(t, items[0], "path", "bad/content.md")
	wantBase64(t, items[0], "text", "zzmark "+badByte+" here")

	for _, flag := range []string{"-l", "-c"} {
		items = jsonItems(t, jsonOut(t, cfg, "", ExitOK, "grep", flag, "zzname"))
		if len(items) != 1 {
			t.Fatalf("grep %s zzname: %d items, want 1", flag, len(items))
		}
		wantBase64(t, items[0], "path", badName)
	}
}

// TestInvalidUTF8Listings checks the paths of the commands that list them.
func TestInvalidUTF8Listings(t *testing.T) {
	cfg := setupInvalidUTF8(t)
	for _, c := range []struct {
		args []string
		want string // the path expected in base64
	}{
		{[]string{"ls", "bad"}, badName},
		{[]string{"tree"}, badName},
		{[]string{"find"}, badName},
		{[]string{"stat", badName}, badName},
		{[]string{"lint"}, badName},
	} {
		code := ExitOK
		if c.args[0] == "lint" {
			code = ExitInvalid
		}
		items := jsonItems(t, jsonOut(t, cfg, "", code, c.args...))
		found := false
		for _, it := range items {
			if s, ok := it["path_base64"].(string); ok {
				b, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					t.Fatalf("%v: path_base64 %q: %v", c.args, s, err)
				}
				if string(b) == c.want {
					found = true
					if _, ok := it["path"]; ok {
						t.Errorf("%v: path and path_base64 are both present: %v", c.args, it)
					}
				}
			}
		}
		if !found {
			t.Errorf("%v: no item has path_base64 of %q: %v", c.args, c.want, items)
		}
	}
}

// TestInvalidUTF8Links checks the target of links, which is the value passed
// back to another command.
func TestInvalidUTF8Links(t *testing.T) {
	cfg := setupInvalidUTF8(t)
	items := jsonItems(t, jsonOut(t, cfg, "", ExitOK, "links", "-o", "bad/content.md"))
	if len(items) == 0 {
		t.Fatalf("links bad/content.md: no items")
	}
	for _, it := range items {
		wantPlain(t, it, "path", "bad/content.md")
		wantBase64(t, it, "target", badName)
	}
}

// TestInvalidUTF8Log checks the author, the subject and the paths of --follow.
func TestInvalidUTF8Log(t *testing.T) {
	cfg := setupInvalidUTF8(t)
	items := jsonItems(t, jsonOut(t, cfg, "", ExitOK, "log", "-n", "1", "bad"))
	if len(items) != 1 {
		t.Fatalf("log: %d items, want 1", len(items))
	}
	wantBase64(t, items[0], "author", "a"+badByte+"gent")
	wantBase64(t, items[0], "subject", "zz"+badByte+"subject")

	items = jsonItems(t, jsonOut(t, cfg, "", ExitOK, "log", "--follow", "-n", "1", badName))
	if len(items) != 1 {
		t.Fatalf("log --follow: %d items, want 1", len(items))
	}
	// paths[] keeps its shape, so paths_base64 is a second array of the same
	// length and order rather than a key inside it.
	paths := strList(t, items[0]["paths"], "paths")
	b64 := strList(t, items[0]["paths_base64"], "paths_base64")
	if len(paths) != 1 || len(b64) != len(paths) {
		t.Fatalf("paths %q and paths_base64 %q, want one path in each", paths, b64)
	}
	b, err := base64.StdEncoding.DecodeString(b64[0])
	if err != nil || string(b) != badName {
		t.Errorf("paths_base64[0] decodes to %q (%v), want %q", b, err, badName)
	}
}

// TestInvalidUTF8Writes checks put, its conflict output, mv and rm.
func TestInvalidUTF8Writes(t *testing.T) {
	cfg := setupInvalidUTF8(t)
	newPath := "bad/" + badByte + "new.md"
	newPage := "---\nsummary: new\n---\n# new\n"

	m := jsonOut(t, cfg, newPage, ExitOK, "put", newPath)
	wantBase64(t, m, "path", newPath)

	// The conflict of a path that is not valid UTF-8 names it in base64 too,
	// with the content of the file as it is now.
	m = jsonOut(t, cfg, newPage, ExitConflict, "put", newPath)
	wantBase64(t, m, "path", newPath)
	wantPlain(t, m, "content", newPage)

	// The other way round: a valid path whose current content is not valid.
	m = jsonOut(t, cfg, newPage, ExitConflict, "put", "bad/content.md")
	wantPlain(t, m, "path", "bad/content.md")
	wantBase64(t, m, "content", badContent)

	movedPath := "bad/" + badByte + "moved.md"
	m = jsonOut(t, cfg, "", ExitOK, "mv", newPath, movedPath)
	list, ok := m["moved"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("mv: moved is %v, want one file", m["moved"])
	}
	mf, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("mv: moved[0] is not an object: %v", list[0])
	}
	wantBase64(t, mf, "from", newPath)
	wantBase64(t, mf, "to", movedPath)

	// rm keeps paths[] as it is and adds a second array of the same length
	// and order, so that a reader of paths[] alone is not broken.
	m = jsonOut(t, cfg, "", ExitOK, "rm", movedPath)
	paths := strList(t, m["paths"], "paths")
	b64 := strList(t, m["paths_base64"], "paths_base64")
	if len(paths) != 1 || len(b64) != len(paths) {
		t.Fatalf("rm: paths %q and paths_base64 %q, want one path in each", paths, b64)
	}
	b, err := base64.StdEncoding.DecodeString(b64[0])
	if err != nil || string(b) != movedPath {
		t.Errorf("rm: paths_base64[0] decodes to %q (%v), want %q", b, err, movedPath)
	}
}

// TestValidUTF8NoBase64 checks that a wiki of valid UTF-8 gets no base64 key
// anywhere: the output of the commands is unchanged for it.
func TestValidUTF8NoBase64(t *testing.T) {
	cfg := setup(t)
	for _, args := range [][]string{
		{"cat", "global/push.md"}, {"stat", "global/push.md"}, {"ls", "-R"}, {"tree"}, {"find"},
		{"grep", "lease"}, {"grep", "-l", "lease"}, {"links", "global/push.md"},
		{"log", "global/push.md"}, {"log", "--follow", "global/push.md"}, {"lint", "projects"},
	} {
		code, out, errs := runCLI(t, cfg, "", append([]string{"--json"}, args...)...)
		if code != ExitOK {
			t.Fatalf("%v: code=%d\n%s%s", args, code, out, errs)
		}
		keys, err := jsonKeys(out)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if i := slices.IndexFunc(keys, func(k string) bool { return strings.HasSuffix(k, "_base64") }); i >= 0 {
			t.Errorf("%v: key %q is present for valid UTF-8", args, keys[i])
		}
	}
	// A write and its conflict, which print a path of their own.
	newPage := "---\nsummary: new\n---\n# new\n"
	for _, c := range []struct {
		code int
		args []string
	}{{ExitOK, []string{"put", "global/new.md"}}, {ExitConflict, []string{"put", "global/new.md"}},
		{ExitOK, []string{"mv", "global/new.md", "global/new2.md"}}, {ExitOK, []string{"rm", "global/new2.md"}}} {
		code, out, errs := runCLI(t, cfg, newPage, append([]string{"--json"}, c.args...)...)
		if code != c.code {
			t.Fatalf("%v: code=%d, want %d\n%s%s", c.args, code, c.code, out, errs)
		}
		if strings.Contains(out, "_base64") {
			t.Errorf("%v: base64 key for valid UTF-8: %s", c.args, out)
		}
	}
}

// TestInvalidUTF8TextOutput checks that text output is unchanged: cat prints
// the bytes as stored, and the other commands keep the escaping they had,
// which leaves a byte such as 0xff as it is.
func TestInvalidUTF8TextOutput(t *testing.T) {
	cfg := setupInvalidUTF8(t)
	if _, out, _ := runCLI(t, cfg, "", "cat", "bad/content.md"); out != badContent {
		t.Errorf("cat text output is %q, want %q", out, badContent)
	}
	if _, out, _ := runCLI(t, cfg, "", "grep", "zzmark"); !strings.Contains(out, "zzmark "+badByte+" here") {
		t.Errorf("grep text output lost the byte: %q", out)
	}
	if _, out, _ := runCLI(t, cfg, "", "ls", "bad"); !strings.Contains(out, badByte+"name.md") {
		t.Errorf("ls text output lost the byte: %q", out)
	}
}

// TestInvalidUTF8NoHTMLEscape checks that building the object key by key did
// not start escaping HTML characters, which --json has never escaped.
func TestInvalidUTF8NoHTMLEscape(t *testing.T) {
	cfg := setupEmpty(t)
	pushFiles(t, cfg, map[string]string{"a.md": "---\nsummary: s\n---\n# a\n<b> & </b>\n"})
	_, out, _ := runCLI(t, cfg, "", "--json", "cat", "a.md")
	if !strings.Contains(out, "<b> & </b>") {
		t.Errorf("cat --json escaped HTML: %s", out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
}
