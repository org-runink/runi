package season

import (
	"math"
	"math/rand/v2"
	"testing"
)

// benchSeriesXYZ is the series benchmarks/crossbench/main.go measures
// season_classical_s on: n=4,000, period 24, a line plus a sine plus noise.
func benchSeriesXYZ(n, period int) []float64 {
	r := rand.New(rand.NewPCG(7, 11))
	x := make([]float64, n)
	for i := range x {
		x[i] = 100 + 0.05*float64(i) +
			10*math.Sin(2*math.Pi*float64(i)/float64(period)) + r.NormFloat64()
	}
	return x
}

func BenchmarkClassicalXYZ(b *testing.B) {
	x := benchSeriesXYZ(4000, 24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := Classical(x, 24)
		if err != nil {
			b.Fatal(err)
		}
		_ = d
	}
}

// The like-for-like row against statsmodels.seasonal_decompose: the period is
// given and the break search is off, which is the comparison
// benchmarks/crossbench/main.go measures as season_decompose_s.
func BenchmarkDecomposeNoBreaksXYZ(b *testing.B) {
	x := benchSeriesXYZ(4000, 24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := Decompose(x, Options{Period: 24, MaxChangepoints: -1})
		if err != nil {
			b.Fatal(err)
		}
		_ = d
	}
}

// The default path, which also prices a changepoint search. Measured so that
// a change made for the row above cannot quietly cost the usual caller.
func BenchmarkDecomposeBreaksXYZ(b *testing.B) {
	x := benchSeriesXYZ(4000, 24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := Decompose(x, Options{Period: 24})
		if err != nil {
			b.Fatal(err)
		}
		_ = d
	}
}

// Period detection as well as everything else, the season_auto_s row.
func BenchmarkDecomposeAutoXYZ(b *testing.B) {
	x := benchSeriesXYZ(4000, 24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := Decompose(x, Options{})
		if err != nil {
			b.Fatal(err)
		}
		_ = d
	}
}
