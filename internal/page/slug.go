package page

import (
	"regexp"
	"strings"
)

var reSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidSlug reports whether s is a valid slug or directory name:
// lowercase letters, digits and hyphens, not starting with a hyphen.
func ValidSlug(s string) bool { return reSlug.MatchString(s) }

// ValidPagePath reports whether p is a valid page path: at least one
// directory, every component a valid slug, and a .md suffix.
func ValidPagePath(p string) bool {
	if !strings.HasSuffix(p, ".md") {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(p, ".md"), "/")
	if len(parts) < 2 {
		return false
	}
	for _, x := range parts {
		if !ValidSlug(x) {
			return false
		}
	}
	return true
}
