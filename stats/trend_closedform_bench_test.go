// SPDX-License-Identifier: BSD-3-Clause

package stats

import (
	"math/rand/v2"
	"testing"
)

// trendClosedFormSink keeps the benchmarked call from being optimised away.
var trendClosedFormSink TrendTest

// trendClosedFormSeries builds the same series benchmarks/crossbench measures
// as trend_s: 100,000 points of a 0.001-per-step drift under unit noise.
func trendClosedFormSeries(n int) []float64 {
	r := rand.New(rand.NewPCG(7, 11))
	y := make([]float64, n)
	for i := range y {
		y[i] = 0.001*float64(i) + r.NormFloat64()
	}
	return y
}

func BenchmarkTrendClosedFormN100k(b *testing.B) {
	y := trendClosedFormSeries(100000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		trendClosedFormSink = Trend(y)
	}
}
