package cli

import (
	"flag"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	ctx "github.com/roamer7038/wikictl/internal/context"
	"github.com/roamer7038/wikictl/internal/page"
)

type hit struct {
	Path    string   `json:"path"`
	Summary string   `json:"summary"`
	Matched []string `json:"matched"`
	Updated string   `json:"updated"`
}

type searchOpts struct {
	any bool
	n   int
	all bool
}

func searchFlags(fs *flag.FlagSet) *searchOpts {
	o := &searchOpts{}
	fs.BoolVar(&o.any, "any", false, "match pages containing any of the words instead of all of them")
	fs.IntVar(&o.n, "n", 20, "show at most `N` results")
	fs.BoolVar(&o.all, "all", false, "include pages with status: deprecated")
	return o
}

func (a *app) cmdSearch(c *command, args []string) int {
	fs := newFlagSet(c.name)
	o := searchFlags(fs)
	words, code, ok := a.parseFlags(c, fs, args)
	if !ok {
		return code
	}
	paths, err := a.repo.Grep(words, !o.any, a.dirs)
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	if !o.all {
		dep, _ := a.repo.GrepDeprecated(a.dirs)
		paths = filterOut(paths, dep)
	}
	updated, _ := a.repo.Updated(a.dirs)
	contents, err := a.repo.Cat(paths)
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	hits := []hit{}
	for _, p := range paths {
		pg := page.Parse(p, contents[p])
		matched := []string{}
		lower := strings.ToLower(string(contents[p]))
		for _, w := range words {
			if strings.Contains(lower, strings.ToLower(w)) {
				matched = append(matched, w)
			}
		}
		hits = append(hits, hit{Path: p, Summary: pg.Summary, Matched: matched, Updated: fmtTime(updated[p])})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if o.any && len(hits[i].Matched) != len(hits[j].Matched) {
			return len(hits[i].Matched) > len(hits[j].Matched)
		}
		if hits[i].Updated != hits[j].Updated {
			return hits[i].Updated > hits[j].Updated
		}
		return hits[i].Path < hits[j].Path
	})
	if len(hits) > o.n {
		hits = hits[:o.n]
	}
	a.emit(map[string]any{"items": hits}, func(w io.Writer) {
		for _, h := range hits {
			fmt.Fprintf(w, "%s\t%s\n", h.Path, h.Summary)
		}
	})
	return ExitOK
}

