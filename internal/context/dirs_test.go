package context

import (
	"reflect"
	"testing"

	"github.com/roamer7038/wikictl/internal/config"
)

func TestNames(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:roki/My-App.git": "my-app",
		"https://gitea.example/roki/app": "app",
		"ssh://git@h:2222/x/y.git":       "y",
	} {
		if got := ProjectName(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
	if MachineName("Roki-WSL.local") != "roki-wsl" {
		t.Error(MachineName("Roki-WSL.local"))
	}
}

func TestDefaultDirs(t *testing.T) {
	c := &config.Config{}
	got := DefaultDirs(c, "git@github.com:roki/app.git", "host1")
	if !reflect.DeepEqual(got, []string{"global", "personal", "projects/app", "machines/host1"}) {
		t.Error(got)
	}
	got = DefaultDirs(c, "", "host1")
	if !reflect.DeepEqual(got, []string{"global", "personal", "machines/host1"}) {
		t.Error(got)
	}
	c.Dirs = []string{"team"}
	if got := DefaultDirs(c, "x", "h"); !reflect.DeepEqual(got, []string{"team"}) {
		t.Error(got)
	}
	c.Dirs = nil
	c.Machine = "m9"
	if got := DefaultDirs(c, "", "host1"); got[2] != "machines/m9" {
		t.Error(got)
	}
}

func TestProjectMapping(t *testing.T) {
	c := &config.Config{Projects: map[string]string{"wikictl-prototype": "wikictl"}}
	got := DefaultDirs(c, "https://github.com/roamer7038/wikictl-prototype.git", "h")
	if got[2] != "projects/wikictl" {
		t.Error(got)
	}
	if got := DefaultDirs(c, "git@x:y/other.git", "h"); got[2] != "projects/other" {
		t.Error(got)
	}
}
