package cli

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/spf13/pflag"

	"github.com/roamer7038/wikictl/internal/page"
)

// isTerminal reports whether r is a terminal. Tests replace it.
var isTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func editFlags(a *app, fs *pflag.FlagSet) {
	msgFlag(a, fs)
	fs.BoolVarP(&a.verbose, "verbose", "v", false, "print the path, blob sha and commit")
}

func (a *app) checkEdit(c *command, args []string) error {
	if !isTerminal(a.stdin) {
		return &usageError{c, "standard input is not a terminal"}
	}
	return nil
}

// cmdEdit opens the file in an editor and commits the result as put does,
// with the sha read before editing as the base. When the result cannot be
// committed, the edited file is kept and its path printed.
func (a *app) cmdEdit(c *command, args []string) error {
	p := args[0]
	check := page.CheckFilePath
	if strings.HasSuffix(p, ".md") {
		check = page.CheckPath
	}
	if err := check(p); err != nil {
		return &invalidError{"bad_path: " + err.Error()}
	}
	contents, shas, err := a.repo.CatSHA([]string{p})
	if err != nil {
		return &gitError{err}
	}
	if contents[p] == nil {
		if _, err := a.missing([]string{p}, func(string) bool { return false }); err != nil {
			return err
		}
		files, err := a.repo.Files([]string{p})
		if err != nil {
			return &gitError{err}
		}
		if len(files) > 0 {
			return fmt.Errorf("%s: is a directory", p)
		}
	}
	tmp, err := os.CreateTemp("", "wikictl-*"+path.Ext(p))
	if err != nil {
		return err
	}
	_, err = tmp.Write(contents[p])
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	keep := func(err error) error {
		fmt.Fprintf(a.stderr, "wikictl: the edited file is kept in %s\n", tmp.Name())
		return err
	}
	// As git does, the editor is run by the shell, so that it may include arguments.
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	run := exec.Command("sh", "-c", editor+` "$@"`, editor, tmp.Name())
	run.Stdin, run.Stdout, run.Stderr = a.stdin, a.stdout, a.stderr
	if err := run.Run(); err != nil {
		return keep(fmt.Errorf("editor %s: %w", editor, err))
	}
	edited, err := os.ReadFile(tmp.Name())
	if err != nil {
		return keep(err)
	}
	if !bytes.Equal(edited, contents[p]) {
		if err := a.writeFile(p, edited, shas[p], "edit"); err != nil {
			return keep(err)
		}
	}
	return os.Remove(tmp.Name())
}
