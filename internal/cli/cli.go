// Package cli implements the wikictl command line: flag parsing, the
// command table, configuration loading and output formatting.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/roamer7038/wikictl/internal/config"
	ctx "github.com/roamer7038/wikictl/internal/context"
	"github.com/roamer7038/wikictl/internal/repo"
)

// command describes one subcommand.
type command struct {
	name    string
	args    string                 // positional argument synopsis, such as "<path>"
	minArgs int                    // minimum number of positional arguments
	maxArgs int                    // maximum number of positional arguments; -1 for unlimited
	summary string                 // one line for the command list
	detail  string                 // description shown by "help <command>"
	flags   func(fs *flag.FlagSet) // registers command flags; nil when there are none
	run     func(a *app, c *command, args []string) error
}

// commands lists the subcommands in the order shown by help.
var commands = []*command{
	{name: "init", maxArgs: 0,
		summary: "Create the initial pages in an empty repository",
		detail: `Create README.md and global/index.md in one commit, in the repository given
by repo in the config file. The repository must already exist. Fails with
exit code 1 if the branch shown by "wikictl context" already exists.`,
		run: (*app).cmdInit},
	{name: "search", args: "<word>...", minArgs: 1, maxArgs: -1,
		summary: "Find pages containing the given words",
		detail: `Find pages that contain all of the words (fixed strings, ignoring case)
anywhere in the file, frontmatter included. Pages with "status: deprecated"
are skipped unless --all is given. Results are ordered newest first; with
--any, pages matching more words come first. Text output shows the summary
of each page, or its title when it has none.

Output: items[] {path, summary, title, matched, updated}.`,
		flags: func(fs *flag.FlagSet) { searchFlags(fs) }, run: (*app).cmdSearch},
	{name: "get", args: "<path>", minArgs: 1, maxArgs: 1,
		summary: "Show a page with its links and backlinks",
		detail: `Show one page as parsed: its blob sha, frontmatter, title, body (without
frontmatter and the Links section), typed links from the Links section, and
backlinks from other pages: their typed links, or "mentions" for links in
their body. Pass the sha to "put --base" when updating the page. Text output
omits the frontmatter and title.

Output: {path, sha, frontmatter, title, body, links[], backlinks[], updated}.`,
		run: (*app).cmdGet},
	{name: "ls", maxArgs: 0,
		summary: "List pages",
		detail: `List the pages under the search directories with their summary and type.
Pages with "status: deprecated" are skipped unless --all is given. --tag
matches tags written as a YAML list. Text output shows the summary of each
page, or its title when it has none.

Output: items[] {path, summary, title, type, updated}.`,
		flags: func(fs *flag.FlagSet) { lsFlags(fs) }, run: (*app).cmdLs},
	{name: "put", args: "<path> < content", minArgs: 1, maxArgs: 1,
		summary: "Create or replace a page from standard input",
		detail: `Read the whole page, frontmatter included, from standard input and commit it
as <path>. Omit --base for a new page; for an existing page, pass the sha
printed by get. Fails with exit code 4 for a path that breaks the file name
rules, invalid frontmatter or a page over the size limits (see "help lint").
Other format problems only produce warnings. Content equal to the current
page creates no commit.

On a conflict, put exits with code 3 and writes nothing. Standard error shows
"wikictl: conflict (<reason>): <path> sha=<sha>" and standard output the
current content; with --json the output is {error, reason, path, sha,
content, message} with error "conflict". reason is "exists" when the path
already exists, or "changed" when the page no longer has the expected sha;
sha and content are empty when the page has been deleted.

Output: {path, sha, commit}.`,
		flags: func(fs *flag.FlagSet) { putFlags(fs) }, run: (*app).cmdPut},
	{name: "mv", args: "<path> <newpath> | <dir>/ <newdir>/", minArgs: 2, maxArgs: 2,
		summary: "Move or rename a page or a directory, rewriting links",
		detail: `Move or rename a page, or every page under <dir>/ when both arguments end
with a slash. In the same commit, links of the form [text](path) that point
to a moved page are rewritten, keeping how they are written, and the old file
name is added to aliases when it changes. Bare paths in Links lines are not
rewritten.

Fails with exit code 4 for a path that breaks the file name rules (see
"help lint"), 1 if the destination exists, and 3 if a page changed since it
was read (as for put; run it again).

Output: {path, commit, rewritten} or {path, commit, moved, rewritten}.`,
		flags: func(fs *flag.FlagSet) { msgFlag(fs) }, run: (*app).cmdMv},
	{name: "rm", args: "<path>", minArgs: 1, maxArgs: 1,
		summary: "Delete a page",
		detail: `Delete a page. Pages that link to it are left unchanged; lint reports them
as broken_link. Fails with exit code 4 for a path that is not a page path
(see "help lint"), and 3 if the page changed since it was read (as for put;
run it again). To delete a file that is not a page, use git in a clone.

Output: {path, commit}.`,
		flags: func(fs *flag.FlagSet) { msgFlag(fs) }, run: (*app).cmdRm},
	{name: "lint", args: "[<path>...]", maxArgs: -1,
		summary: "Report pages that violate the wiki format",
		detail: `Check pages against the rules below. Without arguments every page under the
search directories is checked; "wikictl --dirs . lint" checks the whole wiki.
Exits with code 4 when anything is found. Each finding is printed as
"<path>:<line>: <code>: <message>"; line 0 means the whole file.

Pages: a page is <dir>/<name>.md, never at the wiki root, with summary (or
description) in its frontmatter (missing_summary). A page over 1 MiB is
page_too_large; frontmatter over 64 KiB or nested over 100 levels is
frontmatter_invalid. The title is the first heading, else the file name.

File names: a file or directory name must not be empty, start with a dot or
<, or contain whitespace, control characters or any of " \ # ? : ( ) ` + "`" + `
(bad_path; put, mv and rm reject such paths). Lowercase ASCII letters, digits
and hyphens are recommended (name_style). Names in one directory that differ
only by case are reported as case_collision.

Links: "## Links" must be the last heading, and each line in it is
"- <type>: <target> | <note>" or "- <target>" for see_also (links_syntax); an
untyped URL must be "<scheme>://...". A page link is a relative path ending
in .md, optionally with #fragment. Page links in the Links section, and
[text](path) links in the body outside code, must point to an existing page
(broken_link).

Output: items[] {path, line, code, message}.`,
		run: (*app).cmdLint},
	{name: "dirs", args: "[<dir>...]", maxArgs: -1,
		summary: "List the directories of the wiki with their page counts",
		detail: `List every directory that directly contains a page, with the number of pages
directly in it (deprecated pages included) and the summary of its index.md.
The whole wiki is listed regardless of --dirs; each <dir> argument limits the
output to that directory and the ones below it.

Output: items[] {dir, pages, summary}; summary is "" without an index.md.`,
		run: (*app).cmdDirs},
	{name: "context", maxArgs: 0,
		summary: "Show the resolved configuration and search directories",
		detail: `Show the config file, the selected profile and how it was selected (flag,
env, match, default or none), the repository, mirror, branch, author, the
machine and project names, the origin remote of the current directory, and
the search directories with the number of pages at any depth under each.

The branch is branch in the config file, else the branch saved in the mirror,
else the remote HEAD, else main. The saved branch is kept until branch is set
or the mirror is deleted.

A profile matches when a match.remotes glob matches the origin remote as
lowercase host/path (* is one path element, a trailing /* any depth), or when
the current directory is at or below a match.paths entry. When several
profiles match, the command fails with exit code 2.

Output: {config, profile, profile_source, repo, mirror, branch, author,
machine, project, remote, dirs, pages}.`,
		run: (*app).cmdContext},
}

