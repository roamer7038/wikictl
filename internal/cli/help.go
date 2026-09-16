package cli

import (
	"fmt"
	"io"
	"runtime/debug"
	"text/tabwriter"

	"github.com/spf13/pflag"
)

// version is set at build time:
//
//	go build -ldflags "-X github.com/roamer7038/wikictl/internal/cli.version=v0.1.0"
//
// When it is empty, the module version recorded by "go install" is used.
var version string

// Version returns the version string printed by "wikictl version".
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

const description = `wikictl reads and writes a Markdown wiki in a Git repository using only
git: no daemon, no index, no working tree. Pages are found with git grep and
written as commits pushed with --force-with-lease.`

const outputHelp = `Output:
  Text goes to standard output; with --json every command except help prints
  one JSON object, whose fields "wikictl help <command>" lists. Warnings go to
  standard error as "wikictl: warning: <path>:<line>: <code>: <message>", or
  "wikictl: warning: config file <path>: <message>" for the configuration.
  Errors go to standard error as "wikictl: <message>", or with --json to
  standard output as {"error": "<kind>", "message": "..."}, where <kind> is
  error, usage, conflict, invalid or git. In text output, control characters
  other than tab are printed as \xNN, except that a newline in an error or a
  configuration warning, such as in the output of git, starts a line indented
  by two spaces; JSON output and the content of files printed by cat or on a
  conflict are not changed. A backslash is not escaped, so a name holding the
  four characters \x01 cannot be told from the byte 0x01; --json keeps the
  control characters as stored, although invalid UTF-8 becomes U+FFFD there.
`

const writesHelp = `Writes:
  put, edit, mv and rm write their changes as one commit. Its message is -m,
  else "wikictl: <command> " followed by the paths, the sources before the
  destination for mv, or "<first path> and <n> more" when they are long.
  Nothing is printed on success unless -v is given. When a path to create
  already exists, as with put without --base, or a file to replace or delete
  no longer has the expected sha, because it changed since it was read or
  --base is not its current sha, nothing is written and the command exits
  with code 3: text output prints "wikictl: conflict (<reason>): <path>
  sha=<sha>" on standard error and the current content of the file on
  standard output, and --json prints {error, reason, path, sha, content,
  message} with error "conflict". reason is "exists" when the path already
  exists, or "changed" when the file no longer has the expected sha; sha and
  content are empty when the file has been deleted. With --no-fetch, edit, mv
  and rm read the mirror as last fetched, so a file that changed since then is
  reported as a conflict.
  A write that another clone pushes over is retried; when every attempt is
  rejected because the branch moved in between, nothing is written either and
  the command exits with code 3 and reason "moved", with no path, sha or
  content: text output prints "wikictl: conflict (moved): <message>" on
  standard error, followed by what git reported indented by two spaces, and
  --json prints {error, reason, message, detail}, where detail is that output
  of git. Nothing has to be re-read; run the command again. A push that fails
  for another reason, such as a branch that cannot be locked or a hook that
  rejects it, exits with 5, since running it again would not help.
`

const mirrorHelp = `Mirror:
  wikictl keeps a bare mirror of the wiki under $XDG_CACHE_HOME/wikictl
  (~/.cache/wikictl), shown by "wikictl context". If a mirror breaks, delete
  it; the next command creates it again. git in the mirror runs without the
  variables listed by "git rev-parse --local-env-vars", GIT_NAMESPACE and
  GIT_QUARANTINE_PATH, so settings given with "git -c" do not apply; put them
  in a git config file.
`

