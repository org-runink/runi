package tree

import (
	"math/rand/v2"
	"strconv"
	"testing"
)

// The package doc promises no allocation on the hot paths; this is the test
// of it. Each closure changes its argument so the call is not optimised to
// nothing, and Set alternates large and small values so the maxima above it
// change and the heap repair runs on every call.
func TestZeroAllocs(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	tr, _ := randomForest(t, r, 1000)
	const k = 10
	entries := make([]Entry[int], 0, k)
	keys := make([]int, 0, 1000)
	i, x := 0, 0.0
	var sink float64
	var sinkOK bool
	next := func() int { i = (i*7919 + 13) % 1000; return i }
	for name, f := range map[string]func(){
		"Set":      func() { x = 1e6 - x; must(t, tr.Set(next(), x)) },
		"Sum":      func() { sink += tr.Sum(next()) },
		"Own":      func() { sink += tr.Own(next()) },
		"Max":      func() { v, _, ok := tr.Max(next()); sink, sinkOK = sink+v, ok },
		"Parent":   func() { p, ok := tr.Parent(next()); sink, sinkOK = sink+float64(p), ok },
		"Depth":    func() { d, ok := tr.Depth(next()); sink, sinkOK = sink+float64(d), ok },
		"TopK":     func() { entries = tr.TopK(0, k, Metric(next()&1), entries[:0]) },
		"Children": func() { keys = tr.Children(next(), keys[:0]) },
		"Path":     func() { keys = tr.Path(next(), keys[:0]) },
	} {
		if n := testing.AllocsPerRun(1000, f); n != 0 {
			t.Errorf("%s: %v allocations per call, want 0", name, n)
		}
	}
	if len(entries) != k {
		t.Fatalf("TopK closure returned %d entries, want %d", len(entries), k)
	}
	_, _ = sink, sinkOK
}

// A check that cannot fail is worth nothing: the same measurement sees the
// allocation when the destination has no room, which is the case the docs
// exclude.
func TestZeroAllocsCanFail(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	tr, _ := randomForest(t, r, 1000)
	var entries []Entry[int]
	var keys []int
	if n := testing.AllocsPerRun(100, func() { entries = tr.TopK(0, 10, BySum, nil) }); n == 0 {
		t.Error("AllocsPerRun reported 0 for TopK into a nil slice")
	}
	if n := testing.AllocsPerRun(100, func() { keys = tr.Path(999, nil) }); n == 0 {
		t.Error("AllocsPerRun reported 0 for Path into a nil slice")
	}
	_, _ = entries, keys
}

// spine builds a tree whose node "leaf" sits at the given depth, with seven
// siblings at every level, so each update walks depth levels of 8-way
// heaps. It returns the tree and the key of that deepest node.
func spine(b testing.TB, depth int) (*Tree[int], int) {
	tr := New[int]()
	must(b, tr.AddRoot(0))
	next, at := 1, 0
	for range depth {
		var child int
		for s := range 8 {
			must(b, tr.Add(next, at))
			must(b, tr.Set(next, float64(next%97)))
			if s == 3 {
				child = next
			}
			next++
		}
		at = child
	}
	return tr, at
}

func BenchmarkSet(b *testing.B) {
	for _, depth := range []int{3, 10, 30} {
		b.Run("depth="+strconv.Itoa(depth), func(b *testing.B) {
			tr, leaf := spine(b, depth)
			b.ReportAllocs()
			for i := range b.N {
				// Alternately the maximum of every ancestor and not: the
				// worst case for the heap repair.
				_ = tr.Set(leaf, float64(i&1)*1e6)
			}
		})
	}
}

func BenchmarkTopK(b *testing.B) {
	for _, n := range []int{1_000, 100_000} {
		b.Run("nodes="+strconv.Itoa(n), func(b *testing.B) {
			r := rand.New(rand.NewPCG(3, 4))
			tr, _ := randomForest(b, r, n)
			for i := range n {
				must(b, tr.Set(i, float64(r.IntN(1e6))))
			}
			dst := make([]Entry[int], 0, 10)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				dst = tr.TopK(0, 10, BySum, dst[:0])
			}
		})
	}
}

func BenchmarkDiff(b *testing.B) {
	const n = 10_000
	r := rand.New(rand.NewPCG(5, 6))
	tr, _ := randomForest(b, r, n)
	a := tr.Snapshot()
	for i := range n / 100 { // 1% of nodes each added, moved and removed
		must(b, tr.Add(n+i, r.IntN(n)))
		if id, p := r.IntN(n), r.IntN(n); tr.Move(id, p) != nil {
			continue // a cycle: skip it
		}
	}
	for removed := 0; removed < n/100; {
		if tr.Remove(r.IntN(n)) == nil {
			removed++
		}
	}
	c := tr.Snapshot()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = Diff(a, c)
	}
}
