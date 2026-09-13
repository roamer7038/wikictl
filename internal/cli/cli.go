// Package cli implements the wikictl command line: flag parsing, the
// command table, configuration loading and output formatting.
package cli

import (
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

// Exit codes. Each maps to the "error" field of the JSON error object.
const (
	ExitOK       = 0 // success
	ExitError    = 1 // general error, such as a missing page
	ExitUsage    = 2 // usage or configuration error
	ExitConflict = 3 // the page changed since it was read
	ExitInvalid  = 4 // the page violates the wiki format
	ExitGit      = 5 // a git command failed
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
	run     func(a *app, c *command, args []string) int
}

// commands lists the subcommands in the order shown by help.
var commands = []*command{
	{name: "init", maxArgs: 0,
		summary: "Create the initial pages in an empty repository",
		detail: `Create README.md and global/index.md, as a single commit, in the repository
given by repo in the config file. The remote repository itself must already
exist on the Git host. Fails with exit code 1 if the branch already exists.`,
		run: (*app).cmdInit},
	{name: "search", args: "<word>...", minArgs: 1, maxArgs: -1,
		summary: "Find pages containing the given words",
		detail: `Find pages that contain all of the words (case-insensitive, fixed strings).
Pages with "status: deprecated" are skipped unless --all is given. Results
are ordered by last update, newest first; with --any, pages matching more
words come first. Text output shows the summary of each page, or its title
(first heading, else the file name) when the page has no summary.

Output: items[] {path, summary, title, matched, updated}.`,
		flags: func(fs *flag.FlagSet) { searchFlags(fs) }, run: (*app).cmdSearch},
	{name: "get", args: "<path>", minArgs: 1, maxArgs: 1,
		summary: "Show a page with its links and backlinks",
		detail: `Show one page: its blob sha, frontmatter, title, body (without frontmatter
and the Links section), typed links from the Links section, and backlinks
from other pages. Pass the sha to "put --base" when updating the page.

Output: {path, sha, frontmatter, title, body, links[], backlinks[], updated}.`,
		run: (*app).cmdGet},
	{name: "ls", maxArgs: 0,
		summary: "List pages",
		detail: `List the pages under the search directories with their summary and type.
Pages with "status: deprecated" are skipped unless --all is given. Text
output shows the summary of each page, or its title (first heading, else the
file name) when the page has no summary.

Output: items[] {path, summary, title, type, updated}.`,
		flags: func(fs *flag.FlagSet) { lsFlags(fs) }, run: (*app).cmdLs},
	{name: "put", args: "<path> < content", minArgs: 1, maxArgs: 1,
		summary: "Create or replace a page from standard input",
		detail: `Read the whole page, frontmatter included, from standard input and commit it
as <path>. Omit --base for a new page. For an existing page pass --base with
the blob sha from get; without it, or if the page changed in the meantime, the
command exits with code 3 and prints the current content and sha. A page
whose frontmatter is invalid or whose path breaks the file name rules (see
"help lint") is rejected with exit code 4. A missing summary, links to missing
pages and names outside the recommended form only produce warnings on standard
error; "description" in the frontmatter is read as a synonym of "summary".

Output: {path, sha, commit}.`,
		flags: func(fs *flag.FlagSet) { putFlags(fs) }, run: (*app).cmdPut},
	{name: "mv", args: "<path> <newpath> | <dir>/ <newdir>/", minArgs: 2, maxArgs: 2,
		summary: "Move or rename a page or a directory, rewriting links",
		detail: `Move or rename a page. Links inside the moved page and links to it from other
pages are rewritten in the same commit, and the old file name (without .md) is
added to aliases when it changes. When both arguments end with a slash, every
page under <dir>/ is moved to <newdir>/ instead; file names do not change, so
no alias is added. A new path that breaks the file name rules (see "help lint")
is rejected with exit code 4.

Only links of the form [text](path) are rewritten. A bare path in a Links line,
such as "- part_of: index.md" or "- index.md", is left unchanged and becomes
a broken link; write page targets as [text](path).

Output: {path, commit, rewritten} or {path, commit, moved, rewritten}.`,
		flags: func(fs *flag.FlagSet) { msgFlag(fs) }, run: (*app).cmdMv},
	{name: "rm", args: "<path>", minArgs: 1, maxArgs: 1,
		summary: "Delete a page",
		detail: `Delete a page. Pages that link to it are left unchanged; lint reports them
as broken_link.

Output: {path, commit}.`,
		flags: func(fs *flag.FlagSet) { msgFlag(fs) }, run: (*app).cmdRm},
	{name: "lint", args: "[<path>...]", maxArgs: -1,
		summary: "Report pages that violate the wiki format",
		detail: `Check pages for missing_summary, frontmatter_invalid, links_syntax, broken_link
and the file name rules. Without arguments every page under the search
directories is checked. Exits with code 4 when violations are found.

File name rules: a page is <dir>/<name>.md, never at the wiki root. A file or
directory name must not be empty, start with a dot or <, or contain
whitespace, control characters or any of the characters " \ # ? : ( ) ` + "`" + `
(bad_path; put and mv reject such paths). Lowercase ASCII letters, digits and hyphens are
recommended; other names are reported as name_style. Names in one directory
that differ only by case collide on case-insensitive file systems and are
reported as case_collision, against the whole wiki.

A Links line is "- <type>: <target> | <note>", or "- <target>" for an untyped
see_also relation (an untyped URL must be "<scheme>://..."); the bullet may
be "-", "*" or "+" and may be indented.

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
wiki, not a page path.

Output: items[] {dir, pages, summary}; summary is "" without an index.md.`,
		run: (*app).cmdDirs},
	{name: "context", maxArgs: 0,
		summary: "Show the resolved configuration and search directories with their page counts",
		detail: `Show the config file, mirror directory, branch, author, the machine and
project names (as used for machines/<name>/ and projects/<name>/), the origin
remote of the current directory, and the search directories that other
commands use by default: up to four of global/, personal/, projects/<name>/
and machines/<name>/. Each is shown with the number of pages at any depth
under it ("." counts the whole wiki); 0 means the directory has no page yet.
Unlike dirs, which counts only the pages directly in each directory, pages in
subdirectories are included.

Output: {config, mirror, branch, author, machine, project, remote, dirs, pages}.`,
		run: (*app).cmdContext},
}

type app struct {
	cfg     *config.Config
	repo    *repo.Repo
	dirs    []string
	cfgPath string
	dirsArg string
	json    bool
	noFetch bool
	version bool
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
}

type errorOut struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

var osHostname = os.Hostname

// globalFlags registers the flags accepted before or after the command name.
func (a *app) globalFlags(fs *flag.FlagSet) {
	fs.StringVar(&a.cfgPath, "config", "", "read the configuration from `path` instead of $WIKICTL_CONFIG or $XDG_CONFIG_HOME/wikictl/config.yaml (~/.config/wikictl/config.yaml)")
	fs.StringVar(&a.dirsArg, "dirs", "", "search only the comma-separated `dirs` instead of the defaults; . is the whole wiki")
	fs.BoolVar(&a.json, "json", false, "print JSON")
	fs.BoolVar(&a.noFetch, "no-fetch", false, "do not fetch from the remote before running")
	fs.BoolVar(&a.version, "version", false, "print the version and exit")
}

// Main runs the command line given in args and returns the exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	fs := newFlagSet("wikictl")
	a.globalFlags(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return ExitOK
		}
		return a.fail(ExitUsage, "usage", err.Error()+`; run "wikictl help" for usage`)
	}
	if a.version {
		return a.cmdVersion()
	}
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(stderr)
		return ExitUsage
	}
	name, cargs := rest[0], rest[1:]
	cargs, common := splitCommon(cargs)
	if err := fs.Parse(common); err != nil {
		return a.fail(ExitUsage, "usage", err.Error())
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
		return a.fail(ExitUsage, "usage", "unknown command: "+name+`; run "wikictl help" for the list of commands`)
	}
	if wantsHelp(cargs) {
		printCommandHelp(stdout, c)
		return ExitOK
	}
	// Validate flags and argument count before touching the configuration,
	// so that usage errors never depend on the environment. Commands with
	// flags parse cargs again; commands without flags get the positional
	// arguments (with any "--" removed).
	check := newFlagSet(c.name)
	if c.flags != nil {
		c.flags(check)
	}
	rest, code, ok := a.parseFlags(c, check, cargs)
	if !ok {
		return code
	}
	if c.flags == nil {
		cargs = rest
	}
	if code := a.setup(); code != ExitOK {
		return code
	}
	return c.run(a, c, cargs)
}

