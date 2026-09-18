package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/page"
	"github.com/roamer7038/wikictl/internal/wiki"
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

// findItem is one entry of the output of find. The frontmatter fields are
// filled in only with --frontmatter, and a file without frontmatter keeps
// them empty: the list is a pointer so that a frontmatter holding no key is
// printed as [] while a file that has none has no field at all.
type findItem struct {
	Path             string                 `json:"path"`
	Kind             string                 `json:"kind"`
	Frontmatter      *[]page.FrontmatterKey `json:"frontmatter,omitempty"`
	FrontmatterError *frontmatterError      `json:"frontmatter_error,omitempty"`
}

// frontmatterError is a frontmatter that find could not read, with the code
// and the message that lint reports for it.
type frontmatterError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// allFrontmatterKeys is the value pflag gives --frontmatter when it is written
// without "=". An argument cannot hold a NUL, so it cannot be mistaken for a
// list of keys.
const allFrontmatterKeys = "\x00"

// frontmatterKeys is the value of --frontmatter: whether it was given, and the
// keys to print, none of them standing for all.
type frontmatterKeys struct {
	on   bool
	keys []string
}

func (f *frontmatterKeys) String() string { return strings.Join(f.keys, ",") }

func (f *frontmatterKeys) Type() string { return "keys" }

// Set replaces the keys of an earlier --frontmatter, so that the last one
// written wins, as it does for a flag that is not a list.
func (f *frontmatterKeys) Set(s string) error {
	f.on = true
	if s == allFrontmatterKeys {
		f.keys = nil
		return nil
	}
	var keys []string
	for _, k := range strings.Split(s, ",") {
		if k == "" {
			return errors.New("a key must not be empty")
		}
		keys = append(keys, k)
	}
	f.keys = keys
	return nil
}

// wanted reports whether the key k is one of those to print.
func (f *frontmatterKeys) wanted(k string) bool {
	return len(f.keys) == 0 || slices.Contains(f.keys, k)
}

func findFlags(a *app, fs *pflag.FlagSet) {
	fs.Var(&a.fmKeys, "frontmatter", "print the frontmatter, or only the `keys` given")
	fs.Lookup("frontmatter").NoOptDefVal = allFrontmatterKeys
}

// jsonText returns v as the JSON that --frontmatter prints for a key or a
// value in text output, with the control characters escaped as the rest of the
// text output escapes them.
func jsonText(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "null"
	}
	return escapeControl(strings.TrimSuffix(b.String(), "\n"))
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

// findUsage returns a usage error of find with the way to write the expression
// under its message, when the message is one that has one.
func findUsage(c *command, msg string) *usageError {
	if hint := findHint(msg); hint != "" {
		msg += "\n" + hint
	}
	return &usageError{c, msg}
}

// findHint returns the way to write in the expression of find what msg
// rejected, or "" when there is none. It covers the syntax of GNU find that
// wikictl does not take.
func findHint(msg string) string {
	if primary, ok := strings.CutPrefix(msg, "unknown primary: "); ok {
		switch primary {
		case "-iname":
			return "-name matches the last element of the path; it is case-sensitive"
		case "-o", "-or":
			return "the primaries must all be true; there is no OR\n" +
				"run find once per pattern, or search the paths with grep"
		}
	}
	if flag, ok := strings.CutPrefix(msg, "unknown flag: --"); ok && findValue["-"+flag] {
		return "a primary takes one dash: write -" + flag + ", not --" + flag
	}
	return ""
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
			return findUsage(c, "unknown primary: "+arg)
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
				days := int(math.Floor(q.now.Sub(t).Hours() / 24))
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

// metaHas reports whether a frontmatter value is the scalar want, or is a
// list with an element that is.
func metaHas(v any, want string) bool {
	if list, ok := v.([]any); ok {
		return slices.ContainsFunc(list, func(e any) bool { return scalarIs(e, want) })
	}
	return scalarIs(v, want)
}

// scalarIs reports whether v is a scalar written as want; a number matches
// any number of equal value.
func scalarIs(v any, want string) bool {
	switch rv := reflect.ValueOf(v); {
	case rv.Kind() == reflect.Invalid || rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice:
		return false
	case rv.CanInt() || rv.CanUint() || rv.CanFloat():
		n, err := strconv.ParseFloat(want, 64)
		return err == nil && n == reflect.ValueOf(v).Convert(reflect.TypeFor[float64]()).Float()
	}
	return fmt.Sprint(v) == want
}

// globRegexp compiles a shell pattern as find matches it: * and ? match any
// characters, "/" included, [...] is a class, negated by ! or ^, that may
// contain names such as [:alpha:], and a backslash quotes the next character.
// A pattern that ends with an unquoted backslash matches nothing.
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
			if i+1 == len(rs) {
				return regexp.Compile(`[^\x00-\x{10FFFF}]`)
			}
			i++
			b.WriteString(regexp.QuoteMeta(string(rs[i])))
		case '[':
			class, n := globClass(rs[i+1:])
			if n == 0 {
				b.WriteString(`\[`)
				continue
			}
			b.WriteString(class)
			i += n
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(`)$`)
	return regexp.Compile(b.String())
}

