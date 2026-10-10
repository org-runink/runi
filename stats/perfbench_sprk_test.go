package stats

import (
	"math"
	"math/rand"
	randv2 "math/rand/v2"
	"testing"
)

// corrInputsSPRK builds the same shape of data benchmarks/crossbench/main.go
// uses for pearson_s and spearman_s: n normal deviates, and a second column
// that is 0.6 of the first plus independent noise.
func corrInputsSPRK(n int) ([]float64, []float64) {
	r := randv2.New(randv2.NewPCG(7, 11))
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

// shapesSPRK is the set of column shapes the ranking behaves differently on:
// the continuous one the published benchmark uses, the two the radix passes
// collapse on, the one that is all ties, and the one that defeats the 32-bit
// prefix entirely by putting every value inside a single prefix.
func shapesSPRK(n int) map[string][]float64 {
	r := rand.New(rand.NewSource(7))
	base := math.Float64bits(1.0)
	out := map[string][]float64{}
	for _, name := range []string{"normal", "sorted", "constant", "tied", "one prefix"} {
		x := make([]float64, n)
		for i := range x {
			switch name {
			case "normal":
				x[i] = r.NormFloat64()
			case "sorted":
				x[i] = float64(i)
			case "constant":
				x[i] = 1.25
			case "tied":
				x[i] = float64(r.Intn(16))
			default:
				x[i] = math.Float64frombits(base + uint64(n-i))
			}
		}
		out[name] = x
	}
	return out
}

// Every shape, new against the implementation this branch replaced, so a win
// on normal deviates cannot hide a loss somewhere else.
func BenchmarkSpearmanShapesSPRK(b *testing.B) {
	const n = 200000
	shapes := shapesSPRK(n)
	y := make([]float64, n)
	r := rand.New(rand.NewSource(11))
	for i := range y {
		y[i] = r.NormFloat64()
	}
	names := []string{"normal", "sorted", "constant", "tied", "one prefix"}
	for _, name := range names {
		x := shapes[name]
		b.Run(name+"/new", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				v, _ := Spearman(x, y)
				sinkSPRK = v
			}
		})
		b.Run(name+"/old", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				v, _ := spearmanOldSPRK(x, y)
				sinkSPRK = v
			}
		})
	}
}
