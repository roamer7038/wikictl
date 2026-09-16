//go:build unix

package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// runEdit runs "edit <p>" with a pipe as standard error, as a terminal gives
// one, and returns the exit code and what was written there. A file is used
// rather than a buffer in memory because the editor and wikictl write to
// standard error at the same time, as they do when a signal arrives.
func runEdit(t *testing.T, cfg, p string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var errs bytes.Buffer
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		io.Copy(&errs, r)
	}()
	code := Main([]string{"--config", cfg, "edit", p}, strings.NewReader(""), io.Discard, w)
	w.Close()
	<-copied
	r.Close()
	return code, errs.String()
}

// TestEditSignalKeepsFile checks that a signal that ends wikictl while the
// editor runs, SIGTERM or SIGHUP, prints where the edited file is kept instead
// of leaving it silently. The exit is replaced, so that the test process
// survives; the editor is ended from there, as the end of wikictl would leave
// it with nothing to write to.
func TestEditSignalKeepsFile(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			cfg := setup(t)
			pid := filepath.Join(t.TempDir(), "editor.pid")
			editWith(t, "printf '%s\\n' \"$$\" > "+shQuote(pid)+"\n"+
				"printf draft > \"$1\"\n"+
				"kill -"+strconv.Itoa(int(sig))+" "+strconv.Itoa(os.Getpid())+"\n"+
				// exec, so that the pid written above is the one waiting: a
				// child of the editor would hold the output pipes open.
				"exec sleep 30\n")
			got := make(chan os.Signal, 1)
			old := exitOnSignal
			exitOnSignal = func(s os.Signal) {
				got <- s
				// The editor is still sleeping; wikictl would be gone by now.
				if b, err := os.ReadFile(pid); err == nil {
					if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
						syscall.Kill(n, syscall.SIGKILL)
					}
				}
			}
			t.Cleanup(func() { exitOnSignal = old })

			code, errs := runEdit(t, cfg, "global/push.md")
			select {
			case s := <-got:
				if s != sig {
					t.Errorf("signal %v, want %v", s, sig)
				}
			default:
				t.Fatalf("%v did not end the command: code=%d errs=%q", sig, code, errs)
			}
			if !strings.Contains(errs, "the edited file is kept in ") {
				t.Fatalf("%v: errs=%q", sig, errs)
			}
			if kept := keptFile(t, errs); kept != "draft" {
				t.Errorf("%v: kept file %q", sig, kept)
			}
		})
	}
}

// TestExitOnSignal checks the code wikictl ends with, the one a shell reports
// for a process killed by the signal.
func TestExitOnSignal(t *testing.T) {
	if got, want := signalExitCode(syscall.SIGTERM), 128+int(syscall.SIGTERM); got != want {
		t.Errorf("SIGTERM: %d, want %d", got, want)
	}
}
