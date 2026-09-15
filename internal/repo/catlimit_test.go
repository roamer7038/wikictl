package repo

import (
	"maps"
	"strings"
	"testing"
)

func TestStatAndCatLimit(t *testing.T) {
	remote := newRemote(t, true)
	big := strings.Repeat("x", 101)
	seedRemote(t, remote, map[string]string{
		"global/small.md":    "---\nsummary: s\n---\n",
		"global/limit.md":    strings.Repeat("y", 100),
		"global/big.md":      big,
		"global/sub.md/a.md": "a",
	})
	r := openFetched(t, remote)
	paths := []string{"global/small.md", "global/limit.md", "global/big.md", "global/sub.md", "missing.md"}

	st, err := r.Stat(paths)
	if err != nil {
		t.Fatal(err)
	}
	bigSHA, _ := r.Git("rev-parse", r.snapshot+":global/big.md")
	if len(st) != 3 || st["global/big.md"] != (Object{SHA: strings.TrimSpace(bigSHA), Size: 101}) || st["global/limit.md"].Size != 100 {
		t.Errorf("stat=%v", st)
	}

	contents, objs, err := r.CatLimit(paths, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 2 || !strings.HasPrefix(string(contents["global/small.md"]), "---") || len(contents["global/limit.md"]) != 100 {
		t.Errorf("contents=%q", contents)
	}
	if !maps.Equal(objs, st) {
		t.Errorf("objs=%v", objs)
	}
}