type app struct {
	cfg     *config.Config
	repo    *repo.Repo
	dirs    []string
	remote  string // origin URL of the current directory; "" when there is none
	cfgPath string
	profile string
	dirsArg string
	json    bool
	noFetch bool
	version bool
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
}

var osHostname = os.Hostname

// globalFlags registers the flags accepted before or after the command name.
func (a *app) globalFlags(fs *flag.FlagSet) {
	fs.StringVar(&a.cfgPath, "config", "", "read the configuration from `path` instead of $WIKICTL_CONFIG")
	fs.StringVar(&a.profile, "profile", "", "use the profile `name` from the config file")
	fs.StringVar(&a.dirsArg, "dirs", "", "search only the comma-separated `dirs`; . is the whole wiki")
	fs.BoolVar(&a.json, "json", false, "print JSON")
	fs.BoolVar(&a.noFetch, "no-fetch", false, "do not fetch before reading; writes still fetch")
	fs.BoolVar(&a.version, "version", false, "print the version and exit")
}

// Main runs the command line given in args and returns the exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	return a.report(a.run(args))
}

// run parses the command line and runs the command. Errors are reported by
// the caller.
func (a *app) run(args []string) error {
	fs := newFlagSet("wikictl")
	a.globalFlags(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(a.stdout)
			return nil
		}
		return &usageError{msg: err.Error() + `; run "wikictl help" for usage`}
	}
	if a.version {
		return a.cmdVersion()
	}
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(a.stderr)
		return exitStatus(ExitUsage)
	}
	name, cargs := rest[0], rest[1:]
	cargs, common := splitCommon(cargs)
	if err := fs.Parse(common); err != nil {
		return &usageError{msg: err.Error()}
	}
	if a.version {
		return a.cmdVersion()
	}
	switch name {
	case "help":
		return a.cmdHelp(cargs)
	case "version":
		if wantsHelp(cargs) {
			return a.cmdHelp([]string{"version"})
		}
		return a.cmdVersion()
	}
	c := lookup(name)
	if c == nil {
		return &usageError{msg: "unknown command: " + name + `; run "wikictl help" for the list of commands`}
	}
	if wantsHelp(cargs) {
		printCommandHelp(a.stdout, c)
		return nil
	}
	// Validate flags and argument count before touching the configuration,
	// so that usage errors never depend on the environment. Commands with
	// flags parse cargs again; commands without flags get the positional
	// arguments (with any "--" removed).
	check := newFlagSet(c.name)
	if c.flags != nil {
		c.flags(check)
	}
	rest, err := parseFlags(c, check, cargs)
	if err != nil {
		return err
	}
	if c.flags == nil {
		cargs = rest
	}
	if err := a.setup(); err != nil {
		return err
	}
	return c.run(a, c, cargs)
}

