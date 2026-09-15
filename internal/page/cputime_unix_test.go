//go:build unix

package page

import (
	"syscall"
	"testing"
	"time"
)

// cpuTime returns the processor time used by this process. Unlike the wall
// clock, it does not count the time taken by other processes, such as the
// tests of other packages that go test runs at the same time.
func cpuTime(t testing.TB) time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
