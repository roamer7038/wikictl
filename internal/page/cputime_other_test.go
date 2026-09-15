//go:build !unix

package page

import (
	"testing"
	"time"
)

// cpuTime returns the wall clock where the processor time of the process is
// not available.
func cpuTime(testing.TB) time.Duration {
	return time.Duration(time.Now().UnixNano())
}
