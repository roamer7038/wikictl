package page

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var reRecommended = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// breaking lists the characters that a name must not contain: '"' and '\'
// make git quote the path in its listings, '#' starts a fragment, '?' starts
// a query, ':' makes a relative link look like a URL scheme, '(' and ')'
// end or unbalance the destination of a Markdown link, and '`' starts a code
// span that hides a link from the link scan.
const breaking = "\"\\#?:()`"

// CheckName returns nil when s can be a file or directory name of a page
// path, or an error naming the rule it breaks. Only names that break links
// or the git-based scanning are rejected: empty names, names starting with
// a dot or '<', and names containing whitespace, control characters or one
// of the characters " \ # ? : ( ) `.
func CheckName(s string) error {
	if s == "" {
		return errors.New("empty name")
	}
	if strings.HasPrefix(s, ".") {
		return fmt.Errorf("name %q starts with a dot", s)
	}
	if strings.HasPrefix(s, "<") {
		return fmt.Errorf("name %q starts with '<', which breaks links", s)
	}
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			return fmt.Errorf("name %q contains whitespace", s)
		case unicode.IsControl(r):
			return fmt.Errorf("name %q contains a control character", s)
		case strings.ContainsRune(breaking, r):
			return fmt.Errorf("name %q contains %q, which breaks links", s, r)
		}
	}
	return nil
}

// MaxNameLen is the limit, in bytes, of one file or directory name. The
// usual file systems refuse a longer name, so a clone of a wiki holding one
// fails with "File name too long". CheckName does not apply the limit: a
// name already in the wiki must stay removable with rm and movable away with
// mv, so only the paths that a command writes are checked, through CheckPath,
// CheckFilePath and CheckLength.
const MaxNameLen = 255

// CheckLength returns an error when a component of p, which may be a single
// name, is over MaxNameLen bytes.
func CheckLength(p string) error {
	for _, x := range strings.Split(p, "/") {
		if len(x) > MaxNameLen {
			return fmt.Errorf("name %q is longer than %d bytes", x, MaxNameLen)
		}
	}
	return nil
}

// IsPagePath reports whether p names a page, the only kind of file whose
// content is interpreted: a name ending in .md with no component starting with
// a dot, at the wiki root or in a directory. Every command applies this
// definition; whether such a path may be written is decided by CheckPath.
func IsPagePath(p string) bool {
	if !strings.HasSuffix(p, ".md") {
		return false
	}
	for _, x := range strings.Split(p, "/") {
		if strings.HasPrefix(x, ".") {
			return false
		}
	}
	return true
}

// CheckPath returns nil when p can be a page path: a .md suffix and, without
// it, a path that CheckFilePath accepts. The length of a name is checked with
// the suffix, which is part of the file name.
func CheckPath(p string) error {
	if !strings.HasSuffix(p, ".md") {
		return errors.New("path must end in .md")
	}
	if err := CheckFilePath(strings.TrimSuffix(p, ".md")); err != nil {
		return err
	}
	return CheckLength(p)
}

// CheckFilePath returns nil when p can be the path of a file: every component
// passing CheckName and no component over MaxNameLen bytes. A file at the wiki
// root is such a path of one component; the root itself is not, since CheckName
// rejects both "." and the empty name.
func CheckFilePath(p string) error {
	for _, x := range strings.Split(p, "/") {
		if err := CheckName(x); err != nil {
			return err
		}
	}
	return CheckLength(p)
}

// Recommended reports whether s has the recommended form of a name:
// lowercase ASCII letters, digits and hyphens, not starting with a hyphen.
// Other names work but are reported by lint as name_style.
func Recommended(s string) bool { return reRecommended.MatchString(s) }

// PathIssues returns the issues of page path p: bad_path when CheckPath
// rejects it, otherwise name_style for the first component that is not
// Recommended.
func PathIssues(p string) []Issue {
	if err := CheckPath(p); err != nil {
		return []Issue{{Path: p, Code: "bad_path", Message: err.Error()}}
	}
	for _, x := range strings.Split(strings.TrimSuffix(p, ".md"), "/") {
		if !Recommended(x) {
			return []Issue{NameStyle(p, x)}
		}
	}
	return nil
}

