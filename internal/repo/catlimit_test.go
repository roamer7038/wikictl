package repo

import (
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
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
	h, _ := r.Head()
	bigSHA, _ := r.BlobSHA(h, "global/big.md")
	if len(st) != 3 || st["global/big.md"] != (Object{SHA: bigSHA, Size: 101}) || st["global/limit.md"].Size != 100 {
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

func TestReadBlob(t *testing.T) {
	remote := newRemote(t, true)
	seedRemote(t, remote, map[string]string{"global/a.md": "hello\n"})
	r := openFetched(t, remote)
	h, _ := r.Head()
	sha, _ := r.BlobSHA(h, "global/a.md")
	var got []byte
	err := r.ReadBlob(sha, func(rd io.Reader) error {
		var err error
		got, err = io.ReadAll(rd)
		return err
	})
	if err != nil || string(got) != "hello\n" {
		t.Errorf("got=%q err=%v", got, err)
	}
	if err := r.ReadBlob(strings.Repeat("0", 40), func(io.Reader) error { return nil }); err == nil {
		t.Error("reading a missing blob must fail")
	}
}

// TestContainsFolded compares ContainsFolded, reading one byte at a time so
// that every chunk boundary is exercised, with Fold on the whole text.
func TestContainsFolded(t *testing.T) {
	texts := []string{
		"",
		"Äpfel und ΟΔΟΣ",
		"abc\xe3\x81",
		"a\xffb\xe3\x81\x82c",
		"\x80\x80\x80\x80Kelvin K",
	}
	words := []string{"", "äpfel", "οδος", "zzz", "\xe3\x81", "�", "b�", "あc", "kelvin k", "k", "c"}
	for _, text := range texts {
		got, err := ContainsFolded(iotest.OneByteReader(strings.NewReader(text)), words)
		if err != nil {
			t.Fatal(err)
		}
		want := make([]bool, len(words))
		for i, w := range words {
			want[i] = strings.Contains(Fold(text), Fold(w))
		}
		if !slices.Equal(got, want) {
			t.Errorf("text %q: got %v, want %v", text, got, want)
		}
		got, _ = ContainsFolded(strings.NewReader(text), words)
		if !slices.Equal(got, want) {
			t.Errorf("text %q in one chunk: got %v, want %v", text, got, want)
		}
	}
}
