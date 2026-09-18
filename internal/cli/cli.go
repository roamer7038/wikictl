// Package cli implements the wikictl command line: flag parsing, the
// command table, configuration loading and output formatting.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/config"
	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/repo"
)

// command describes one subcommand.
type command struct {
	name    string
	args    string                                        // positional argument synopsis, such as "<path>"
	minArgs int                                           // minimum number of positional arguments
	maxArgs int                                           // maximum number of positional arguments; -1 for unlimited
	summary string                                        // one line for the command list
	detail  string                                        // description shown by "help <command>"
	flags   func(a *app, fs *pflag.FlagSet)               // registers command flags into fields of a; nil when there are none
	paths   bool                                          // the positional arguments are paths in the wiki, cleaned by wiki.Clean
	expr    bool                                          // the arguments are an expression for check to parse; only -h and arguments starting with "--" are flags
	writes  bool                                          // the command changes the wiki; openRepo always fetches for it, ignoring fetch_ttl
	check   func(a *app, c *command, args []string) error // validates the arguments before the configuration is read; nil when there is nothing to check
	run     func(a *app, c *command, args []string) error
}

// commands lists the subcommands in the order shown by help.
var commands = []*command{
	{name: "grep", args: "<pattern> [<path>...]", maxArgs: -1,
		summary: "Print the lines that match a pattern",
		detail: `Search the files under each path, or the whole wiki without paths, for the
lines that match the pattern, as "grep -r" does: the pattern is a basic regular
expression unless -E or -F is given, and each matching line is printed as
"<path>:<line>", or "<path>:<number>:<line>" with -n. As in GNU grep, -h
leaves out "<path>:" before the lines and the counts of -c, and --help prints
this help. With -e, which may be given more than once, every argument is a
path. Binary files are skipped, and pages with status: deprecated are searched
too. With -i, the case of letters other than ASCII is ignored only together
with -F. -E and -F, and -L and --all-match, cannot be combined. The command
exits with code 0 when a line is selected and 1 when none is; as in GNU grep
the code of -L also tells whether a line was selected, not whether a path was
printed, so -L can print paths and exit with code 1. A pattern that does not
compile is a usage error. A path that does not exist is reported on standard
error, the other paths are still searched, and the command exits with code 2,
or with 0 when -q selected anything. A submodule is not such a path: it holds
no line, so searching one selects nothing and exits with code 1.

The paths printed by -l and -L are one per line: pass them to another command
with "| tr '\n' '\0' | xargs -0 -r wikictl ls -lt", which keeps names holding
quotation marks or spaces intact and runs nothing when nothing matched. A name
holding a newline cannot be passed this way, and neither can one holding a
control character, which text output escapes as \xNN; --json prints the exact
names, but wikictl rejects a path holding a control character as bad_path with
exit code 4. A name holding a colon, allowed for a file that is not a page
(see "help lint"), makes "<path>:<text>", or "<path>:<line>:<text>" with -n,
ambiguous, as it does in GNU grep; use --json for such names.

Output: items[] {path, line, text}; with -l or -L, items[] {path}; with -c,
items[] {path, count}; with -q, nothing.`,
		flags: grepFlags, check: (*app).checkGrep, run: (*app).cmdGrep},
	{name: "cat", args: "<path>...", minArgs: 1, maxArgs: -1, paths: true,
		summary: "Print files as stored",
		detail: `Print each file as stored in the wiki, in the order given. A path that does
not exist ("no such file or directory"), is a directory ("is a directory") or
is a submodule ("is a submodule") is reported on standard error, also with
--json, the other files are still printed, and the command exits with code 1.
The sha in the JSON output is the one to pass to "put --base" when updating
the page.

Output: items[] {path, sha, content}.`,
		run: (*app).cmdCat},
	{name: "stat", args: "<path>...", minArgs: 1, maxArgs: -1, paths: true,
		summary: "Show the sha, last update and attributes of files",
		detail: `Show, for each page, its blob sha, the time of the last commit that changed
it, and the attributes read from it: title (the first heading, else the file
name), summary (or description), type, tags, status and aliases. A file that
is not a page, as "help lint" defines one, is shown too, with its sha and
update time and with the attributes empty. A path that does not exist, is a
directory or is a submodule is reported as cat reports it.

Output: items[] {path, sha, updated, title, summary, type, tags, status,
aliases}.`,
		run: (*app).cmdStat},
	{name: "links", args: "[<path>...]", maxArgs: -1, paths: true,
		summary: "List the links in pages and to them",
		detail: `List the links of each page, one per line as
"<direction><TAB><type><TAB><target>". Direction "out" is a link in the page:
a typed link from its Links section, or "mentions" for a link in its body to a
page or to an http or https URL that the Links section does not link to.
Direction "in" is a link to the page from another page: that page's typed
link, or "mentions". With -o only the links in the pages are listed, with -i
only the links to them.

A path that is a directory stands for the pages under it, and without a path
every page of the wiki is read, the pages with status: deprecated and the .md
files at the root included, as lint reads them. The path is printed before
each link, as "<path><TAB><direction>...", when more than one path is given,
when none is, or when a path is a directory; -H prints it always and -h
never, and of the two the one written last wins. For a graph of the whole
wiki use -o, since listing both directions gives every link twice, once for
each of the two pages it joins.

The pages that link to the pages read are found with one search, however many
they are. A path that does not exist, is a submodule or is not a page ("is not
a page") is reported as cat reports it, also with --json, and the command
exits with code 1.

Output: items[] {path, direction, type, target, note, line, url}. line is the
line the link is on in the page that holds it: the page of path for "out",
and the page of target for "in". url tells whether target is a URL instead of
a page.`,
		flags: linksFlags, run: (*app).cmdLinks},
	{name: "ls", args: "[<path>...]", maxArgs: -1, paths: true,
		summary: "List the entries of directories",
		detail: `List the files and directories directly in each directory given, or the file
itself for a path that is a file; without arguments, the root of the wiki.
Names of directories end with "/". With more than one argument or with -R,
the entries of each directory follow a line "<dir>:". Names starting with a
dot and pages whose frontmatter has status: deprecated are hidden unless -a is
given; submodules are never listed, and naming one reports "is a submodule".
With -l, a line shows the type from the frontmatter, the time of the last
commit that changed the entry (for a directory, any file under it), the name,
and the summary, or the title when the page has no summary; "-" marks an empty
type or time, a file that is not a page has no type, title or summary, and a
directory that holds only submodules has no time. A path that does not exist
("no such file or directory") or is a submodule ("is a submodule") is reported
on standard error, also with --json, the others are still listed, and the
command exits with code 1.

Output: items[] {path, kind, type, summary, title, updated}; kind is "file"
or "dir".`,
		flags: lsFlags, run: (*app).cmdLs},
	{name: "find", args: "[<path>...] [<expression>]", maxArgs: -1, expr: true,
		summary: "Find files and directories by name, type, update time or frontmatter",
		detail: `Print the path of each file and directory under each path, or under the root
of the wiki without paths, that matches the expression, one per line, starting
with the path itself, as "find" does. Pages with status: deprecated and names
starting with a dot are included; submodules are not listed, and naming one
reports "is a submodule". The expression is a list of primaries that must all
be true; "!" negates the primary that follows it.

  -name PATTERN    the last element of the path matches the shell pattern
  -path PATTERN    the path as printed, without a leading "./", matches the
                   shell pattern; * and ? also match "/"

In a pattern, a class name such as [:alpha:] covers ASCII characters only.
  -type f|d        the entry is a file or a directory
  -maxdepth N      descend at most N levels below the paths
  -mindepth N      print no entry less than N levels below the paths
  -mtime [+|-]N    the last commit that changed the entry (for a directory, any
                   file under it) is N days old, more than N days (+N) or
                   less (-N); the age is rounded down to whole days
  -newer PATH      the entry changed later than PATH
  -meta KEY=VALUE  the frontmatter value of KEY is VALUE, or a list with the
                   element VALUE; a number matches any number of equal value,
                   such as 1.50 for 1.5; a directory, a mapping and a page over
                   1 MiB never match

A path with no time of its own, such as a directory that holds only
submodules, matches neither -mtime nor -newer.

--frontmatter=KEY,... prints the frontmatter of every file that has one, as
-meta reads it, not only of the pages: each top-level key, the line it is
written on counted from the start of the file, and its value. The "=" is
required, so "--frontmatter status" reads status as a path; without keys
every key is printed. Text output becomes a list of the frontmatter keys,
one line per key as "<path><TAB><line><TAB><key><TAB><value>", where the
key and the value are JSON, so a file without frontmatter, or without any
of the keys given, prints no line at all. A key that a merge key ("<<")
brought in has the line of the "<<", which is not printed itself, and a
number that is not finite is ".inf", "-.inf" or ".nan". A frontmatter that
does not parse, or a file over 1 MiB, is a warning on standard error that
does not change the exit code, or frontmatter_error with --json.

Only -h and the arguments starting with "--", such as --json, are flags. A
path that does not exist ("no such file or directory") or is a submodule ("is
a submodule") is reported on standard error, the other paths are still
searched, and the command exits with code 1.

Output: items[] {path, kind}; kind is "file" or "dir". With --frontmatter,
each item also has frontmatter[] {key, line, value}, empty when no key is
printed and absent when the file has no frontmatter at all, or
frontmatter_error {code, message}, whose code is frontmatter_invalid or
page_too_large.`,
		flags: findFlags, check: (*app).checkFind, run: (*app).cmdFind},
	{name: "put", args: "<path> < content", minArgs: 1, maxArgs: 1, paths: true, writes: true,
		summary: "Create or replace a file from standard input",
		detail: `Read the content of a file from standard input and commit it as <path>, at the
wiki root or in a directory. Omit --base for a new file. For an existing file
pass --base with the blob sha from stat; without it, or if the file changed in
the meantime, the command exits with code 3 and prints the current content and
sha, as the conflict output below describes. A path that breaks the file name
rules (see "help lint"), or that git refuses to store, such as one with a
component git~1, which names .git on NTFS, is rejected with exit code 4 and a
message starting with "bad_path:", and nothing is committed. A path that names
a page (see "help lint") is checked as one: a page whose frontmatter is invalid
or that is over the size limits is rejected with exit code 4, and a missing
summary, Links lines that do not parse, links to files missing from the wiki
and names outside the recommended form only produce warnings on standard error;
"description" in the frontmatter is read as a synonym of "summary", and
"summary" wins when it is not blank. When the content equals the current
file, no commit is created and commit is the current commit. A path that is a
directory, a symbolic link or a submodule, or that is below a file, is
rejected with exit code 1; a file replaced keeps its mode.

The commit message is -m, else "wikictl: <command> " followed by the paths,
the sources before the destination for mv, or "<first path> and <n> more"
when they are long; edit, mv and rm name their own command the same way.

A conflict writes nothing and exits with code 3. Text output prints
"wikictl: conflict (<reason>): <path> sha=<sha>" on standard error and the
current content of the file on standard output, and --json prints {error,
reason, path, sha, content, message} with error "conflict". reason is
"exists" when the path to create already exists, as with put without --base,
or "changed" when a file to replace or delete no longer has the expected sha,
because it changed since it was read or --base is not its current sha; sha
and content are empty when the file has been deleted. A write that another
clone pushes over is retried; when every attempt is rejected because the
branch moved in between, nothing is written either and the command exits with
code 3 and reason "moved", with no path, sha or content: text output prints
"wikictl: conflict (moved): <message>" on standard error, followed by what
git reported indented by two spaces, and --json prints {error, reason,
message, detail}, where detail is that output of git. Nothing has to be
re-read; run the command again. edit, mv and rm report a conflict the same
way.

Output: {path, sha, commit}; with -v, text output is
"<path><TAB><sha><TAB><commit>".`,
		flags: putFlags, run: (*app).cmdPut},
	{name: "edit", args: "<path>", minArgs: 1, maxArgs: 1, paths: true, writes: true,
		summary: "Edit a file in an editor and commit it",
		detail: `Open the file in an editor and commit the result as put does, replacing the
file only if it has not changed since it was opened; a file that does not exist
starts empty. The editor is $VISUAL, else $EDITOR, else vi; one with spaces or
shell characters is run by the shell, so it may include arguments. Nothing is
committed when the content is unchanged, which includes a new file left empty.
When the result cannot be committed, because the file changed in the meantime
(exit code 3; see "wikictl help put"), the path or the content breaks the rules
that put applies (exit code 4), or the editor fails, or the editor leaves
something that is not the regular file wikictl created, such as a symbolic
link (exit code 1), the edited content is kept in a temporary file whose path
is printed on standard error; SIGTERM and SIGHUP while the editor runs print
it too and end wikictl with 128 plus the number of the signal, while an
interrupt is left to the editor. Standard input must be a terminal; otherwise
the command exits with code 2.

Output: {path, sha, commit}, printed only when the file is committed.`,
		flags: editFlags, check: (*app).checkEdit, run: (*app).cmdEdit},
	{name: "mv", args: "<src>... <dst>", minArgs: 1, maxArgs: -1, writes: true,
		summary: "Move or rename files and directories, rewriting links",
		detail: `Move files and directories as mv does. With one source and a destination that
is not a directory of the wiki, rename the source to the destination; otherwise
move every source into the destination directory, keeping its name. -T renames
even when the destination is a directory, and -t moves every argument into the
directory given. Files that are not pages, including a .md file with a path
component starting with a dot, move with their directory and keep their
content, every moved file keeps its mode, and a source that is or contains a
submodule is reported and not moved. A destination that exists is never
replaced: it is reported on standard error as "not replacing". A source that
does not exist, a destination below a file or ending with "/" that is not a
directory ("not a directory"), and a directory moved into itself are reported
too; the other sources are still moved, and the command exits with code 1. The
root of the wiki itself, or a destination that breaks the file name rules (see
"help lint") or that git refuses to store (see "help put"), is rejected with
exit code 4, and nothing is moved. A directory moved with files of both kinds
applies each file's rule by its own destination path, not the directory's,
and warns name_style, on standard error, only for a page among them whose
name is outside the recommended form. A directory holding a file whose name
was accepted only because it was not a page can fail to move this way, if the
destination makes that file a page instead; rm can still delete such a file.
A file added under a directory after mv read it is not moved.

Links in other pages to a moved .md file, whether a page or not, and relative
links inside a moved page whose destination changes with the move, are
rewritten in the same commit. Other links, links to files not ending in .md,
links inside files that are not pages, and links inside code spans and code
fences are left as written. Only the path of a rewritten link changes; a
leading "./", angle brackets, a query, a fragment and a title are kept. When
the name of a page changes, the old name (without .md) is added to aliases if
the frontmatter is empty or a block-style mapping and aliases is absent, a
sequence (block or flow style), or null; otherwise no alias is added and no
warning is printed. Moving a page below a directory starting with a dot
leaves it not a page afterwards (see "help lint"), so lint and links -i no
longer cover it, though links to it are still rewritten as above.

Only links of the form [text](path) are rewritten. A bare path in a Links line,
such as "- see_also: other.md" or "- other.md", is left unchanged and becomes
a broken link; write page targets as [text](path).

If a file that mv changes or deletes changed since mv read it, or a new path
was created, the command exits with code 3 and writes nothing (see "wikictl
help put"); run it again.

Output: {moved[] {from, to}, rewritten, commit}; moved lists every moved file,
rewritten counts the other pages whose links were rewritten, and commit is
empty when nothing was moved. With -v, text output is
"<from><TAB><to><TAB><commit>" for each moved file.`,
		flags: mvFlags, check: (*app).checkMv, run: (*app).cmdMv},
	{name: "rm", args: "<path>...", maxArgs: -1, paths: true, writes: true,
		summary: "Delete files or directories",
		detail: `Delete files, and with -r directories with every file under them, in one
commit. A path that is a directory without -r, is a submodule, or does not
exist, is reported on standard error, the other paths are still deleted, and
the command exits with code 1; with -f a path that does not exist is ignored,
and -f without any path deletes nothing and exits with code 0; -f does not
suppress the report of a submodule. With -r, a submodule under a deleted
directory is reported and left undeleted the same way, while the other files
under the directory are still deleted. The root of the wiki itself, or an
empty path without -f, is rejected with exit code 4 and nothing is deleted.
The file name rules (see "help lint") are not applied, so a file whose name
breaks them can be deleted, except a name holding a control character, which
is rejected as every path is. A file added under a directory after rm read it
is not deleted. Pages that link to a deleted page are left unchanged; lint
reports them as broken_link. If a file changed since rm read it, the command
exits with code 3 and deletes nothing (see "wikictl help put"); run it again.

Output: {paths, commit}; paths lists the deleted files, and commit is empty
when nothing was deleted. With -v, text output is "<path><TAB><commit>" for
each deleted file.`,
		flags: rmFlags, check: (*app).checkRm, run: (*app).cmdRm},
	{name: "lint", args: "[<path>...]", maxArgs: -1, paths: true,
		summary: "Report pages that violate the wiki format",
		detail: `Check pages for missing_summary, frontmatter_invalid, links_syntax,
broken_link, page_too_large and the file name rules. Each path is a page or a
directory; for a directory every page under it is checked, submodules and the
files that are not pages left out. Without arguments every page of the wiki is
checked. Exits with code 4 when violations are found. A path that does not
exist ("no such file or directory"), is a submodule ("is a submodule") or is a
file that is not a page ("is not a page") is reported on standard error, also
with --json, the other paths are still checked, and the command exits with
code 1, even when violations are found.

Each finding is printed as "<path>:<line>: <code>: <message>"; line 0 means
the whole file.

missing_summary: no summary or description, or no frontmatter.
links_syntax: a line in the Links section is not a valid Links line.
broken_link: a link to a page that does not exist in the wiki repository.

Size limits: a page over 1 MiB is not parsed and is reported as
page_too_large. Frontmatter over 64 KiB, or with collections nested more than
100 levels deep, is reported as frontmatter_invalid. Commands that read pages
treat such a page as having no frontmatter.

File name rules: a page is a file whose name ends in .md and whose path has no
component starting with a dot, at the wiki root or in a directory. A page's
file or directory name must not be empty, start with a dot or <, or contain
whitespace, control characters or any of the characters " \ # ? : ( ) ` + "`" + `
(bad_path). A file that is not a page follows a looser rule instead: only an
empty name, ".", ".." and a control character are bad_path, so a leading dot,
<, whitespace and the characters above are allowed. A name over 255 bytes is
bad_path either way, since a clone cannot check it out, and so is a path that
git itself refuses to store regardless of these rules (see "help put"). put,
edit and the destination of mv reject a path that breaks its rule, while rm
and moving such a file away still work, except for a name holding a control
character, which every command rejects. Lowercase ASCII letters, digits and
hyphens are recommended for a page's name; other names are reported as
name_style, never for a file that is not a page.
Names in one directory that differ only by case collide on case-insensitive
file systems and are reported as case_collision, against the whole wiki. Names
that differ only by Unicode normalisation, such as one written in NFC and one
in NFD, or by normalisation and case at once, are one name on a file system
that normalises them, as macOS does, and are reported as unicode_collision,
also against the whole wiki.

lint.ignore in the configuration lists rule names left out of the report, the
exit code and --json: only name_style and missing_summary may be listed, and
the same names are left out of the warnings that put, edit and mv print too.
A profile's lint.ignore replaces the top-level list rather than adding to it.
A name it may not list, such as broken_link, is warned about as a rule that
cannot be ignored; a misspelled name is warned about as not a rule at all.
Either warning goes to standard error and changes nothing about what is
reported.

A Links line is "- <type>: <target> | <note>", or "- <target>" for an untyped
see_also relation (an untyped URL must be "<scheme>://..."); the bullet may
be "-", "*" or "+" and may be indented. The "## Links" heading must be the last
heading of the page; one followed by another heading is reported as
links_syntax.

A page link is a relative path ending in .md, optionally followed by a ?query
or a #fragment; absolute paths and paths that leave the wiki are not page
links. Links of the form [text](path) in the body are also read, except inside
code fences and code spans: links lists them as "mentions", and lint reports
them as broken_link when the page is missing. Code fences are never
interpreted, so a "## Links" heading inside a fence does not start the Links
section.

Code fences follow CommonMark. A fence opens with a line of three or more
backticks or tildes indented by up to three spaces (after backticks, the rest
of the line must not contain a backtick), and closes only with a line of the
same character, at least as many times, followed by nothing but spaces and
tabs; a line such as "` + "```bash" + `" inside a fence does not close it. A fence
that is never closed runs to the end of the page.

The title of a page is the text of its first heading outside code fences and
before the Links section, else the file name. A closing sequence of # is
removed only when a space or a tab precedes it, so "# C#" has the title "C#".

Output: items[] {path, line, code, message}.`,
		run: (*app).cmdLint},
	{name: "tree", args: "[<dir>...]", maxArgs: -1, paths: true,
		summary: "Show files and directories as a tree",
		detail: `Show the files and directories under each directory as a tree, or under the
root of the wiki without arguments, followed by the number of directories and
files. As in tree, a directory shown at the top counts as a directory only
when something under it is listed, although items never lists it. Names
starting with a dot and pages whose frontmatter has status: deprecated are
hidden unless -a is given; submodules are never listed, so they count as
neither, and naming one reports "is a submodule". A path that does not exist
("no such file or directory"), is a file ("not a directory") or is a submodule
("is a submodule") is reported on standard error, also with --json, and the
command exits with code 1.

Output: {items[] {path, kind}, directories, files}; kind is "file" or "dir".`,
		flags: treeFlags, run: (*app).cmdTree},
	{name: "context", maxArgs: 0,
		summary: "Show the resolved configuration",
		detail: `Show the config file, the selected profile and how it was selected (flag,
env, match, default or none), the wiki repository, fetch_ttl and the time of
the last fetch recorded in the mirror, mirror directory, branch, author, and
the origin remote of the current directory.

The mirror is a bare clone under $XDG_CACHE_HOME/wikictl (~/.cache/wikictl).
git in it runs without the variables listed by "git rev-parse
--local-env-vars", GIT_NAMESPACE and GIT_QUARANTINE_PATH, so settings given
with "git -c" do not apply; put them in a git config file.

A read normally fetches before every command, like a write. fetch_ttl in the
config file (seconds) skips that fetch for a read, not a write, when the
mirror was fetched within that many seconds; unset or 0, its default, never
skips. It is for a read repeated over time, such as a shell session or an
agent's use of wikictl, and does not apply when the tracking ref does not
exist yet, such as right after the mirror was created. --no-fetch skips a
single read regardless of fetch_ttl. put, edit, mv and rm always fetch when
--no-fetch is not given, ignoring fetch_ttl: they decide what they change
from what they read, and a stale read could leave a file added on the remote
out of an rm -r or a mv.

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

Output: {config, profile, profile_source, repo, fetch_ttl, fetched, mirror,
branch, author, remote}; fetched is the last fetch time recorded in the
mirror, empty when the mirror has never been fetched.`,
		run: (*app).cmdContext},
}

