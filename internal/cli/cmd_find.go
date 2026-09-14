package cli

import (
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// findQuery is the expression of find, parsed by checkFind. The tests read
// the times and frontmatter that cmdFind fills in.
type findQuery struct {
	paths       []string
	tests       []findTest
	minDepth    int
	maxDepth    int      // -1 for no limit
	newer       []string // the paths given to -newer
	needTimes   bool
	needPages   bool
	now         time.Time
	updated     map[string]time.Time
	frontmatter map[string]map[string]any
}

type findTest struct {
	not   bool
	match func(e findEntry) bool
}

type findEntry struct {
	path string
	dir  bool
}

// findValue lists the primaries that take a value.
var findValue = map[string]bool{"-name": true, "-path": true, "-type": true, "-maxdepth": true, "-mindepth": true,
	"-mtime": true, "-newer": true, "-meta": true}

// splitExpr separates the flags of wikictl, which are the arguments starting
// with "--" and -h, from the paths and the expression of find. The value of a
// primary or of a flag is kept with it.
func splitExpr(fs *pflag.FlagSet, args []string) (flags, expr []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case findValue[arg] && i+1 < len(args):
			expr = append(expr, arg, args[i+1])
			i++
		case arg == "-h" || strings.HasPrefix(arg, "--"):
			flags = append(flags, arg)
			if f := fs.Lookup(arg[min(2, len(arg)):]); f != nil && f.NoOptDefVal == "" && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		default:
			expr = append(expr, arg)
		}
	}
	return flags, expr
}

func (a *app) checkFind(c *command, args []string) error {
	q := &findQuery{maxDepth: -1}
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") && args[i] != "!" {
		i++
	}
	paths, err := cleanPaths(args[:i])
	if err != nil {
		return err
	}
	q.paths = paths
	not := false
	for ; i < len(args); i++ {
		arg := args[i]
		if arg == "!" {
			not = !not
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return &usageError{c, "paths must precede the expression: " + arg}
		}
		if !findValue[arg] {
			return &usageError{c, "unknown primary: " + arg}
		}
		if i+1 == len(args) {
			return &usageError{c, "missing argument to " + arg}
		}
		i++
		val := args[i]
		var match func(findEntry) bool
		switch arg {
		case "-name", "-path":
			re, err := globRegexp(val)
			if err != nil {
				return &usageError{c, "invalid pattern: " + val}
			}
			if arg == "-name" {
				match = func(e findEntry) bool { return re.MatchString(path.Base(e.path)) }
			} else {
				match = func(e findEntry) bool { return re.MatchString(e.path) }
			}
		case "-type":
			if val != "f" && val != "d" {
				return &usageError{c, "-type must be f or d"}
			}
			match = func(e findEntry) bool { return e.dir == (val == "d") }
		case "-maxdepth", "-mindepth":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return &usageError{c, arg + " must be a non-negative integer"}
			}
			if arg == "-maxdepth" {
				q.maxDepth = n
			} else {
				q.minDepth = n
			}
			match = func(findEntry) bool { return true }
		case "-mtime":
			n, err := strconv.Atoi(val)
			if err != nil {
				return &usageError{c, "-mtime must be an integer with an optional + or -"}
			}
			q.needTimes = true
			match = func(e findEntry) bool {
				t, ok := q.updated[e.path]
				if !ok {
					return false
				}
				days := int(q.now.Sub(t) / (24 * time.Hour))
				switch val[0] {
				case '+':
					return days > n
				case '-':
					return days < -n
				}
				return days == n
			}
		case "-newer":
			ref, err := cleanPaths([]string{val})
			if err != nil {
				return err
			}
			q.newer = append(q.newer, ref[0])
			q.needTimes = true
			match = func(e findEntry) bool {
				t, ok := q.updated[e.path]
				return ok && t.After(q.updated[ref[0]])
			}
		case "-meta":
			key, want, ok := strings.Cut(val, "=")
			if !ok || key == "" {
				return &usageError{c, "-meta must be KEY=VALUE"}
			}
			q.needPages = true
			match = func(e findEntry) bool { return !e.dir && metaHas(q.frontmatter[e.path][key], want) }
		}
		q.tests = append(q.tests, findTest{not, match})
		not = false
	}
	if not {
		return &usageError{c, "missing primary after !"}
	}
	a.find = q
	return nil
}

