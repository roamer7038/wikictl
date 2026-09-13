//go:build !unix

package repo

import (
	"errors"
	"runtime"
)

// lockFile is not implemented on this platform, so creating a mirror and
// writing pages fail with an explicit error.
func lockFile(path string) (func(), error) {
	return nil, errors.New("file locking is not supported on " + runtime.GOOS + "; wikictl requires a Unix-like OS")
}