// NameStyle returns the name_style issue of p for its component name, which
// is not Recommended.
func NameStyle(p, name string) Issue {
	return Issue{Path: p, Code: "name_style", Message: fmt.Sprintf("name %q: lowercase ASCII letters, digits and hyphens are recommended", name)}
}

// CaseCollisions reports, as case_collision issues, every path in paths
// that shares a directory with a file or directory whose name differs only
// by case. Such names collide on case-insensitive file systems.
func CaseCollisions(paths []string) []Issue {
	return collisions(paths, strings.ToLower, nil, "case_collision", "differs only by case from")
}

// UnicodeCollisions reports, as unicode_collision issues, every path in paths
// that shares a directory with a file or directory whose name differs only by
// Unicode normalisation, such as a name written in NFC and one written in
// NFD, or by normalisation and case at once. Such names are the same name on
// a file system that normalises them, as macOS does, so a clone there keeps
// only one of them. A pair that differs by case alone is left to
// CaseCollisions, whose message says what to change; lowercasing does not
// make these names equal, so no case_collision covers them.
func UnicodeCollisions(paths []string) []Issue {
	folded := func(s string) string { return strings.ToLower(norm.NFC.String(s)) }
	// The pairs the other two checks report are left to them: those that share
	// the key of CaseCollisions (case alone) and those that share their NFC
	// form (normalisation alone).
	notCase, notNorm := differsUnder(strings.ToLower), differsUnder(norm.NFC.String)
	out := collisions(paths, norm.NFC.String, nil, "unicode_collision", "differs only by Unicode normalisation from")
	out = append(out, collisions(paths, folded, func(q, o string) bool { return notCase(q, o) && notNorm(q, o) },
		"unicode_collision", "differs only by Unicode normalisation and case from")...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// differsUnder returns a keep predicate for collisions that accepts the pairs
// a check keyed by key does not already report. The key itself is used, so
// that the two checks agree exactly. strings.EqualFold, which staticcheck
// suggests in place of comparing two strings.ToLower calls, is not the same
// test: it folds "İ" (U+0130) apart from "i", although the two differ only by
// case, and CaseCollisions reports that pair already.
func differsUnder(key func(string) string) func(q, o string) bool {
	return func(q, o string) bool { return key(q) != key(o) }
}

// collisions reports, with code and phrase, every path in paths that shares a
// directory with a file or directory whose name has the same key but is
// written differently. Each path is reported for its first colliding prefix.
// When keep is set, only the other spellings it accepts are collisions, so
// that a code covers the pairs another code does not.
func collisions(paths []string, key func(string) string, keep func(q, o string) bool, code, phrase string) []Issue {
	spellings := map[string]map[string]bool{} // key of a prefix -> prefixes as written
	for _, p := range paths {
		for _, q := range prefixes(p) {
			k := key(q)
			if spellings[k] == nil {
				spellings[k] = map[string]bool{}
			}
			spellings[k][q] = true
		}
	}
	var out []Issue
	for _, p := range paths {
		for _, q := range prefixes(p) {
			others := spellings[key(q)]
			if len(others) < 2 {
				continue
			}
			var names []string
			for o := range others {
				if o != q && (keep == nil || keep(q, o)) {
					names = append(names, fmt.Sprintf("%q", o))
				}
			}
			if len(names) == 0 {
				continue
			}
			sort.Strings(names)
			out = append(out, Issue{Path: p, Code: code, Message: fmt.Sprintf("%q %s %s", q, phrase, strings.Join(names, ", "))})
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// prefixes returns every directory prefix of p followed by p itself.
func prefixes(p string) []string {
	var out []string
	for i, r := range p {
		if r == '/' {
			out = append(out, p[:i])
		}
	}
	return append(out, p)
}