// metaHas reports whether a frontmatter value is want, or is a list with an
// element that is.
func metaHas(v any, want string) bool {
	if list, ok := v.([]any); ok {
		return slices.ContainsFunc(list, func(e any) bool { return e != nil && fmt.Sprint(e) == want })
	}
	return v != nil && fmt.Sprint(v) == want
}

// globRegexp compiles a shell pattern as find matches it: * and ? match any
// characters, "/" included, [...] is a class, negated by ! or ^, and a
// backslash quotes the next character.
func globRegexp(pattern string) (*regexp.Regexp, error) {
	rs := []rune(pattern)
	var b strings.Builder
	b.WriteString(`^(?s:`)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '\\':
			if i+1 < len(rs) {
				i++
			}
			b.WriteString(regexp.QuoteMeta(string(rs[i])))
		case '[':
			j := i + 1
			if j < len(rs) && (rs[j] == '!' || rs[j] == '^') {
				j++
			}
			if j < len(rs) && rs[j] == ']' {
				j++
			}
			for j < len(rs) && rs[j] != ']' {
				j++
			}
			if j == len(rs) {
				b.WriteString(`\[`)
				continue
			}
			class := string(rs[i+1 : j])
			if class[0] == '!' {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(`)$`)
	return regexp.Compile(b.String())
}

func (a *app) cmdFind(c *command, args []string) error {
	q := a.find
	t, err := a.readTree(false)
	if err != nil {
		return err
	}
	exists := func(p string) bool { return t.isDir(p) || t.isFile(p) }
	for _, ref := range q.newer {
		if !exists(ref) {
			fmt.Fprintf(a.stderr, "wikictl: %s: no such file or directory\n", escapeControl(ref))
			return exitStatus(ExitError)
		}
	}
	paths := q.paths
	if len(paths) == 0 {
		paths = []string{"."}
	}
	var entries []findEntry
	var missing, files []string
	var walk func(p string, depth int)
	walk = func(p string, depth int) {
		dir := t.isDir(p)
		if depth >= q.minDepth {
			entries = append(entries, findEntry{p, dir})
			if !dir {
				files = append(files, p)
			}
		}
		if dir && (q.maxDepth < 0 || depth < q.maxDepth) {
			for _, e := range t.entries(p, true) {
				walk(e, depth+1)
			}
		}
	}
	for _, p := range paths {
		if !exists(p) {
			missing = append(missing, p)
			continue
		}
		walk(p, 0)
	}
	if q.needTimes {
		if q.updated, err = a.latestUpdates(); err != nil {
			return err
		}
		q.now = time.Now()
	}
	if q.needPages {
		pages, err := a.readPages(files)
		if err != nil {
			return &gitError{err}
		}
		q.frontmatter = map[string]map[string]any{}
		for _, p := range files {
			q.frontmatter[p] = pages.parse(p).Frontmatter
		}
	}
	items := []treeItem{}
	for _, e := range entries {
		if !slices.ContainsFunc(q.tests, func(ft findTest) bool { return ft.match(e) == ft.not }) {
			kind := "file"
			if e.dir {
				kind = "dir"
			}
			items = append(items, treeItem{e.path, kind})
		}
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			fmt.Fprintln(w, escapeControl(it.Path))
		}
	})
	for _, p := range missing {
		fmt.Fprintf(a.stderr, "wikictl: %s: no such file or directory\n", escapeControl(p))
	}
	if len(missing) > 0 {
		return exitStatus(ExitError)
	}
	return nil
}
