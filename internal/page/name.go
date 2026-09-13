package page

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
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

// CheckPath returns nil when p can be a page path: at least one directory,
// every component passing CheckName, and a .md suffix.
func CheckPath(p string) error {
	if !strings.HasSuffix(p, ".md") {
		return errors.New("path must end in .md")
	}
	parts := strings.Split(strings.TrimSuffix(p, ".md"), "/")
	if len(parts) < 2 {
		return errors.New("page must be in a directory, not at the wiki root")
	}
	for _, x := range parts {
		if err := CheckName(x); err != nil {
			return err
		}
	}
	return nil
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
			return []Issue{{Path: p, Code: "name_style", Message: fmt.Sprintf("name %q: lowercase ASCII letters, digits and hyphens are recommended", x)}}
		}
	}
	return nil
}

// CaseCollisions reports, as case_collision issues, every path in paths
// that shares a directory with a file or directory whose name differs only
// by case. Such names collide on case-insensitive file systems.
func CaseCollisions(paths []string) []Issue {
	spellings := map[string]map[string]bool{} // lowercased prefix -> prefixes as written
	for _, p := range paths {
		for _, q := range prefixes(p) {
			k := strings.ToLower(q)
			if spellings[k] == nil {
				spellings[k] = map[string]bool{}
			}
			spellings[k][q] = true
		}
	}
	var out []Issue
	for _, p := range paths {
		for _, q := range prefixes(p) {
			others := spellings[strings.ToLower(q)]
			if len(others) < 2 {
				continue
			}
			var names []string
			for o := range others {
				if o != q {
					names = append(names, fmt.Sprintf("%q", o))
				}
			}
			sort.Strings(names)
			out = append(out, Issue{Path: p, Code: "case_collision", Message: fmt.Sprintf("%q differs only by case from %s", q, strings.Join(names, ", "))})
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