type app struct {
	cfg     *config.Config
	repo    *repo.Repo
	remote  string // origin URL of the current directory; "" when there is none
	cfgPath string
	profile string
	json    bool
	noFetch bool
	version bool
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer

	// Command flags, registered by the flags function of the command.
	linksIn   bool
	linksOut  bool
	filename  filenameMode
	long      bool
	recursive bool
	byTime    bool
	dirsOnly  bool
	level     positiveInt
	all       bool
	base      string
	msg       string
	verbose   bool
	force     bool

	noTargetDir bool
	targetDir   string

	ignoreCase   bool
	filesWith    bool
	filesWithout bool
	countLines   bool
	lineNumber   bool
	word         bool
	invert       bool
	noFilename   bool
	quiet        bool
	extended     bool
	fixed        bool
	allMatch     bool
	patterns     []string

	find     *findQuery
	fmKeys   frontmatterKeys
	cleaned  []string     // the paths cleaned by the check of grep; for mv, the sources and the destination
	warnings []page.Issue // non-blocking issues of a write, printed when its commit succeeds
}

// globalFlags registers the flags accepted before or after the command name.
// Their defaults are the current values, so that registering them again for
// the command arguments keeps the values given before the command name.
func (a *app) globalFlags(fs *pflag.FlagSet) {
	fs.StringVar(&a.cfgPath, "config", a.cfgPath, "read the configuration from `path`")
	fs.StringVar(&a.profile, "profile", a.profile, "use the profile `name` from the config file")
	fs.BoolVar(&a.json, "json", a.json, "print JSON")
	fs.BoolVar(&a.noFetch, "no-fetch", a.noFetch, "do not fetch before reading; writes still fetch")
	fs.BoolVar(&a.version, "version", a.version, "print the version and exit")
}

