// SPDX-License-Identifier: BSD-3-Clause

package sustain_test

import (
	"fmt"
	"math"
	"time"

	"github.com/org-runink/runi/sustain"
)

var start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// A temperature read every 10 s, noisy around 89–91. A single threshold at
// 90 would change its answer on most readings; a Hysteresis that turns on at
// 90 and off only at 85 changes once on the way up and once on the way down.
func Example() {
	plain := false
	h, err := sustain.NewHysteresis(sustain.High, 90, 85, 0)
	if err != nil {
		panic(err)
	}
	flips := 0
	temps := []float64{86, 88, 89.5, 90.2, 89.8, 90.4, 89.6, 90.1, 89.7, 87, 84.5, 86}
	for i, x := range temps {
		at := start.Add(time.Duration(i) * 10 * time.Second)
		if (x >= 90) != plain {
			plain, flips = !plain, flips+1
		}
		was := h.Active()
		if on := h.Update(x, at); on != was {
			fmt.Printf("%v  %.1f  hot=%v\n", at.Format("15:04:05"), x, on)
		}
	}
	fmt.Println("single threshold changed", flips, "times; hysteresis", h.Transitions())
	// Output:
	// 00:00:30  90.2  hot=true
	// 00:01:40  84.5  hot=false
	// single threshold changed 6 times; hysteresis 2
}

// A Low alarm with a minimum dwell: free memory below 10% turns it on, above
// 15% off, and once it changes it stays at least 30 s. The recovery at 20 s
// is held back; the one at 40 s is past the dwell and clears it.
func ExampleHysteresis() {
	h, err := sustain.NewHysteresis(sustain.Low, 10, 15, 30*time.Second)
	if err != nil {
		panic(err)
	}
	for i, free := range []float64{12, 8, 16, 14, 17} {
		at := start.Add(time.Duration(i) * 10 * time.Second)
		fmt.Printf("t=%2ds free=%2.0f%%  low=%v\n", i*10, free, h.Update(free, at))
	}
	fmt.Println("held", h.Held())
	// Output:
	// t= 0s free=12%  low=false
	// t=10s free= 8%  low=true
	// t=20s free=16%  low=true
	// t=30s free=14%  low=true
	// t=40s free=17%  low=false
	// held 1
}

// A health check that must fail for 30 s before the state turns unhealthy,
// and succeed on three consecutive checks before it turns healthy again. The
// single failure at 10 s is ignored.
func ExampleDebounce() {
	d, err := sustain.NewDebounce(sustain.Delay{For: 30 * time.Second}, sustain.Delay{Readings: 3})
	if err != nil {
		panic(err)
	}
	failing := []bool{false, true, false, true, true, true, true, false, true, false, false, false}
	for i, f := range failing {
		at := start.Add(time.Duration(i) * 10 * time.Second)
		was := d.Active()
		if now := d.Update(f, at); now != was {
			fmt.Printf("t=%3ds unhealthy=%v\n", i*10, now)
		}
	}
	end := start.Add(time.Duration(len(failing)-1) * 10 * time.Second)
	fmt.Println("healthy for", d.InState(end))
	// Output:
	// t= 60s unhealthy=true
	// t=110s unhealthy=false
	// healthy for 0s
}

// Escalate when 3 of the last 5 latency readings reach 250 ms. A dip under
// the level between them does not reset the count, as it would a Debounce.
// The NaN, a missed reading, takes no slot.
func ExampleNofM() {
	w, err := sustain.NewNofM(sustain.High, 250, 3, 5)
	if err != nil {
		panic(err)
	}
	fmt.Printf("escalate on %d of the last %d\n", w.Need(), w.Size())
	for i, ms := range []float64{120, 260, 140, 300, math.NaN(), 180, 255, 130, 110, 100} {
		at := start.Add(time.Duration(i) * time.Second)
		on := w.Update(ms, at)
		fmt.Printf("%4.0f  %d/%d  escalated=%v\n", ms, w.Count(), w.Len(), on)
	}
	// Output:
	// escalate on 3 of the last 5
	//  120  0/1  escalated=false
	//  260  1/2  escalated=false
	//  140  1/3  escalated=false
	//  300  2/4  escalated=false
	//  NaN  2/4  escalated=false
	//  180  2/5  escalated=false
	//  255  3/5  escalated=true
	//  130  2/5  escalated=false
	//  110  2/5  escalated=false
	//  100  1/5  escalated=false
}

// A target replica count that jumps from 4 to 20 and back is turned into a
// ramp of at most 2 per second up and 4 per second down. Readings come at
// irregular times; the bound follows the time between them.
func ExampleSlew() {
	s, err := sustain.NewSlew(2, 4)
	if err != nil {
		panic(err)
	}
	for _, r := range []struct {
		after  time.Duration
		target float64
	}{
		{0, 4}, {time.Second, 20}, {3 * time.Second, 20}, {3500 * time.Millisecond, 20},
		{10 * time.Second, 20}, {11 * time.Second, 4}, {12 * time.Second, 4}, {20 * time.Second, 4},
	} {
		out := s.Update(r.target, start.Add(r.after))
		fmt.Printf("t=%-4v target %2.0f  output %4.1f  limited=%v\n", r.after, r.target, out, s.Limited())
	}
	// Output:
	// t=0s   target  4  output  4.0  limited=false
	// t=1s   target 20  output  6.0  limited=true
	// t=3s   target 20  output 10.0  limited=true
	// t=3.5s target 20  output 11.0  limited=true
	// t=10s  target 20  output 20.0  limited=false
	// t=11s  target  4  output 16.0  limited=true
	// t=12s  target  4  output 12.0  limited=true
	// t=20s  target  4  output  4.0  limited=false
}

// CPU utilisation classified as normal, warning or critical, with a 5-point
// deadband on each edge: warning from 70 until back to 65, critical from 90
// until back to 85. Readings near either edge do not flip the level.
func ExampleBand() {
	b, err := sustain.NewBand(0, sustain.Edge{Up: 70, Down: 65}, sustain.Edge{Up: 90, Down: 85})
	if err != nil {
		panic(err)
	}
	names := []string{"normal", "warning", "critical"}
	for i, cpu := range []float64{60, 71, 68, 69, 92, 88, 89, 84, 66, 64} {
		at := start.Add(time.Duration(i) * time.Minute)
		fmt.Printf("%3.0f%%  %s\n", cpu, names[b.Update(cpu, at)])
	}
	// Output:
	//  60%  normal
	//  71%  warning
	//  68%  warning
	//  69%  warning
	//  92%  critical
	//  88%  critical
	//  89%  critical
	//  84%  warning
	//  66%  warning
	//  64%  normal
}
