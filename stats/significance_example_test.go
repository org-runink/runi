// SPDX-License-Identifier: BSD-3-Clause

package stats_test

import (
	"fmt"

	"github.com/org-runink/runi/stats"
)

func ExampleTrend() {
	weekly := []float64{12, 14, 13, 15, 17, 16, 18, 20, 19, 21}
	t := stats.Trend(weekly)
	fmt.Printf("slope %.2f per week, rising: %v\n", t.Slope, t.Rising(stats.DefaultAlpha))
	// Output: slope 0.96 per week, rising: true
}

func ExampleBenjaminiHochberg() {
	p := []float64{0.001, 0.008, 0.039, 0.041, 0.042, 0.06}
	_, keep := stats.BenjaminiHochberg(p, 0.05)
	fmt.Println(keep)
	// Output: [true true false false false false]
}
