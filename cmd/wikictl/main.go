// Command wikictl reads and writes a Markdown wiki on a Git host using only git.
package main

import (
	"os"

	"github.com/roamer7038/wikictl/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
