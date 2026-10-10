package stats

import (
	"math/rand/v2"
	"testing"
)

// corrInputsSPRK builds the same shape of data benchmarks/crossbench/main.go
// uses for pearson_s and spearman_s: n normal deviates, and a second column
// that is 0.6 of the first plus independent noise.
func corrInputsSPRK(n int) ([]float64, []float64) {
	r := rand.New(rand.NewPCG(7, 11))
	xs, ys := make([]float64, n), make([]float64, n)
	for i := range xs {
		xs[i] = r.NormFloat64()
		ys[i] = 0.6*xs[i] + r.NormFloat64()
	}
	return xs, ys
}

func BenchmarkSpearman200kSPRK(b *testing.B) {
	xs, ys := corrInputsSPRK(200000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := Spearman(xs, ys)
		if err != nil {
			b.Fatal(err)
		}
		sinkSPRK = v
	}
}

func BenchmarkPearson200kSPRK(b *testing.B) {
	xs, ys := corrInputsSPRK(200000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := Pearson(xs, ys)
		if err != nil {
			b.Fatal(err)
		}
		sinkSPRK = v
	}
}

var sinkSPRK float64
