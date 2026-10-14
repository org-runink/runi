// SPDX-License-Identifier: BSD-3-Clause

package stats_test

import (
	"fmt"
	"math"

	"github.com/org-runink/runi/stats"
)

func Example() {
	latencyMS := []float64{120, 132, 118, math.NaN(), 125, 410, 128, 122, 131, 119}

	// Look before modelling: Describe says how many NaN it dropped instead of hiding them.
	s := stats.Describe(latencyMS)
	fmt.Printf("n=%d nan=%d mean=%.1f median=%.1f max=%.0f\n", s.N, s.NaN, s.Mean, s.Median, s.Max)

	// The median ignored the 410 that dragged the mean, so fence on quartiles too.
	fence := s.Q3 + 1.5*(s.Q3-s.Q1)
	for _, v := range latencyMS {
		if v > fence {
			fmt.Printf("outlier: %.0f (fence %.1f)\n", v, fence)
		}
	}
	// Output:
	// n=9 nan=1 mean=156.1 median=125.0 max=410
	// outlier: 410 (fence 147.5)
}

func ExampleCorrelate() {
	temp := make([]float64, 24)
	sales := make([]float64, 24)
	for i := range temp {
		temp[i] = 15 + float64(i%12)
		sales[i] = 40 + 3*temp[i] + float64((i*7)%5)
	}
	// ok is false below MinCorrelationSamples, so a short series cannot claim a link.
	c, ok := stats.Correlate(temp, sales)
	fmt.Printf("ok=%v n=%d r=%.3f significant=%v\n", ok, c.N, c.R, c.Significant)
	// Output:
	// ok=true n=24 r=0.991 significant=true
}
