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