// globClass translates the class at the start of rs, which follows its "[",
// into a regular expression, and returns it with the number of runes up to
// and including the closing "]", or 0 when the class is not closed.
func globClass(rs []rune) (string, int) {
	var b strings.Builder
	b.WriteString("[")
	i := 0
	if i < len(rs) && (rs[i] == '!' || rs[i] == '^') {
		b.WriteString("^")
		i++
	}
	start := i
	for ; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == ']' && i > start:
			return b.String() + "]", i + 1
		case r == '[' && i+1 < len(rs) && rs[i+1] == ':':
			if name, _, ok := strings.Cut(string(rs[i+2:]), ":]"); ok {
				b.WriteString("[:" + name + ":]")
				i += len([]rune(name)) + 3
				continue
			}
		case r == '\\' && i+1 < len(rs):
			i++
			r = rs[i]
		case r == '-':
			b.WriteString("-")
			continue
		}
		if strings.ContainsRune(`\]-^[`, r) {
			b.WriteString(`\`)
		}
		b.WriteRune(r)
	}
	return "", 0
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
			return a.reportMissing([]string{ref}, t.pathMessage)
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
		if q.updated, err = a.latestUpdates(t, slices.Concat(paths, q.newer)); err != nil {
			return err
		}
		q.now = time.Now()
	}
	var pages wiki.Pages
	if q.needPages {
		if pages, err = wiki.ReadPages(a.repo, files); err != nil {
			return &gitError{err}
		}
		q.frontmatter = map[string]map[string]any{}
		for _, p := range files {
			q.frontmatter[p] = pages.Parse(p).Frontmatter
		}
	}
	items := []findItem{}
	var matched []string
	for _, e := range entries {
		if !slices.ContainsFunc(q.tests, func(ft findTest) bool { return ft.match(e) == ft.not }) {
			kind := "file"
			if e.dir {
				kind = "dir"
			} else {
				matched = append(matched, e.path)
			}
			items = append(items, findItem{Path: e.path, Kind: kind})
		}
	}
	var warnings []page.Issue
	if a.fmKeys.on {
		// Without -meta nothing has been read yet, and only the files that
		// matched are needed.
		if !q.needPages {
			if pages, err = wiki.ReadPages(a.repo, matched); err != nil {
				return &gitError{err}
			}
		}
		for i := range items {
			if items[i].Kind != "file" {
				continue
			}
			keys, ok, iss := pages.Frontmatter(items[i].Path)
			switch {
			case iss != nil:
				items[i].FrontmatterError = &frontmatterError{iss.Code, iss.Message}
				warnings = append(warnings, *iss)
			case ok:
				kept := []page.FrontmatterKey{}
				for _, k := range keys {
					if a.fmKeys.wanted(k.Key) {
						kept = append(kept, k)
					}
				}
				items[i].Frontmatter = &kept
			}
		}
	}
	a.emit(map[string]any{"items": items}, func(w io.Writer) {
		for _, it := range items {
			if !a.fmKeys.on {
				fmt.Fprintln(w, escapeControl(it.Path))
				continue
			}
			if it.Frontmatter == nil {
				continue
			}
			for _, k := range *it.Frontmatter {
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", escapeControl(it.Path), k.Line, jsonText(k.Key), jsonText(k.Value))
			}
		}
	})
	// With --json the error is in the output of each item already.
	if !a.json {
		for _, is := range warnings {
			fmt.Fprintf(a.stderr, "wikictl: warning: %s:%d: %s: %s\n", escapeControl(is.Path), is.Line, is.Code, escapeControl(is.Message))
		}
	}
	return a.reportMissing(missing, t.pathMessage)
}