// Main runs the command line given in args and returns the exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	return a.report(a.run(args))
}

// run parses the command line and runs the command. Errors are reported by
// the caller. The global flags before the command name are parsed first; the
// arguments after it are parsed once, with the global flags and the flags of
// the command, which may come before or after the positional arguments.
func (a *app) run(args []string) error {
	fs := newFlagSet("wikictl")
	a.globalFlags(fs)
	fs.SetInterspersed(false)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			printUsage(a.stdout)
			return nil
		}
		a.json = a.json || jsonRequested(fs, args)
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
	fs = newFlagSet(name)
	a.globalFlags(fs)
	switch name {
	case "help", "version":
		err := fs.Parse(cargs)
		switch {
		case errors.Is(err, pflag.ErrHelp):
			topic := name
			if name == "help" && len(fs.Args()) > 0 {
				topic = fs.Args()[0]
			}
			return a.cmdHelp([]string{topic})
		case err != nil:
			a.json = a.json || jsonRequested(fs, cargs)
			return &usageError{msg: err.Error() + `; run "wikictl help" for usage`}
		}
		if name == "help" && !a.version {
			return a.cmdHelp(fs.Args())
		}
		return a.cmdVersion()
	}
	c := lookup(name)
	if c == nil {
		return &usageError{msg: "unknown command: " + name + `; run "wikictl help" for the list of commands`}
	}
	if c.flags != nil {
		c.flags(a, fs)
	}
	// Flags and the argument count are validated before the configuration is
	// read, so that usage errors never depend on the environment.
	args, expr := cargs, []string(nil)
	if c.expr {
		args, expr = splitExpr(fs, cargs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			printCommandHelp(a.stdout, c)
			return nil
		}
		a.json = a.json || jsonRequested(fs, args)
		return &usageError{c, err.Error()}
	}
	if a.version {
		return a.cmdVersion()
	}
	pos := append(fs.Args(), expr...)
	if len(pos) < c.minArgs {
		return &usageError{c, "missing argument"}
	}
	if c.maxArgs >= 0 && len(pos) > c.maxArgs {
		return &usageError{c, "too many arguments: " + strings.Join(pos[c.maxArgs:], " ")}
	}
	if c.paths {
		var err error
		if pos, err = cleanPaths(pos); err != nil {
			return err
		}
	}
	if c.check != nil {
		if err := c.check(a, c, pos); err != nil {
			return err
		}
	}
	if err := a.setup(c.writes); err != nil {
		return err
	}
	return c.run(a, c, pos)
}

