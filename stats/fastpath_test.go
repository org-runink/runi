package stats

import (
	"math"
	"math/rand/v2"
	"testing"
)

// Slope is Trend's slope without the test, and must be the same number, not a
// close one: callers swap one for the other.
func TestSlopeIsTrendsSlopeBitForBit(t *testing.T) {
	r := rand.New(rand.NewPCG(41, 42))
	same := func(y []float64) {
		t.Helper()
		got, want := Slope(y), Trend(y).Slope
		if math.Float64bits(got) != math.Float64bits(want) && !(math.IsNaN(got) && math.IsNaN(want)) {
			t.Fatalf("n=%d: Slope=%v Trend.Slope=%v", len(y), got, want)
		}
	}
	for n := 0; n < 4; n++ {
		same(make([]float64, n))
	}
	same([]float64{1, math.NaN(), 3})
	same([]float64{math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64}) // the overflow fallback
	for i := 0; i < 5000; i++ {
		n := 2 + r.IntN(400)
		y := make([]float64, n)
		off := []float64{0, 1e9, -1e15}[r.IntN(3)]
		for j := range y {
			y[j] = off + float64(j)*(r.Float64()-0.5) + r.NormFloat64()
		}
		same(y)
	}
}

// Quantile now takes its own path; it must give exactly what Quantiles gives,
// on both sides of the stack-buffer length and for every kind of q.
func TestQuantileMatchesQuantiles(t *testing.T) {
	r := rand.New(rand.NewPCG(43, 44))
	qs := []float64{0, 0.25, 0.5, 0.75, 1, 0.1, 0.999, -0.1, 1.1, math.NaN()}
	for i := 0; i < 20000; i++ {
		n := r.IntN(2 * smallQuantile)
		x := make([]float64, n)
		for j := range x {
			x[j] = math.Round(r.NormFloat64()*4) / 2 // ties on purpose
		}
		if n > 0 && r.IntN(20) == 0 {
			x[r.IntN(n)] = math.NaN()
		}
		q := qs[r.IntN(len(qs))]
		if r.IntN(3) == 0 {
			q = r.Float64()
		}
		got, want := Quantile(x, q), Quantiles(x, q)[0]
		if got != want && !(math.IsNaN(got) && math.IsNaN(want)) {
			t.Fatalf("n=%d q=%v: Quantile=%v Quantiles=%v", n, q, got, want)
		}
	}
	// Quantile must not reorder the caller's data on either path.
	for _, n := range []int{5, 3 * smallQuantile} {
		x := make([]float64, n)
		for j := range x {
			x[j] = float64(n - j)
		}
		Median(x)
		for j := range x {
			if x[j] != float64(n-j) {
				t.Fatalf("n=%d: Median reordered its input", n)
			}
		}
	}
}

// What the fast path is for: a median of a few values costs no allocation, and
// of a long column exactly one, the copy.
func TestQuantileAllocations(t *testing.T) {
	short := []float64{3, 1, 2}
	if a := testing.AllocsPerRun(100, func() { Median(short) }); a != 0 {
		t.Errorf("Median of 3: %v allocations, want 0", a)
	}
	full := make([]float64, smallQuantile)
	if a := testing.AllocsPerRun(100, func() { Median(full) }); a != 0 {
		t.Errorf("Median of %d: %v allocations, want 0", smallQuantile, a)
	}
	// Distinct values. A column of one repeated value is a known weak spot of
	// the partition quickSelect shares with Quantiles: it runs out of its
	// budget and falls back to the radix sort, which allocates its own buffer.
	long := make([]float64, 1000)
	for i := range long {
		long[i] = float64((i * 7919) % 1000)
	}
	if a := testing.AllocsPerRun(100, func() { Quantile(long, 0.9) }); a != 1 {
		t.Errorf("Quantile of 1000: %v allocations, want 1", a)
	}
	y := make([]float64, 365)
	if a := testing.AllocsPerRun(100, func() { Slope(y) }); a != 0 {
		t.Errorf("Slope: %v allocations, want 0", a)
	}
}

func BenchmarkMedian3(b *testing.B) {
	x := []float64{3, 1, 2}
	b.ReportAllocs()
	for b.Loop() {
		Median(x)
	}
}

func BenchmarkSlope365(b *testing.B) {
	y := make([]float64, 365)
	for i := range y {
		y[i] = float64(i % 17)
	}
	b.ReportAllocs()
	for b.Loop() {
		Slope(y)
	}
}

func BenchmarkTrend365(b *testing.B) {
	y := make([]float64, 365)
	for i := range y {
		y[i] = float64(i % 17)
	}
	b.ReportAllocs()
	for b.Loop() {
		Trend(y)
	}
}
