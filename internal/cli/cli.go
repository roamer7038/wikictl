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
		detail: `Create README.md and global/index.md, as a single commit, in the repository
given by repo in the config file or its selected profile. The repository
itself must already exist; it may be on a Git host, on a server reached over
SSH, or a local bare repository; a relative local path is resolved against
the directory of the config file. Fails with exit code 1 if the branch already
exists. The branch is the one shown by "wikictl context"; in an empty
repository without branch in the config file, it is main.`,
		run: (*app).cmdInit},
	{name: "search", args: "<word>...", minArgs: 1, maxArgs: -1,
		summary: "Find pages containing the given words",
		detail: `Find pages that contain all of the words (fixed strings, ignoring case,
non-ASCII letters included) anywhere in the file, frontmatter included.
Pages with a line "status: deprecated" (unquoted, anywhere in the file) are
skipped unless --all is given. Results are ordered by last update, newest
first; with --any, pages matching more words come first. Text output shows
the summary of each page, or its title (first heading, else the file name)
when the page has no summary. Control characters other than tab are shown as
\xNN in text output.

Output: items[] {path, summary, title, matched, updated}.`,
		flags: func(fs *flag.FlagSet) { searchFlags(fs) }, run: (*app).cmdSearch},
	{name: "get", args: "<path>", minArgs: 1, maxArgs: 1,
		summary: "Show a page with its links and backlinks",
		detail: `Show one page: its blob sha, frontmatter, title, body (without frontmatter
and the Links section), typed links from the Links section, and backlinks
from other pages: their typed links, or "mentions" for links in their body.
Pass the sha to "put --base" when updating the page.

The page is shown as parsed, not as stored; put prints the stored content when
it reports a conflict. Text output shows the path, sha, update time, body,
links and backlinks; the frontmatter and title are only in JSON output.
Control characters other than tab in the targets and notes of the links are
shown as \xNN in text output; the body is printed as it is.

Output: {path, sha, frontmatter, title, body, links[], backlinks[], updated}.`,
		run: (*app).cmdGet},
	{name: "ls", maxArgs: 0,
		summary: "List pages",
		detail: `List the pages under the search directories with their summary and type.
Pages with a line "status: deprecated" (unquoted, anywhere in the file) are
skipped unless --all is given. --tag matches tags written as a YAML list. Text
output shows the summary of each page, or its title (first heading, else the
file name) when the page has no summary. Control characters other than tab
are shown as \xNN in text output.

Output: items[] {path, summary, title, type, updated}.`,
		flags: func(fs *flag.FlagSet) { lsFlags(fs) }, run: (*app).cmdLs},
	{name: "put", args: "<path> < content", minArgs: 1, maxArgs: 1,
		summary: "Create or replace a page from standard input",
		detail: `Read the whole page, frontmatter included, from standard input and commit it
as <path>. Omit --base for a new page. For an existing page pass --base with
the blob sha from get; without it, or if the page changed in the meantime, the
command exits with code 3 and prints the current content and sha. A page
whose frontmatter is invalid, that is over the size limits, or whose path
breaks the file name rules (see "help lint") is rejected with exit code 4. A
missing summary, Links lines that do not parse, links to files missing from
the wiki and names outside the recommended form only produce warnings on
standard error; "description" in the frontmatter is read as a synonym of
"summary", and "summary" wins when it is not blank. When the content equals
the current page, no commit is created and commit is the current commit. The
default commit message is "wikictl: put <path>".

Warnings are printed as "wikictl: warning: <path>:<line>: <code>: <message>";
control characters other than tab in the message are shown as \xNN.

On a conflict, text output prints "wikictl: conflict (<reason>): <path>
sha=<sha>" on standard error and the current content on standard output. With
--json the output is {error, reason, path, sha, content, message} with error
"conflict". reason is "exists" when the page (or the destination of mv)
already exists, or "changed" when it no longer has the expected sha; sha and
content are empty when the page has been deleted. mv and rm report conflicts
in the same way.

Output: {path, sha, commit}.`,
		flags: func(fs *flag.FlagSet) { putFlags(fs) }, run: (*app).cmdPut},
	{name: "mv", args: "<path> <newpath> | <dir>/ <newdir>/", minArgs: 2, maxArgs: 2,
		summary: "Move or rename a page or a directory, rewriting links",
		detail: `Move or rename a page. Links to it from other pages, and relative links inside
it whose destination changes with the move, are rewritten in the same commit,
and the old file name (without .md) is added to aliases when it changes. Other
links and links inside code spans and code fences are left as written. Only
the path of a rewritten link changes; a leading "./", angle brackets, a query,
a fragment and a title are kept. The alias is added only when the frontmatter
is empty or a block-style mapping and aliases is absent, a sequence (block or
flow style), or null; otherwise, such as when aliases is a string or there is
no frontmatter, no alias is added and no warning is printed. When both
arguments end with a slash, every page under <dir>/ is moved to <newdir>/
instead; file names do not change, so no alias is added. A path or new path
that breaks the file name rules (see "help lint") is rejected with exit code 4.
A rewritten page keeps its BOM, and all its lines get the line ending (CRLF or
LF) of its first line. If <newpath> already exists, or any page exists under
<newdir>/, the command fails with exit code 1. The default commit message is
"wikictl: mv <path> <newpath>". Warnings are printed as for put.

Only links of the form [text](path) are rewritten. A bare path in a Links line,
such as "- part_of: index.md" or "- index.md", is left unchanged and becomes
a broken link; write page targets as [text](path).

If a page that mv changes or deletes changed since mv read it, or the new path
was created, the command exits with code 3, writes nothing and prints the
current content and sha of that page, as put does; run it again. With
--no-fetch, mv builds the change from the unfetched mirror, so a page that
changed since the last fetch is reported as a conflict.

Output: {path, commit, rewritten} or {path, commit, moved, rewritten}; moved is
the number of pages moved, and rewritten counts only the other pages whose
links were rewritten.`,
		flags: func(fs *flag.FlagSet) { msgFlag(fs) }, run: (*app).cmdMv},
	{name: "rm", args: "<path>", minArgs: 1, maxArgs: 1,
		summary: "Delete a page",
		detail: `Delete a page. Pages that link to it are left unchanged; lint reports them
as broken_link. A path that is not a page path (see "help lint"), such as a
file at the wiki root, is rejected with exit code 4. If the page changed since
rm read it, the command exits with code 3, deletes nothing and prints the
current content and sha, as put does; run it again. With --no-fetch, a page
that changed since the last fetch is reported as a conflict. To delete a file
that is not a page, clone the wiki repository and use git. The default commit
message is "wikictl: rm <path>".

Output: {path, commit}.`,
		flags: func(fs *flag.FlagSet) { msgFlag(fs) }, run: (*app).cmdRm},
	{name: "lint", args: "[<path>...]", maxArgs: -1,
		summary: "Report pages that violate the wiki format",
		detail: `Check pages for missing_summary, frontmatter_invalid, links_syntax, broken_link,
page_too_large and the file name rules. Without arguments every page under the
search directories is checked; "wikictl --dirs . lint" checks the whole wiki.
Exits with code 4 when violations are found. Each finding is printed as
"<path>:<line>: <code>: <message>"; line 0 means the whole file.

missing_summary: no summary or description, or no frontmatter.
links_syntax: a line in the Links section is not a valid Links line.
broken_link: a link to a page that does not exist in the wiki repository.

Size limits: a page over 1 MiB is not parsed and is reported as
page_too_large. Frontmatter over 64 KiB, or with collections nested more than
100 levels deep, is reported as frontmatter_invalid. Commands that read pages
treat such a page as having no frontmatter.

File name rules: a page is <dir>/<name>.md, never at the wiki root. A file or
directory name must not be empty, start with a dot or <, or contain
whitespace, control characters or any of the characters " \ # ? : ( ) ` + "`" + `
(bad_path; put, mv and rm reject such paths). Lowercase ASCII letters, digits and hyphens are
recommended; other names are reported as name_style. Names in one directory
that differ only by case collide on case-insensitive file systems and are
reported as case_collision, against the whole wiki.

A Links line is "- <type>: <target> | <note>", or "- <target>" for an untyped
see_also relation (an untyped URL must be "<scheme>://..."); the bullet may
be "-", "*" or "+" and may be indented. The "## Links" heading must be the last
heading of the page; one followed by another heading is reported as
links_syntax.

A page link is a relative path ending in .md, optionally followed by
#fragment; absolute paths and paths that leave the wiki are not page links.
Links of the form [text](path) in the body are also read, except inside code
fences and code spans: get lists them as "mentions" backlinks, and lint
reports them as broken_link when the page is missing. Code fences are never
interpreted, so a "## Links" heading inside a fence does not start the
Links section.

Code fences follow CommonMark. A fence opens with a line of three or more
backticks or tildes indented by up to three spaces (after backticks, the rest
of the line must not contain a backtick), and closes only with a line of the
same character, at least as many times, followed by nothing but spaces and
tabs; a line such as "` + "```bash" + `" inside a fence does not close it. A fence
that is never closed runs to the end of the page.

The title of a page is the text of its first heading outside code fences and
before the Links section, else the file name. A closing sequence of # is
removed only when a space or a tab precedes it, so "# C#" has the title "C#".

Control characters other than tab in a message are shown as \xNN in text
output.

Output: items[] {path, line, code, message}.`,
		run: (*app).cmdLint},
	{name: "dirs", args: "[<dir>...]", maxArgs: -1,
		summary: "List the directories of the wiki with their page counts",
		detail: `List every directory that directly contains at least one page, with the
number of pages directly in it (nested directories are listed on their own)
and the summary of its index.md, or "(no index)" when it has none. Deprecated
pages are counted. The whole wiki is listed regardless of the search
directories, so --dirs has no effect; arguments restrict the output to the
directories at or below each <dir>, which must be a directory path inside the
wiki, not a page path. Control characters other than tab in the summary are
shown as \xNN in text output.

Output: items[] {dir, pages, summary}; summary is "" without an index.md.`,
		run: (*app).cmdDirs},
	{name: "context", maxArgs: 0,
		summary: "Show the resolved configuration and search directories with their page counts",
		detail: `Show the config file, the selected profile and how it was selected (flag,
env, match, default or none), the wiki repository, mirror directory, branch,
author, the machine and project names (as used for machines/<name>/ and
projects/<name>/), the origin remote of the current directory, and the search
directories that other commands use by default: up to four of global/,
personal/, projects/<name>/ and machines/<name>/. Each is shown with the
number of pages at any depth under it ("." counts the whole wiki); 0 means the
directory has no page yet. Unlike dirs, which counts only the pages directly in
each directory, pages in subdirectories are included.

The branch is branch in the config file, else the branch saved in the mirror,
else the remote HEAD, else main. The saved branch is kept, so a change of the
remote default branch is not followed until branch is set or the mirror is
deleted.

A profile is selected by --profile, else $WIKICTL_PROFILE, else match, else
default_profile. A profile matches when one of its match.remotes globs matches
the origin remote, normalised to lowercase host/path without scheme, user,
port and .git, or when the current directory is one of match.paths (absolute
or starting with ~) or below one. In a glob, * matches one path element and a
trailing /* matches any depth below. When several profiles match, the command
fails with exit code 2.

The user information (user:token@) of an HTTPS or other URL in repo and
remote is shown as ***@; an SSH user name without a password, such as git@, is
shown as it is.

Output: {config, profile, profile_source, repo, mirror, branch, author, machine, project, remote, dirs, pages}.`,
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
	fs.StringVar(&a.cfgPath, "config", "", "read the configuration from `path` instead of $WIKICTL_CONFIG or $XDG_CONFIG_HOME/wikictl/config.yaml (~/.config/wikictl/config.yaml)")
	fs.StringVar(&a.profile, "profile", "", "use the profile `name` from the config file instead of $WIKICTL_PROFILE, match or default_profile")
	fs.StringVar(&a.dirsArg, "dirs", "", "search only the comma-separated `dirs` instead of the defaults; . is the whole wiki")
	fs.BoolVar(&a.json, "json", false, "print JSON")
	fs.BoolVar(&a.noFetch, "no-fetch", false, "do not fetch from the remote before reading; writes still fetch before committing")
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
