package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/repo"
)

type grepLine struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type grepPath struct {
	Path string `json:"path"`
}

type grepCount struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

func grepFlags(a *app, fs *pflag.FlagSet) {
	fs.BoolVarP(&a.ignoreCase, "ignore-case", "i", false, "ignore case; letters other than ASCII only with -F")
	fs.BoolVarP(&a.filesWith, "files-with-matches", "l", false, "print only the paths of the files with a matching line")
	fs.BoolVarP(&a.filesWithout, "files-without-match", "L", false, "print only the paths of the files without a matching line")
	fs.BoolVarP(&a.countLines, "count", "c", false, "print the number of matching lines of each file that has one")
	fs.BoolVarP(&a.lineNumber, "line-number", "n", false, "print the line number before each line")
	fs.BoolVarP(&a.noFilename, "no-filename", "h", false, "print the lines and counts without the path")
	fs.BoolVarP(&a.word, "word-regexp", "w", false, "match whole words only")
	fs.BoolVarP(&a.invert, "invert-match", "v", false, "select the lines that do not match")
	fs.BoolVarP(&a.quiet, "quiet", "q", false, "print nothing, also with --json; the exit code tells whether anything was selected")
	fs.BoolVarP(&a.extended, "extended-regexp", "E", false, "read the patterns as extended regular expressions")
	fs.BoolVarP(&a.fixed, "fixed-strings", "F", false, "read the patterns as fixed strings")
	fs.StringArrayVarP(&a.patterns, "regexp", "e", nil, "search for `pattern`; may be given more than once")
	fs.BoolVar(&a.allMatch, "all-match", false, "with several -e, select only the files that match every pattern; not with -L")
}

func (a *app) checkGrep(c *command, args []string) error {
	// Without -e, the first argument is the pattern.
	paths := args
	if len(a.patterns) == 0 && len(args) > 0 {
		a.patterns, paths = args[:1], args[1:]
	}
	switch {
	case len(a.patterns) == 0:
		return &usageError{c, "missing pattern"}
	case a.extended && a.fixed:
		return &usageError{c, "-E and -F cannot be combined"}
	case a.filesWithout && a.allMatch:
		return &usageError{c, "-L and --all-match cannot be combined"}
	}
	var err error
	a.cleaned, err = cleanPaths(paths)
	return err
}

// cmdGrep searches the files under the paths with git grep, taking the options
// of GNU grep. It exits with 0 when a line is selected, 1 when none is, and 2
// when a path does not exist, unless -q selected anything. With -L, which
// prints the files without a matching line, the code still tells whether a
// line was selected.
func (a *app) cmdGrep(c *command, args []string) error {
	patterns, paths := a.patterns, a.cleaned
	missing, err := a.absent(paths)
	if err != nil {
		return err
	}
	flags := []string{"-I"}
	switch {
	case a.fixed && a.ignoreCase:
		// git grep -i ignores the case of ASCII letters only in the C locale.
		flags = append(flags, "-E")
		folded := make([]string, len(patterns))
		for i, p := range patterns {
			folded[i] = repo.FoldPattern(p)
		}
		patterns = folded
	case a.fixed:
		flags = append(flags, "-F")
	case a.extended:
		flags = append(flags, "-E")
	default:
		flags = append(flags, "-G")
	}
	if a.ignoreCase && !a.fixed {
		flags = append(flags, "-i")
	}
	if a.word {
		flags = append(flags, "-w")
	}
	if a.invert {
		flags = append(flags, "-v")
	}
	if a.allMatch {
		flags = append(flags, "--all-match")
	}
	// matchFlags is set for -L, whose output does not say whether a line was
	// selected: a second search with -l answers that for the exit code.
	var matchFlags []string
	switch {
	case a.filesWith:
		flags = append(flags, "-l")
	case a.filesWithout:
		matchFlags = append(slices.Clone(flags), "-l")
		flags = append(flags, "-L")
	case a.countLines:
		flags = append(flags, "-c")
	case a.quiet:
		flags = append(flags, "-l")
	default:
		flags = append(flags, "-n")
	}
	search := slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return slices.Contains(missing, p) })
	var records [][]string
	selected := false
	if len(paths) == 0 || len(search) > 0 {
		if records, err = a.grepRecords(c, flags, patterns, search); err != nil {
			return err
		}
		if matchFlags != nil {
			matching, err := a.grepRecords(c, matchFlags, patterns, search)
			if err != nil {
				return err
			}
			selected = len(matching) > 0
		}
	}
	items := []any{}
	var text strings.Builder
	for _, r := range records {
		p := escapeControl(r[0])
		prefix := p + ":"
		if a.noFilename {
			prefix = ""
		}
		switch {
		case len(r) == 1:
			items = append(items, grepPath{r[0]})
			fmt.Fprintln(&text, p)
		case len(r) == 2:
			n, _ := strconv.Atoi(r[1])
			items = append(items, grepCount{r[0], n})
			fmt.Fprintf(&text, "%s%d\n", prefix, n)
		default:
			n, _ := strconv.Atoi(r[1])
			items = append(items, grepLine{r[0], n, r[2]})
			if a.lineNumber {
				fmt.Fprintf(&text, "%s%d:%s\n", prefix, n, escapeControl(r[2]))
			} else {
				fmt.Fprintf(&text, "%s%s\n", prefix, escapeControl(r[2]))
			}
		}
	}
	if !a.quiet {
		a.emit(map[string]any{"items": items}, func(w io.Writer) { io.WriteString(w, text.String()) })
	}
	for _, p := range missing {
		fmt.Fprintf(a.stderr, "wikictl: %s: no such file or directory\n", escapeControl(p))
	}
	// Apart from -L, every form prints one record per selected file.
	if !a.filesWithout {
		selected = len(items) > 0
	}
	switch {
	case a.quiet && selected:
		return nil
	case len(missing) > 0:
		return exitStatus(ExitUsage)
	case !selected:
		return exitStatus(ExitError)
	}
	return nil
}

// grepRecords runs git grep with flags and reports a pattern that does not
// compile as a usage error of c.
func (a *app) grepRecords(c *command, flags, patterns, search []string) ([][]string, error) {
	records, err := a.repo.GrepRecords(flags, patterns, search)
	// git reports a pattern that does not compile as "fatal: -e option, '<pattern>': <reason>".
	var ge *repo.GitError
	if errors.As(err, &ge) {
		if _, reason, ok := strings.Cut(ge.Stderr, "fatal: -e option, '"); ok {
			return nil, &usageError{c, "invalid pattern: '" + strings.TrimSpace(reason)}
		}
	}
	if err != nil {
		return nil, &gitError{err}
	}
	return records, nil
}

// absent returns the paths that are neither a file nor a directory in the
// wiki. The root "." always exists.
func (a *app) absent(paths []string) ([]string, error) {
	var check []string
	for _, p := range paths {
		if p != "." {
			check = append(check, p)
		}
	}
	if len(check) == 0 {
		return nil, nil
	}
	files, err := a.repo.Files(check)
	if err != nil {
		return nil, &gitError{err}
	}
	var out []string
	for _, p := range check {
		if !slices.ContainsFunc(files, func(f string) bool { return f == p || strings.HasPrefix(f, p+"/") }) {
			out = append(out, p)
		}
	}
	return out, nil
}
