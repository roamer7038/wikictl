package cli

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// isTerminal reports whether r is a terminal. Tests replace it.
var isTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
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

// readEdited reads the file the editor was given without following a symbolic
// link, so that an editor which replaces it with a link does not make wikictl
// read and commit the content of the link's target. An editor that writes a new
// file and renames it into place, as vim does with backupcopy=no, is left
// working: the file read is required to be a regular file, not the one created.
func readEdited(name string) ([]byte, error) {
	if fi, err := os.Lstat(name); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: the editor replaced the file with a symbolic link", name)
	}
	f, err := os.OpenFile(name, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: the editor replaced the file with %s", name, fi.Mode().Type())
	}
	return io.ReadAll(f)
}

// cmdEdit opens the file in an editor and commits the result as put does,
// with the sha read before editing as the base. When the result cannot be
// committed, the edited file is kept and its path printed.
func (a *app) cmdEdit(c *command, args []string) error {
	p := args[0]
	if err := checkFilePath(p); err != nil {
		return a.badPath(p, err)
	}
	var dirs []string
	for d := path.Dir(p); d != "."; d = path.Dir(d) {
		dirs = append(dirs, d)
	}
	above, err := a.repo.Stat(dirs)
	if err != nil {
		return &gitError{err}
	}
	for _, d := range dirs {
		if _, ok := above[d]; ok {
			return fmt.Errorf("%s: %s is a file", p, d)
		}
	}
	contents, shas, err := a.repo.CatSHA([]string{p})
	if err != nil {
		return &gitError{err}
	}
	if contents[p] == nil {
		if err := a.repo.CheckMissing([]string{p}); err != nil {
			return &gitError{err}
		}
		files, err := a.repo.Files([]string{p})
		if err != nil {
			return &gitError{err}
		}
		if len(files) > 0 {
			return fmt.Errorf("%s: is a directory", p)
		}
	}
	tmp, err := os.CreateTemp("", "wikictl-*"+strings.ReplaceAll(path.Ext(p), "*", ""))
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
	// As git does, an editor with spaces or shell characters, which may include
	// arguments, is run by the shell; any other is run directly.
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	run := exec.Command(editor, tmp.Name())
	if strings.ContainsAny(editor, "|&;<>()$`\\\"' \t\n*?[#~=%") {
		run = exec.Command("sh", "-c", editor+` "$@"`, editor, tmp.Name())
	}
	run.Stdin, run.Stdout, run.Stderr = a.stdin, a.stdout, a.stderr
	// As git does, interrupts are left to the editor while it runs: wikictl
	// receives and drops them, and the editor still gets them.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGQUIT)
	err = run.Run()
	signal.Stop(sig)
	if err != nil {
		return keep(fmt.Errorf("editor %s: %w", editor, err))
	}
	edited, err := readEdited(tmp.Name())
	if err != nil {
		return keep(err)
	}
	if !bytes.Equal(edited, contents[p]) {
		if err := a.writeFile(p, edited, shas[p], "edit"); err != nil {
			return keep(err)
		}
	}
	os.Remove(tmp.Name())
	return nil
}
