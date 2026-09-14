package repo

import (
	"regexp"
	"strings"
)

// redacted replaces the userinfo of a URL in output.
const redacted = "***"

// RedactURL returns s with the userinfo of a repository URL (the "user:token"
// before "@") replaced by "***", so that a password or token is not shown.
// For https and other schemes any userinfo is replaced, since a token may be
// given as the user name alone. For ssh:// and the scp-like form
// [user@]host:path, the user name alone (such as "git") is not a credential
// and is kept; only a userinfo with a password is replaced. Strings that are
// not such URLs, such as local paths, are returned unchanged.
func RedactURL(s string) string {
	if i := strings.Index(s, "://"); i > 0 && isScheme(s[:i]) {
		rest := s[i+3:]
		end := strings.IndexAny(rest, "/?#")
		if end < 0 {
			end = len(rest)
		}
		at := strings.LastIndex(rest[:end], "@")
		if at < 0 {
			return s
		}
		if isSSH(s[:i]) && !strings.Contains(rest[:at], ":") {
			return s
		}
		return s[:i+3] + redacted + rest[at:]
	}
	end := strings.IndexByte(s, '/')
	if end < 0 {
		end = len(s)
	}
	at := strings.LastIndex(s[:end], "@")
	if at <= 0 || !strings.Contains(s[:at], ":") || !strings.Contains(s[at:end], ":") {
		return s
	}
	return redacted + s[at:]
}

func isScheme(s string) bool {
	for i, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.')) {
			return false
		}
	}
	return true
}

func isSSH(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "ssh", "git+ssh", "ssh+git":
		return true
	}
	return false
}

// schemeURL matches URLs with a scheme inside free text such as git's stderr.
var schemeURL = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s'"]+`)

// redactText applies RedactURL to every URL with a scheme in s.
func redactText(s string) string {
	return schemeURL.ReplaceAllStringFunc(s, RedactURL)
}