// jsonRequested reports whether args, which fs failed to parse, contain
// --json. pflag stops at the first error, so a --json after it would not be
// set, and the usage error would not be printed as JSON. The value of a flag
// that takes one is skipped, and nothing after "--" is a flag.
func jsonRequested(fs *pflag.FlagSet, args []string) bool {
	on := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return on
		case a == "--json":
			on = true
		case strings.HasPrefix(a, "--json="):
			on, _ = strconv.ParseBool(strings.TrimPrefix(a, "--json="))
		case strings.HasPrefix(a, "--"):
			if f := fs.Lookup(a[2:]); f != nil && f.NoOptDefVal == "" {
				i++
			}
		case len(a) > 1 && a[0] == '-':
			// In a group of short flags, the first one that takes a value
			// takes the rest of the group, or the next argument.
			for j := 1; j < len(a); j++ {
				if f := fs.ShorthandLookup(a[j : j+1]); f != nil && f.NoOptDefVal == "" {
					if j == len(a)-1 {
						i++
					}
					break
				}
			}
		}
	}
	return on
}

// setup loads the configuration with its profile and opens the mirror.
// writes is true for a command that changes the wiki, which always fetches.
func (a *app) setup(writes bool) error {
	a.remote = cwdRemote()
	dir, _ := os.Getwd()
	cfg, err := config.Load(a.cfgPath, config.Selector{Profile: a.profile, Dir: dir, Remote: a.remote})
	if cfg != nil {
		for _, w := range cfg.Warnings {
			fmt.Fprintln(a.stderr, "wikictl: warning: "+escapeMessage(w))
		}
	}
	if err != nil {
		return &usageError{msg: err.Error()}
	}
	a.cfg = cfg
	if err := a.openRepo(writes); err != nil {
		// Preparing the mirror can fail without git failing, such as when the
		// cache directory cannot be created. Such a failure comes from the
		// environment, where running the command again would not help, so it
		// is a configuration error rather than a git failure.
		var ge *repo.GitError
		if errors.As(err, &ge) {
			return &gitError{err}
		}
		return &usageError{msg: err.Error()}
	}
	return nil
}

// openRepo opens the mirror under $XDG_CACHE_HOME/wikictl (or
// ~/.cache/wikictl), named by mirrorName, and fixes the commit that the
// command reads. Unless --no-fetch was given, it fetches: writes (put, edit,
// mv, rm) always fetch, ignoring fetch_ttl, because they decide what they
// change from what they read; a read fetches unless fetch_ttl says the
// mirror was fetched recently enough.
func (a *app) openRepo(writes bool) error {
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
		if writes {
			err = r.Fetch()
		} else {
			ttl := 0
			if a.cfg.FetchTTL != nil {
				ttl = *a.cfg.FetchTTL
			}
			err = r.FetchIfStale(time.Duration(ttl) * time.Second)
		}
		if err != nil {
			return err
		}
	}
	return r.Snapshot()
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
	gitConfig := func(key string) string {
		out, _ := exec.Command("git", "config", key).Output()
		return strings.TrimSpace(string(out))
	}
	au := repo.Author{Name: a.cfg.Author.Name, Email: a.cfg.Author.Email}
	if au.Name == "" {
		au.Name = gitConfig("user.name")
	}
	if au.Email == "" {
		au.Email = gitConfig("user.email")
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
