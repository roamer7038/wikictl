//go:build !unix

package cli

// openNoFollow is 0 where the flag does not exist; readEdited then relies on
// its own check that the path is not a symbolic link.
const openNoFollow = 0
