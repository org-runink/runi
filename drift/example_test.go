// SPDX-License-Identifier: BSD-3-Clause

package drift_test

import (
	"fmt"
	"time"

	"github.com/org-runink/runi/drift"
)

// A latency series around 20 ms with a spread of about 2 ms steps up by 3 ms
// at sample 13. The CUSUM, tuned with the textbook defaults, flags it a few
// samples later and estimates where it started.
//
// S⁺ = max(0, S⁺ + x − 21) is 0 through sample 12 and then runs 3, 4, 6, 10,
// 10, 13: over h on sample 18, and positive since sample 13.
func Example() {
	latency := []float64{
		21, 19, 22, 18, 20, 21, 17, 20, 23, 19, 20, 18, // in control
		24, 22, 23, 25, 21, 24, 23, 22, 24, 25, // +3 ms from sample 13
	}
	c, err := drift.DefaultCUSUM(20, 2) // k = 1, h = 10
	if err != nil {
		panic(err)
	}
	for i, x := range latency {
		if s := c.Add(x); s != drift.None {
			fmt.Printf("sample %d: shift %s, began at sample %d\n", i+1, s, c.N()-c.Since()+1)
			fmt.Printf("upper sum %.0f over h = 10\n", c.Upper())
			break
		}
	}
	// Output:
	// sample 18: shift up, began at sample 13
	// upper sum 13 over h = 10
}

// A counter of microseconds stalled, read at irregular times, turned into the
// share of time stalled over roughly the last 10 seconds. The stall share
// jumps from 10% to 50% at 3 s; the average moves towards it in proportion to
// the time each reading covers, not the number of readings, and loses only
// the interval in which the counter restarted.
func ExampleDecayed() {
	d, err := drift.NewDecayed(10 * time.Second)
	if err != nil {
		panic(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	readings := []struct {
		after time.Duration
		total uint64 // µs stalled since boot
	}{
		{0, 5_000_000},
		{time.Second, 5_100_000},             // 10% over 1 s
		{3 * time.Second, 5_300_000},         // 10% over 2 s
		{3 * time.Second, 5_300_000},         // a duplicate: skipped
		{3500 * time.Millisecond, 5_550_000}, // 50% over 0.5 s
		{7 * time.Second, 7_300_000},         // 50% over 3.5 s
		{8 * time.Second, 40},                // the counter restarted
		{10 * time.Second, 1_000_040},        // 50% over 2 s
	}
	for _, r := range readings {
		rate, ok := d.Update(r.total, start.Add(r.after))
		if !ok {
			fmt.Printf("t=%v: no rate\n", r.after)
			continue
		}
		fmt.Printf("t=%v: %.1f%% stalled\n", r.after, rate/1e6*100)
	}
	fmt.Println("skipped", d.Skipped(), "resets", d.Resets())
	// Output:
	// t=0s: no rate
	// t=1s: 10.0% stalled
	// t=3s: 10.0% stalled
	// t=3s: no rate
	// t=3.5s: 12.0% stalled
	// t=7s: 23.2% stalled
	// t=8s: no rate
	// t=10s: 28.0% stalled
	// skipped 1 resets 1
}