// printUsage writes the top-level help.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: wikictl [global flags] <command> [flags] [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, description)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(tw, "  %s\t%s\n", c.name, c.summary)
	}
	fmt.Fprintf(tw, "  %s\t%s\n", "version", "Print the version")
	fmt.Fprintf(tw, "  %s\t%s\n", "help", "Show help for a command")
	tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags may come before or after the arguments. Every argument after -- is an")
	fmt.Fprintln(w, "argument, even if it starts with -. Paths are relative to the wiki root: a")
	fmt.Fprintln(w, "leading / or ./ makes no difference, and neither does a trailing / except")
	fmt.Fprintln(w, "that mv uses it to move a directory. A path outside the wiki or with a")
	fmt.Fprintln(w, "control character is rejected with exit code 4. An empty path names no file:")
	fmt.Fprintln(w, `the commands that read report it as "no such file or directory" (grep with`)
	fmt.Fprintln(w, "code 2), and the commands that write reject it as bad_path with exit code 4.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags (before or after the command):")
	fs := newFlagSet("wikictl")
	(&app{}).globalFlags(fs)
	printFlags(w, fs)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Without --config, the configuration is $WIKICTL_CONFIG, else")
	fmt.Fprintln(w, "$XDG_CONFIG_HOME/wikictl/config.yaml (~/.config/wikictl/config.yaml); without")
	fmt.Fprintln(w, "--profile, the profile is $WIKICTL_PROFILE, else match, else default_profile,")
	fmt.Fprintln(w, `as "wikictl help context" describes.`)
	fmt.Fprintln(w)
	fmt.Fprint(w, outputHelp)
	fmt.Fprintln(w)
	fmt.Fprint(w, writesHelp)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes:")
	fmt.Fprintln(w, "  0 success   1 error   2 usage or configuration   3 conflict")
	fmt.Fprintln(w, "  4 invalid path or page   5 git failure")
	fmt.Fprintln(w, "  A write that lost the push race on every attempt also exits with 3, with")
	fmt.Fprintln(w, "  reason \"moved\". A git failure while reading the wiki also exits with 5 and")
	fmt.Fprintln(w, "  prints no partial result; so does a page that exists but cannot be read,")
	fmt.Fprintln(w, "  instead of \"no such file or directory\". A mirror that cannot be prepared")
	fmt.Fprintln(w, "  for a reason that is not a git failure, such as a cache directory that")
	fmt.Fprintln(w, "  cannot be created, exits with 2: it comes from the environment, so running")
	fmt.Fprintln(w, "  the command again would not help.")
	fmt.Fprintln(w)
	fmt.Fprint(w, mirrorHelp)
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Run "wikictl help <command>" for details on a command.`)
}

// printCommandHelp writes the help of one command.
func printCommandHelp(w io.Writer, c *command) {
	fmt.Fprintln(w, "Usage: "+synopsis(c))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.detail)
	if c.flags != nil {
		fs := newFlagSet(c.name)
		c.flags(&app{}, fs)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		printFlags(w, fs)
	}
}

// synopsis returns the one-line usage of c, such as "wikictl put [flags] <path> < content".
func synopsis(c *command) string {
	s := "wikictl " + c.name
	if c.flags != nil {
		s += " [flags]"
	}
	if c.args != "" {
		s += " " + c.args
	}
	return s
}

// printFlags lists the flags of fs as "-s, --name <value>   usage (default x)".
// A backquoted word in a usage string names the value, as pflag.UnquoteUsage does.
func printFlags(w io.Writer, fs *pflag.FlagSet) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fs.VisitAll(func(f *pflag.Flag) {
		value, usage := pflag.UnquoteUsage(f)
		left := "--" + f.Name
		if f.Shorthand != "" {
			left = "-" + f.Shorthand + ", " + left
		}
		if value != "" {
			left += " <" + value + ">"
		}
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			usage += " (default " + f.DefValue + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\n", left, usage)
	})
	tw.Flush()
}

// newFlagSet returns a FlagSet that neither prints nor exits by itself and
// lists its flags in the order they were registered; callers report problems
// through usageError.
func newFlagSet(name string) *pflag.FlagSet {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.SortFlags = false
	return fs
}

func (a *app) cmdVersion() error {
	a.emit(map[string]string{"version": Version()}, func(w io.Writer) { fmt.Fprintln(w, "wikictl "+Version()) })
	return nil
}

func (a *app) cmdHelp(args []string) error {
	if len(args) == 0 {
		printUsage(a.stdout)
		return nil
	}
	switch args[0] {
	case "help":
		fmt.Fprintln(a.stdout, "Usage: wikictl help [<command>]\n\nShow help for a command, or the list of commands.")
		return nil
	case "version":
		fmt.Fprintln(a.stdout, "Usage: wikictl version\n\nPrint the version.")
		return nil
	}
	c := lookup(args[0])
	if c == nil {
		return &usageError{msg: "unknown command: " + args[0]}
	}
	printCommandHelp(a.stdout, c)
	return nil
}

func lookup(name string) *command {
	for _, c := range commands {
		if c.name == name {
			return c
		}
	}
	return nil
}
