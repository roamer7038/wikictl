package repo

import "testing"

func TestPushStatus(t *testing.T) {
	cases := map[string]string{
		"To /r.git\n \tabc:refs/heads/main\tdef..123\nDone\n":                                "ok",
		"To /r.git\n*\tabc:refs/heads/main\t[new branch]\nDone\n":                            "ok",
		"To /r.git\n!\tabc:refs/heads/main\t[rejected] (stale info)\nDone\n":                 "stale",
		"To /r.git\n!\tabc:refs/heads/main\t[remote rejected] (pre-receive hook declined)\n": "rejected",
		"fatal: could not read from remote repository\n":                                     "none",
		"": "none",
	}
	for in, want := range cases {
		if got := pushStatus(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
