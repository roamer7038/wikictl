package page

import "testing"

const sample = "# Title\n\nbody [x](a.md)\n\n```\n## Links\n# not heading\n```\n\n## Section\n\n## Links\n- part_of: [parent](../global/p.md)\n"

func TestScanFence(t *testing.T) {
	ls := ScanLines([]byte(sample), 4)
	if ls[0].N != 4 || ls[0].InFence {
		t.Errorf("first line: %+v", ls[0])
	}
	if !ls[5].InFence || ls[5].Text != "## Links" {
		t.Errorf("fenced Links must be InFence: %+v", ls[5])
	}
}

func TestLinksStartAndTitle(t *testing.T) {
	ls := ScanLines([]byte(sample), 4)
	i := LinksStart(ls)
	if i < 0 || ls[i].Text != "## Links" || ls[i].InFence {
		t.Fatalf("LinksStart=%d", i)
	}
	title, ok := Title(ls, i)
	if !ok || title != "Title" {
		t.Errorf("title=%q ok=%v", title, ok)
	}
	ls2 := ScanLines([]byte("# t\n## Links\n- a: [b](b.md)\n## after\n"), 1)
	if LinksStart(ls2) != -1 {
		t.Error("Links followed by a heading must not be a Links section")
	}
	if LinksStart(ScanLines([]byte("# t\nbody\n"), 1)) != -1 {
		t.Error("no Links section")
	}
}