// setup loads the configuration with its profile, opens the mirror and
// resolves the search directories.
func (a *app) setup() error {
	a.remote = cwdRemote()
	dir, _ := os.Getwd()
	cfg, err := config.Load(a.cfgPath, config.Selector{Profile: a.profile, Dir: dir, Remote: a.remote})
	if err != nil {
		return &usageError{msg: err.Error()}
	}
	a.cfg = cfg
	if err := a.openRepo(); err != nil {
		return &gitError{err}
	}
	if a.dirsArg != "" {
		a.dirs = strings.Split(a.dirsArg, ",")
	} else {
		host, _ := osHostname()
		a.dirs = ctx.DefaultDirs(cfg, a.remote, host)
	}
	return nil
}

// splitCommon separates the global flags (--json, --dirs, --config,
// --profile, --no-fetch, --version) from the command arguments so that they
// may follow the command name. Everything after "--" belongs to the command.
func splitCommon(args []string) (rest, common []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(rest, args[i:]...), common
		}
		name := strings.TrimLeft(a, "-")
		switch {
		case name == "json" || name == "no-fetch" || name == "version":
			common = append(common, a)
		case strings.HasPrefix(name, "json=") || strings.HasPrefix(name, "no-fetch=") || strings.HasPrefix(name, "dirs=") || strings.HasPrefix(name, "config=") || strings.HasPrefix(name, "profile="):
			common = append(common, a)
		case (name == "dirs" || name == "config" || name == "profile") && i+1 < len(args):
			common = append(common, a, args[i+1])
			i++
		default:
			rest = append(rest, a)
		}
	}
	return rest, common
}

// openRepo opens the mirror under $XDG_CACHE_HOME/wikictl (or
// ~/.cache/wikictl), named by mirrorName, and fetches unless --no-fetch was
// given.
func (a *app) openRepo() error {
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		h, _ := os.UserHomeDir()
		cache = filepath.Join(h, ".cache")
	}
	r, err := repo.Open(filepath.Join(cache, "wikictl", mirrorName(a.cfg.Repo)), a.cfg.Repo, a.cfg.Branch)
	if err != nil {
		return err
	}
	a.repo = r
	if !a.noFetch {
		return r.Fetch()
	}
	return nil
}

// mirrorName returns the mirror directory name for the repository URL: the
// last path segment without ".git", followed by "-" and the first 12 hex
// digits of the SHA-256 of the whole URL. The hash keeps URLs that share the
// readable part apart; the readable part holds no host or user information.
func mirrorName(repoURL string) string {
	base := strings.TrimRight(repoURL, `/\`)
	if i := strings.LastIndexAny(base, `/\:`); i >= 0 {
		base = base[i+1:]
	}
	// A URL without a path leaves "token@host" here.
	if i := strings.LastIndex(base, "@"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".git")
	base = strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, base)
	base = strings.TrimLeft(base, ".")
	if len(base) > 64 {
		base = base[:64]
	}
	if base == "" {
		base = "wiki"
	}
	sum := sha256.Sum256([]byte(repoURL))
	return base + "-" + hex.EncodeToString(sum[:])[:12]
}

// cwdRemote returns the origin URL of the repository containing the current
// directory, or "" when there is none.
func cwdRemote() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// author returns the commit author from the configuration, falling back to
// git config user.name and user.email.
func (a *app) author() (repo.Author, error) {
	au := repo.Author{Name: a.cfg.Author.Name, Email: a.cfg.Author.Email}
	if au.Name == "" {
		out, _ := exec.Command("git", "config", "user.name").Output()
		au.Name = strings.TrimSpace(string(out))
	}
	if au.Email == "" {
		out, _ := exec.Command("git", "config", "user.email").Output()
		au.Email = strings.TrimSpace(string(out))
	}
	if au.Name == "" || au.Email == "" {
		return au, fmt.Errorf("author is not set: add author.name and author.email to %s, or set git config user.name and user.email", a.cfg.Path)
	}
	return au, nil
}

// emit writes v as JSON when --json is set; otherwise it calls text.
func (a *app) emit(v any, text func(w io.Writer)) {
	if a.json {
		enc := json.NewEncoder(a.stdout)
		enc.SetEscapeHTML(false)
		enc.Encode(v)
		return
	}
	if text != nil {
		text(a.stdout)
	}
}