type getOut struct {
	Path        string         `json:"path"`
	SHA         string         `json:"sha"`
	Frontmatter map[string]any `json:"frontmatter"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	Links       []linkOut      `json:"links"`
	Backlinks   []backlinkOut  `json:"backlinks"`
	Updated     string         `json:"updated"`
}

type linkOut struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Note   string `json:"note"`
}

type backlinkOut struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (a *app) cmdGet(c *command, args []string) int {
	p := args[0]
	contents, err := a.repo.Cat([]string{p})
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	content, ok := contents[p]
	if !ok {
		return a.fail(ExitError, "error", "page not found: "+p)
	}
	head, _ := a.repo.Head()
	sha, _ := a.repo.BlobSHA(head, p)
	pg := page.Parse(p, content)
	links := []linkOut{}
	for _, l := range pg.Links {
		links = append(links, linkOut{l.Type, l.Target, l.Note})
	}
	backlinks := a.backlinks(p)
	updated, _ := a.repo.Updated([]string{path.Dir(p)})
	fm := pg.Frontmatter
	if fm == nil {
		fm = map[string]any{}
	}
	out := getOut{Path: p, SHA: sha, Frontmatter: fm, Title: pg.Title, Body: pg.Body, Links: links, Backlinks: backlinks, Updated: fmtTime(updated[p])}
	a.emit(out, func(w io.Writer) {
		fmt.Fprintf(w, "path: %s\nsha: %s\nupdated: %s\n\n%s", p, sha, out.Updated, pg.Body)
		if len(links) > 0 {
			fmt.Fprintln(w, "\nlinks:")
			for _, l := range links {
				fmt.Fprintf(w, "%s\n", strings.TrimRight("  "+l.Type+": "+l.Target+" "+l.Note, " "))
			}
		}
		if len(backlinks) > 0 {
			fmt.Fprintln(w, "\nbacklinks:")
			for _, b := range backlinks {
				fmt.Fprintf(w, "  %s (%s)\n", b.Path, b.Type)
			}
		}
	})
	return ExitOK
}

// backlinks greps the whole wiki for the slug of target to collect candidate
// pages, then parses each candidate and keeps those whose links resolve to
// target. Typed links win over body mentions.
func (a *app) backlinks(target string) []backlinkOut {
	slug := strings.TrimSuffix(path.Base(target), ".md")
	cands, _ := a.repo.Grep([]string{slug + ".md"}, true, nil)
	contents, _ := a.repo.Cat(cands)
	out := []backlinkOut{}
	for _, cp := range cands {
		if cp == target {
			continue
		}
		pg := page.Parse(cp, contents[cp])
		typed := false
		for _, l := range pg.Links {
			if !l.IsURL && l.Target == target {
				out = append(out, backlinkOut{cp, l.Type})
				typed = true
			}
		}
		if !typed {
			for _, m := range pg.Mentions {
				if m.Target == target {
					out = append(out, backlinkOut{cp, "mentions"})
					break
				}
			}
		}
	}
	return out
}

type lsItem struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
	Type    string `json:"type"`
	Updated string `json:"updated"`
}

type lsOpts struct {
	typ, tag string
	all      bool
}

func lsFlags(fs *flag.FlagSet) *lsOpts {
	o := &lsOpts{}
	fs.StringVar(&o.typ, "type", "", "only pages with this `type` in the frontmatter")
	fs.StringVar(&o.tag, "tag", "", "only pages tagged `tag`")
	fs.BoolVar(&o.all, "all", false, "include pages with status: deprecated")
	return o
}

func (a *app) cmdLs(c *command, args []string) int {
	fs := newFlagSet(c.name)
	o := lsFlags(fs)
	if _, code, ok := a.parseFlags(c, fs, args); !ok {
		return code
	}
	paths, err := a.repo.List(a.dirs)
	if err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	if !o.all {
		dep, _ := a.repo.GrepDeprecated(a.dirs)
		paths = filterOut(paths, dep)
	}
	contents, _ := a.repo.Cat(paths)
	updated, _ := a.repo.Updated(a.dirs)
	items := []lsItem{}
	for _, p := range paths {
		pg := page.Parse(p, contents[p])
		t, _ := pg.Frontmatter["type"].(string)
		if o.typ != "" && t != o.typ {
			continue
		}
		if o.tag != "" && !hasTag(pg.Frontmatter, o.tag) {
			continue
		}
		items = append(items, lsItem{p, pg.Summary, t, fmtTime(updated[p])})
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			fmt.Fprintf(w, "%s\t%s\n", it.Path, it.Summary)
		}
	})
	return ExitOK
}

// cmdContext prints the values other commands derive from the environment:
// machine and project are the resolved names used for machines/<name>/ and
// projects/<name>/, not the raw hostname and remote URL.
func (a *app) cmdContext(c *command, args []string) int {
	host, _ := osHostname()
	machine := a.cfg.Machine
	if machine == "" {
		machine = ctx.MachineName(host)
	}
	remote := cwdRemote()
	project := ""
	if remote != "" {
		project = ctx.ProjectName(remote)
		if m, ok := a.cfg.Projects[project]; ok && m != "" {
			project = m
		}
	}
	au, _ := a.author()
	out := map[string]any{"config": a.cfg.Path, "mirror": a.repo.Dir, "branch": a.repo.Branch, "author": au.Name,
		"machine": machine, "project": project, "remote": remote, "dirs": a.dirs}
	a.emit(out, func(w io.Writer) {
		for _, k := range []string{"config", "mirror", "branch", "author", "machine", "project", "remote"} {
			fmt.Fprintf(w, "%s: %v\n", k, out[k])
		}
		fmt.Fprintf(w, "dirs: %s\n", strings.Join(a.dirs, ", "))
	})
	return ExitOK
}

func hasTag(fm map[string]any, tag string) bool {
	ts, _ := fm["tags"].([]any)
	for _, t := range ts {
		if s, _ := t.(string); s == tag {
			return true
		}
	}
	return false
}

func filterOut(paths []string, drop map[string]bool) []string {
	var out []string
	for _, p := range paths {
		if !drop[p] {
			out = append(out, p)
		}
	}
	return out
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
