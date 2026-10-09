package lazy

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
)

// lazy's contract is "at most once, whoever asks". Both halves are statements
// about concurrent schedules, so they are stated here over many callers and
// many runs rather than over one hand-written race.

// However many goroutines force a value at once, the function runs once and
// they all see the same answer.
func TestPropertyConcurrentGettersEvaluateOnce(t *testing.T) {
	r := rand.New(rand.NewPCG(141, 142))
	for i := 0; i < 300; i++ {
		callers := 2 + r.IntN(64)
		var ran int64
		v := New(func(context.Context) (int, error) {
			atomic.AddInt64(&ran, 1)
			return 99, nil
		})

		var release sync.WaitGroup
		var done sync.WaitGroup
		release.Add(1)
		done.Add(callers)
		got := make([]int, callers)
		errs := make([]error, callers)
		for c := 0; c < callers; c++ {
			go func(c int) {
				defer done.Done()
				release.Wait()
				got[c], errs[c] = v.Get(context.Background())
			}(c)
		}
		release.Done()
		done.Wait()

		if n := atomic.LoadInt64(&ran); n != 1 {
			t.Fatalf("%d callers caused %d evaluations", callers, n)
		}
		for c := range got {
			if errs[c] != nil || got[c] != 99 {
				t.Fatalf("caller %d got %v, %v", c, got[c], errs[c])
			}
		}
	}
}

// A value is not forced until someone asks, however many times it is wrapped.
// A Map chain that evaluated eagerly would defeat the point of the package.
func TestPropertyMapChainsStayUnevaluated(t *testing.T) {
	r := rand.New(rand.NewPCG(143, 144))
	for i := 0; i < 2000; i++ {
		var ran int64
		v := New(func(context.Context) (int, error) {
			atomic.AddInt64(&ran, 1)
			return 1, nil
		})
		depth := 1 + r.IntN(8)
		cur := v
		for d := 0; d < depth; d++ {
			cur = Map(cur, func(x int) int { return x + 1 })
		}
		if n := atomic.LoadInt64(&ran); n != 0 {
			t.Fatalf("a chain of %d maps evaluated %d times before being forced", depth, n)
		}
		got, err := cur.Get(context.Background())
		if err != nil || got != 1+depth {
			t.Fatalf("depth %d: got %v, %v", depth, got, err)
		}
		if n := atomic.LoadInt64(&ran); n != 1 {
			t.Fatalf("depth %d: source evaluated %d times", depth, n)
		}
	}
}

// Map composes: mapping f then g is mapping g after f, and the source is
// still evaluated once however the chain is shaped.
func TestPropertyMapComposes(t *testing.T) {
	r := rand.New(rand.NewPCG(145, 146))
	for i := 0; i < 2000; i++ {
		x := r.IntN(1000)
		a, b := r.IntN(10)+1, r.IntN(10)+1
		f := func(v int) int { return v * a }
		g := func(v int) int { return v + b }

		var ran int64
		src := New(func(context.Context) (int, error) {
			atomic.AddInt64(&ran, 1)
			return x, nil
		})
		stepwise, err := Map(Map(src, f), g).Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		fused, err := Map(New(func(context.Context) (int, error) { return x, nil }),
			func(v int) int { return g(f(v)) }).Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if stepwise != fused {
			t.Fatalf("Map(Map(v,f),g) = %v but Map(v, g.f) = %v", stepwise, fused)
		}
		if n := atomic.LoadInt64(&ran); n != 1 {
			t.Fatalf("the shared source was evaluated %d times", n)
		}
	}
}

// An error is remembered, like a value: the function does not run again on
// every retry, and every caller sees the same error.
func TestPropertyAnErrorIsMemoisedToo(t *testing.T) {
	r := rand.New(rand.NewPCG(147, 148))
	for i := 0; i < 2000; i++ {
		boom := errors.New("boom")
		var ran int64
		v := New(func(context.Context) (int, error) {
			atomic.AddInt64(&ran, 1)
			return 0, boom
		})
		for try := 0; try < 1+r.IntN(5); try++ {
			if _, err := v.Get(context.Background()); !errors.Is(err, boom) {
				t.Fatalf("try %d: got %v", try, err)
			}
		}
		if n := atomic.LoadInt64(&ran); n != 1 {
			t.Fatalf("a failing function ran %d times", n)
		}
	}
}

// All returns one result per value, in order, whatever the values are and
// however many of them fail.
func TestPropertyAllIsOrderedAndComplete(t *testing.T) {
	r := rand.New(rand.NewPCG(149, 150))
	for i := 0; i < 2000; i++ {
		n := r.IntN(12)
		vs := make([]*Value[int], n)
		failAt := -1
		if n > 0 && r.IntN(3) == 0 {
			failAt = r.IntN(n)
		}
		for j := range vs {
			j := j
			if j == failAt {
				vs[j] = New(func(context.Context) (int, error) { return 0, errors.New("boom") })
				continue
			}
			vs[j] = New(func(context.Context) (int, error) { return j * 10, nil })
		}
		got, err := All(context.Background(), vs...)
		if len(got) != n {
			t.Fatalf("n=%d: All returned %d results", n, len(got))
		}
		if (err != nil) != (failAt >= 0) {
			t.Fatalf("n=%d failAt=%d: err = %v", n, failAt, err)
		}
		for j := range got {
			if j == failAt {
				continue // a failed value's slot holds the zero value
			}
			if got[j] != j*10 {
				t.Fatalf("result %d = %v, want %v", j, got[j], j*10)
			}
		}
	}
}
