package cli

import (
	"reflect"
	"testing"
)

func TestSplitCommon(t *testing.T) {
	cases := []struct {
		in, rest, common []string
	}{
		{[]string{"a", "--json", "b"}, []string{"a", "b"}, []string{"--json"}},
		{[]string{"--dirs", "x,y", "a", "-no-fetch"}, []string{"a"}, []string{"--dirs", "x,y", "-no-fetch"}},
		{[]string{"--config=c.yaml", "--version"}, nil, []string{"--config=c.yaml", "--version"}},
		{[]string{"--any", "--", "--json"}, []string{"--any", "--", "--json"}, nil},
		{[]string{"--dirs"}, []string{"--dirs"}, nil},
	}
	for _, c := range cases {
		rest, common := splitCommon(c.in)
		if !reflect.DeepEqual(rest, c.rest) || !reflect.DeepEqual(common, c.common) {
			t.Errorf("%v: rest=%v common=%v", c.in, rest, common)
		}
	}
}

func TestWantsHelp(t *testing.T) {
	if !wantsHelp([]string{"x", "--help"}) || wantsHelp([]string{"--", "-h"}) || wantsHelp([]string{"x"}) {
		t.Error("wantsHelp")
	}
}
