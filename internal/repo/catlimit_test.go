package repo

import (
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
	_, shas, _ := r.CatSHA([]string{"global/big.md"})
	if len(st) != 3 || st["global/big.md"] != (Object{SHA: shas["global/big.md"], Size: 101}) || st["global/limit.md"].Size != 100 {
		t.Errorf("stat=%v", st)
	}

	contents, large, err := r.CatLimit(paths, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 2 || !strings.HasPrefix(string(contents["global/small.md"]), "---") || len(contents["global/limit.md"]) != 100 {
		t.Errorf("contents=%q", contents)
	}
	if len(large) != 1 || large["global/big.md"] != st["global/big.md"] {
		t.Errorf("large=%v", large)
	}
}