// setup loads the configuration, opens the mirror and resolves the search directories.
func (a *app) setup() int {
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		return a.fail(ExitUsage, "usage", err.Error())
	}
	a.cfg = cfg
	if err := a.openRepo(); err != nil {
		return a.fail(ExitGit, "git", err.Error())
	}
	if a.dirsArg != "" {
		a.dirs = strings.Split(a.dirsArg, ",")
	} else {
		host, _ := osHostname()
		a.dirs = ctx.DefaultDirs(cfg, cwdRemote(), host)
	}
	return ExitOK
}

// splitCommon separates the global flags (--json, --dirs, --config,
// --no-fetch, --version) from the command arguments so that they may follow
// the command name. Everything after "--" belongs to the command.
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
		case strings.HasPrefix(name, "json=") || strings.HasPrefix(name, "no-fetch=") || strings.HasPrefix(name, "dirs=") || strings.HasPrefix(name, "config="):
			common = append(common, a)
		case (name == "dirs" || name == "config") && i+1 < len(args):
			common = append(common, a, args[i+1])
			i++
		default:
			rest = append(rest, a)
		}
	}
	return rest, common
}

// openRepo opens the mirror under $XDG_CACHE_HOME/wikictl (or
// ~/.cache/wikictl), named after the repository URL, and fetches unless
// --no-fetch was given.
func (a *app) openRepo() error {
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		h, _ := os.UserHomeDir()
		cache = filepath.Join(h, ".cache")
	}
	name := strings.NewReplacer("/", "_", ":", "_", "@", "_", "\\", "_").Replace(a.cfg.Repo)
	r, err := repo.Open(filepath.Join(cache, "wikictl", name), a.cfg.Repo, a.cfg.Branch)
	if err != nil {
		return err
	}
	a.repo = r
	if !a.noFetch {
		return r.Fetch()
	}
	return nil
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

// fail reports an error of the given kind and returns code. With --json the
// error object goes to stdout; otherwise one line goes to stderr.
func (a *app) fail(code int, kind, msg string) int {
	if a.json {
		enc := json.NewEncoder(a.stdout)
		enc.SetEscapeHTML(false)
		enc.Encode(errorOut{Error: kind, Message: msg})
	} else {
		fmt.Fprintln(a.stderr, "wikictl: "+msg)
	}
	return code
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
