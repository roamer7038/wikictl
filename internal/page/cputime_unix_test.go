//go:build unix

package page

import (
	"syscall"
	"time"
)

// cpuTime returns the processor time used by this process. Unlike the wall
// clock, it does not count the time taken by other processes, such as the
// tests of other packages that go test runs at the same time.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return time.Duration(time.Now().UnixNano())
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
