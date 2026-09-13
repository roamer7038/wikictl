package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "c.yaml")
	os.WriteFile(p, []byte("repo: https://h/r.git\nauthor: {name: n, email: e}\nmachine: m1\n"), 0o600)
	c, err := Load(p)
	if err != nil || c.Repo != "https://h/r.git" || c.Author.Name != "n" || c.Machine != "m1" || c.Path != p {
		t.Fatalf("%+v %v", c, err)
	}
	t.Setenv("WIKICTL_CONFIG", p)
	if c2, err := Load(""); err != nil || c2.Repo != c.Repo {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(d, "none.yaml")); err == nil {
		t.Error("missing file must error")
	}
	os.WriteFile(p, []byte("author: {name: n}\n"), 0o600)
	if _, err := Load(p); err == nil {
		t.Error("missing repo must error")
	}
}
