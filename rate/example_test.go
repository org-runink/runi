// SPDX-License-Identifier: BSD-3-Clause

package rate_test

import (
	"fmt"
	"math"
	"time"

	"github.com/org-runink/runi/rate"
)

// A 32-bit octet counter read once a second. It wraps past 4294967295 between
// the second and third readings, which still yields the true increase; then
// the device restarts and the counter falls by a little, which is a reset:
// that interval yields no rate rather than one of nearly 4 GB/s.
func Example() {
	c, err := rate.NewCounter(32)
	if err != nil {
		panic(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	readings := []struct {
		after time.Duration
		total uint64
	}{
		{0, 4_294_966_000},
		{time.Second, 4_294_967_000},             // +1000
		{2 * time.Second, 704},                   // wrapped: 296 to the top, then 704
		{2 * time.Second, 900},                   // same time: skipped
		{3 * time.Second, 600},                   // fell by 104: a restart
		{3500 * time.Millisecond, 1_100},         // +500 over 0.5 s
		{4500 * time.Millisecond, 5_000_000_000}, // above 2^32 − 1: not from this counter
	}
	for _, r := range readings {
		got, ok := c.Update(r.total, start.Add(r.after))
		if !ok {
			fmt.Printf("t=%v: no rate\n", r.after)
			continue
		}
		fmt.Printf("t=%v: +%d over %v = %.0f B/s\n", r.after, got.Delta, got.Interval, got.PerSecond)
	}
	fmt.Println("wraps", c.Wraps(), "resets", c.Resets(), "skipped", c.Skipped(), "invalid", c.Invalid())
	// Output:
	// t=0s: no rate
	// t=1s: +1000 over 1s = 1000 B/s
	// t=2s: +1000 over 1s = 1000 B/s
	// t=2s: no rate
	// t=3s: no rate
	// t=3.5s: +500 over 500ms = 1000 B/s
	// t=4.5s: no rate
	// wraps 1 resets 1 skipped 1 invalid 1
}

// The lowest and highest of the last three rates, updated as each arrives.
// The NaN, a missed reading, is skipped and takes no slot.
func ExampleWindow() {
	w, err := rate.NewWindow(3)
	if err != nil {
		panic(err)
	}
	for _, r := range []float64{120, 80, 95, 300, math.NaN(), 90, 110, 105} {
		w.Add(r)
		fmt.Printf("%5.0f  min %3.0f  max %3.0f\n", r, w.Min(), w.Max())
	}
	fmt.Println("skipped", w.Skipped())
	// Output:
	//   120  min 120  max 120
	//    80  min  80  max 120
	//    95  min  80  max 120
	//   300  min  80  max 300
	//   NaN  min  80  max 300
	//    90  min  90  max 300
	//   110  min  90  max 300
	//   105  min  90  max 110
	// skipped 1
}
