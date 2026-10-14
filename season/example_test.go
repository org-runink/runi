// SPDX-License-Identifier: BSD-3-Clause

package season_test

import (
	"fmt"

	"github.com/org-runink/runi/season"
)

// dailyOrders is eight weeks of orders: a weekly cycle on a slow climb, plus
// noise from a fixed-seed generator so the printed output never changes.
func dailyOrders() []float64 {
	weekly := []float64{-8, -2, 3, 5, 6, 4, -8}
	seed := uint64(7)
	x := make([]float64, 56)
	for i := range x {
		seed = seed*6364136223846793005 + 1442695040888963407
		noise := float64(seed>>11)/float64(1<<53)*2 - 1
		x[i] = 100 + 0.5*float64(i) + weekly[i%7] + noise
	}
	return x
}

func Example() {
	x := dailyOrders()

	// Period 0 asks Decompose to find the cycle; this trend never breaks, so skip that search.
	d, err := season.Decompose(x, season.Options{MaxChangepoints: -1})
	if err != nil {
		panic(err)
	}
	fmt.Println("period:", d.Period)
	fmt.Printf("%.1f\n", d.Forecast(7))
	// Output:
	// period: 7
	// [119.6 126.6 131.9 134.2 135.7 134.3 123.0]
}

func ExampleClassical() {
	// When the period is known, the moving average is the cheaper tool.
	d, err := season.Classical(dailyOrders(), 7)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%.1f\n", d.Seasonal[:7])
	// Output:
	// [-8.2 -1.8 3.0 4.9 5.9 4.0 -7.6]
}
