//go:build unix

package cli

import "syscall"

// openNoFollow makes os.OpenFile fail instead of opening the target when the
// path is a symbolic link, and return instead of waiting for a writer when it
// is a named pipe; the check that the file is a regular file then rejects the
// pipe. Opening a regular file is not affected.
const openNoFollow = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
