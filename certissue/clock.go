// SPDX-License-Identifier: BSD-3-Clause

package certissue

import "time"

// A Clock is the only source of time in this package. Replace it to test
// expiry and renewal without waiting; leave it nil for the system clock.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// systemClock is the default Clock and the one place the package reads the
// wall clock.
type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func (systemClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}
