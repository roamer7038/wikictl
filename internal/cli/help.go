package cli

import (
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"text/tabwriter"
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

const description = `wikictl reads and writes a Markdown wiki in a Git repository with git only.`

const outputHelp = `Output:
  Text goes to standard output. With --json every command except help prints
  one JSON object, whose fields "wikictl help <command>" lists. Errors go to
  standard error as "wikictl: <message>", or with --json to standard output
  as {"error": "<kind>", "message": "..."}, where <kind> is error, usage,
  conflict, invalid or git. Warnings go to standard error as
  "wikictl: warning: <path>:<line>: <code>: <message>". In text output,
  control characters other than tab in summaries, titles, lint messages,
  warnings and the links shown by get are printed as \xNN; everything else
  is printed as is.
`

const mirrorHelp = `Mirror:
  The mirror under $XDG_CACHE_HOME/wikictl (~/.cache/wikictl) can be deleted
  at any time. Settings given with "git -c" do not apply; use a git config file.
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
	fmt.Fprintln(w, "Command flags must come before the arguments: \"wikictl search -n 5 lease\".")
	fmt.Fprintln(w, "A flag after an argument is taken as an argument.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags (before or after the command):")
	fs := newFlagSet("wikictl")
	(&app{}).globalFlags(fs)
	printFlags(w, fs)
	fmt.Fprintln(w)
	fmt.Fprint(w, outputHelp)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes:")
	fmt.Fprintln(w, "  0 success   1 error   2 usage or configuration   3 conflict")
	fmt.Fprintln(w, "  4 invalid page   5 git failure, with no partial result")
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
		c.flags(fs)
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

// printFlags lists the flags of fs as "--name <value>   usage (default x)".
// A backquoted word in a usage string names the value, as flag.UnquoteUsage does.
func printFlags(w io.Writer, fs *flag.FlagSet) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fs.VisitAll(func(f *flag.Flag) {
		value, usage := flag.UnquoteUsage(f)
		left := "-" + f.Name
		if len(f.Name) > 1 {
			left = "--" + f.Name
		}
		if value != "" {
			left += " <" + value + ">"
		}
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
			usage += " (default " + f.DefValue + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\n", left, usage)
	})
	tw.Flush()
}

// newFlagSet returns a FlagSet that neither prints nor exits by itself;
// callers report problems through usageError.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// wantsHelp reports whether args ask for help. Arguments after "--" are
// never flags.
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "-h", "--help", "-help":
			return true
		}
	}
	return false
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
	case "help", "-h", "--help", "-help":
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

// parseFlags parses args into fs and checks the number of positional
// arguments against c. It returns a usageError of c on failure.
func parseFlags(c *command, fs *flag.FlagSet, args []string) ([]string, error) {
	if err := fs.Parse(args); err != nil {
		return nil, &usageError{c, err.Error()}
	}
	rest := fs.Args()
	if len(rest) < c.minArgs {
		return nil, &usageError{c, "missing argument"}
	}
	if c.maxArgs >= 0 && len(rest) > c.maxArgs {
		return nil, &usageError{c, "too many arguments: " + strings.Join(rest[c.maxArgs:], " ")}
	}
	return rest, nil
}
