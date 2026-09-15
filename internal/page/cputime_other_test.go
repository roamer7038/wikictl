//go:build !unix

package page

import "time"

// cpuTime returns the wall clock where the processor time of the process is
// not available.
func cpuTime() time.Duration {
	return time.Duration(time.Now().UnixNano())
}
