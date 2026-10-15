package rate

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

// brute is the definition: min and max of the last size finite values.
func brute(xs []float64, size int) (lo, hi float64, n int) {
	var kept []float64
	for _, x := range xs {
		if finite(x) {
			kept = append(kept, x)
		}
	}
	if len(kept) > size {
		kept = kept[len(kept)-size:]
	}
	if len(kept) == 0 {
		return math.NaN(), math.NaN(), 0
	}
	lo, hi = kept[0], kept[0]
	for _, x := range kept[1:] {
		lo, hi = min(lo, x), max(hi, x)
	}
	return lo, hi, len(kept)
}

func same(a, b float64) bool { return a == b || math.IsNaN(a) && math.IsNaN(b) }

func mustWindow(t testing.TB, size int) *Window {
	t.Helper()
	w, err := NewWindow(size)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWindowKnownAnswer(t *testing.T) {
	w := mustWindow(t, 3)
	if !math.IsNaN(w.Min()) || !math.IsNaN(w.Max()) || w.Len() != 0 || w.Size() != 3 {
		t.Fatalf("empty: %v %v %d", w.Min(), w.Max(), w.Len())
	}
	steps := []struct{ x, lo, hi float64 }{
		{5, 5, 5},
		{1, 1, 5},
		{3, 1, 5},
		{4, 1, 4}, // 5 leaves
		{2, 2, 4}, // 1 leaves
		{9, 2, 9},
		{9, 2, 9}, // a tie with the maximum
		{9, 9, 9}, // a constant run
		{0, 0, 9},
	}
	for i, s := range steps {
		w.Add(s.x)
		if w.Min() != s.lo || w.Max() != s.hi {
			t.Fatalf("step %d (+%v): [%v, %v], want [%v, %v]", i, s.x, w.Min(), w.Max(), s.lo, s.hi)
		}
	}
	if w.Len() != 3 {
		t.Fatalf("len %d", w.Len())
	}
}

// Ties: the newer equal value is the one kept, so after its older twin leaves
// the window the value is still there.
func TestWindowTies(t *testing.T) {
	w := mustWindow(t, 2)
	w.Add(7)
	w.Add(7)
	if w.maxq.count != 1 || w.maxq.front().i != 1 || w.minq.front().i != 1 {
		t.Fatalf("tie kept index %d / %d (%d candidates), want the newer, 1", w.maxq.front().i, w.minq.front().i, w.maxq.count)
	}
	w.Add(3) // the first 7 leaves
	if w.Max() != 7 || w.Min() != 3 {
		t.Fatalf("[%v, %v]", w.Min(), w.Max())
	}
	// +0 and −0 are equal: either may be the answer, and it compares equal.
	z := mustWindow(t, 4)
	z.Add(math.Copysign(0, -1))
	z.Add(0)
	if z.Min() != 0 || z.Max() != 0 {
		t.Fatalf("signed zeros: [%v, %v]", z.Min(), z.Max())
	}
}

func TestWindowSkipsNonFinite(t *testing.T) {
	w := mustWindow(t, 2)
	w.Add(1)
	w.Add(math.NaN())
	w.Add(math.Inf(1))
	w.Add(math.Inf(-1))
	if w.Len() != 1 || w.Skipped() != 3 || w.Min() != 1 || w.Max() != 1 {
		t.Fatalf("len %d skipped %d [%v, %v]", w.Len(), w.Skipped(), w.Min(), w.Max())
	}
	w.Add(2) // skipped values took no slot: 1 is still in the window
	if w.Min() != 1 || w.Max() != 2 {
		t.Fatalf("[%v, %v]", w.Min(), w.Max())
	}
}

func TestWindowReset(t *testing.T) {
	w := mustWindow(t, 4)
	buf := &w.maxq.buf[0]
	for _, x := range []float64{3, 1, 4, 1, 5, 9, 2, 6, math.NaN()} {
		w.Add(x)
	}
	w.Reset()
	if w.Len() != 0 || w.Skipped() != 0 || !math.IsNaN(w.Max()) || !math.IsNaN(w.Min()) || w.Size() != 4 {
		t.Fatalf("after Reset: len %d skipped %d [%v, %v]", w.Len(), w.Skipped(), w.Min(), w.Max())
	}
	if &w.maxq.buf[0] != buf {
		t.Fatal("Reset replaced the buffer")
	}
	// Reused, it agrees with brute force from scratch.
	xs := []float64{-1, -5, 2, 2, 0, 8}
	for i, x := range xs {
		w.Add(x)
		lo, hi, n := brute(xs[:i+1], 4)
		if w.Min() != lo || w.Max() != hi || w.Len() != n {
			t.Fatalf("after reuse step %d: [%v, %v] len %d, want [%v, %v] %d", i, w.Min(), w.Max(), w.Len(), lo, hi, n)
		}
	}
	if n := testing.AllocsPerRun(10, w.Reset); n != 0 {
		t.Fatalf("Reset allocates %v", n)
	}
}

// Against brute force on random streams, for many sizes including 1 and
// sizes longer than the stream. Values are drawn from a few small integers so
// ties are everywhere, with constant runs, monotone runs (the queue's worst
// case) and non-finite values mixed in.
func TestWindowMatchesBruteForce(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for _, size := range []int{1, 2, 3, 4, 5, 7, 8, 16, 31, 64, 100, 1000} {
		for trial := range 10 {
			w := mustWindow(t, size)
			xs := make([]float64, 0, 600)
			for len(xs) < 600 {
				switch k := r.IntN(10); {
				case k < 5:
					xs = append(xs, float64(r.IntN(5)))
				case k < 6: // constant run
					v := float64(r.IntN(5))
					for range r.IntN(3 * size) {
						xs = append(xs, v)
					}
				case k < 7: // increasing run
					v := r.NormFloat64()
					for j := range r.IntN(2*size + 1) {
						xs = append(xs, v+float64(j))
					}
				case k < 8: // decreasing run
					v := r.NormFloat64()
					for j := range r.IntN(2*size + 1) {
						xs = append(xs, v-float64(j))
					}
				case k < 9:
					xs = append(xs, r.NormFloat64()*1e6)
				default:
					xs = append(xs, []float64{math.NaN(), math.Inf(1), math.Inf(-1)}[r.IntN(3)])
				}
			}
			for i, x := range xs {
				w.Add(x)
				lo, hi, n := brute(xs[:i+1], size)
				if !same(w.Min(), lo) || !same(w.Max(), hi) || w.Len() != n {
					t.Fatalf("size %d trial %d step %d: [%v, %v] len %d, want [%v, %v] len %d",
						size, trial, i, w.Min(), w.Max(), w.Len(), lo, hi, n)
				}
				if w.maxq.count > size || w.minq.count > size {
					t.Fatalf("size %d: queues %d/%d exceed capacity", size, w.maxq.count, w.minq.count)
				}
			}
		}
	}
}

func TestWindowRejects(t *testing.T) {
	for _, s := range []int{0, -1, math.MinInt, maxWindow + 1, math.MaxInt} {
		if w, err := NewWindow(s); !errors.Is(err, ErrInvalid) || w != nil {
			t.Errorf("size %d: %v, %v", s, w, err)
		}
	}
}

// The zero value is documented as unusable; Add on it panics rather than
// silently reporting nothing.
func TestWindowZeroValuePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Add on the zero Window did not panic")
		}
	}()
	var w Window
	w.Add(1)
}
