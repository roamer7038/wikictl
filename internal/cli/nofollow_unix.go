//go:build unix

package cli

import "syscall"

// openNoFollow makes os.OpenFile fail instead of opening the target when the
// path is a symbolic link.
const openNoFollow = syscall.O_NOFOLLOW
