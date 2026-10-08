package lazy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEvaluatesAtMostOnce(t *testing.T) {
	var n int32
	v := New(func(context.Context) (int, error) { atomic.AddInt32(&n, 1); return 7, nil })
	for i := 0; i < 5; i++ {
		got, err := v.Get(context.Background())
		if got != 7 || err != nil {
			t.Fatalf("got %v, %v", got, err)
		}
	}
	if n != 1 {
		t.Errorf("fn ran %d times, want 1", n)
	}
}

func TestNotEvaluatedUntilForced(t *testing.T) {
	var n int32
	v := New(func(context.Context) (int, error) { atomic.AddInt32(&n, 1); return 1, nil })
	// Composition must not force the source either.
	_ = Map(v, func(i int) int { return i * 2 })
	time.Sleep(20 * time.Millisecond)
	if atomic.LoadInt32(&n) != 0 {
		t.Error("value was evaluated without being forced")
	}
	if v.Resolved() {
		t.Error("Resolved() true before any force")
	}
}

// TestStartLowersLatency is the point of the package: a value started early is
// ready (or nearly) by the time it is needed.
func TestStartLowersLatency(t *testing.T) {
	const work = 120 * time.Millisecond
	mk := func() *Value[int] {
		return New(func(context.Context) (int, error) { time.Sleep(work); return 1, nil })
	}
	ctx := context.Background()

	cold := mk()
	t0 := time.Now()
	_, _ = cold.Get(ctx)
	coldWait := time.Since(t0)

	warm := mk()
	warm.Start(ctx)
	time.Sleep(work + 30*time.Millisecond) // do other things
	t1 := time.Now()
	_, _ = warm.Get(ctx)
	warmWait := time.Since(t1)

	t.Logf("Get latency: cold %v, pre-started %v", coldWait.Round(time.Millisecond), warmWait.Round(time.Microsecond))
	if warmWait > work/4 {
		t.Errorf("pre-started Get took %v, expected near zero", warmWait)
	}
	if coldWait < work {
		t.Errorf("cold Get took %v, expected at least %v", coldWait, work)
	}
}

func TestConcurrentGettersShareOneEvaluation(t *testing.T) {
	var n int32
	release := make(chan struct{})
	v := New(func(context.Context) (int, error) {
		atomic.AddInt32(&n, 1)
		<-release
		return 3, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = v.Get(context.Background()) }()
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()
	if n != 1 {
		t.Errorf("fn ran %d times for 32 concurrent getters, want 1", n)
	}
}

func TestCancelledGetDoesNotPoisonTheValue(t *testing.T) {
	release := make(chan struct{})
	v := New(func(context.Context) (int, error) { <-release; return 9, nil })

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := v.Get(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Get returned %v, want context.Canceled", err)
	}
	close(release)
	if got, err := v.Get(context.Background()); got != 9 || err != nil {
		t.Errorf("after a cancelled Get, value is %v/%v, want 9/nil", got, err)
	}
}

func TestErrorIsMemoised(t *testing.T) {
	var n int32
	boom := errors.New("boom")
	v := New(func(context.Context) (int, error) { atomic.AddInt32(&n, 1); return 0, boom })
	for i := 0; i < 3; i++ {
		if _, err := v.Get(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("got %v, want boom", err)
		}
	}
	if n != 1 {
		t.Errorf("fn ran %d times, want 1 — a lazy value is one evaluation, not a retry", n)
	}
}

func TestPanicBecomesAnError(t *testing.T) {
	v := New(func(context.Context) (int, error) { panic("kaboom") })
	_, err := v.Get(context.Background())
	if !errors.Is(err, ErrPanic) {
		t.Fatalf("got %v, want ErrPanic", err)
	}
	// Every caller sees it, and nothing crashed.
	if _, err2 := v.Get(context.Background()); !errors.Is(err2, ErrPanic) {
		t.Errorf("second caller got %v, want ErrPanic", err2)
	}
}

func TestMapSharesOneSourceEvaluation(t *testing.T) {
	var n int32
	src := New(func(context.Context) (int, error) { atomic.AddInt32(&n, 1); return 10, nil })
	a := Map(src, func(i int) int { return i + 1 })
	b := Map(src, func(i int) int { return i * 2 })
	ctx := context.Background()
	av, _ := a.Get(ctx)
	bv, _ := b.Get(ctx)
	if av != 11 || bv != 20 {
		t.Errorf("got %d and %d, want 11 and 20", av, bv)
	}
	if n != 1 {
		t.Errorf("source ran %d times for 2 consumers, want 1", n)
	}
}

func TestThenPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	src := New(func(context.Context) (int, error) { return 0, boom })
	chained := Then(src, func(context.Context, int) (string, error) {
		t.Error("downstream ran despite an upstream error")
		return "", nil
	})
	if _, err := chained.Get(context.Background()); !errors.Is(err, boom) {
		t.Errorf("got %v, want boom", err)
	}
}

// TestAllRunsConcurrently proves All starts everything before waiting.
//
// It counts how many values are in flight at once rather than timing the whole
// thing. The timing version — n values of 80ms must finish in under 160ms —
// failed on a loaded macOS CI runner that took 203ms, which says nothing about
// whether All is concurrent and everything about what else was running on the
// machine. A test that fails when the runner is busy teaches people to press
// re-run instead of reading it, so it stops being a test.
//
// The barrier makes the property exact: each value blocks until all n have
// started. If All resolved them one at a time this deadlocks rather than
// running slowly, so the timeout below is a backstop, not the assertion.
func TestAllRunsConcurrently(t *testing.T) {
	const n = 5

	var mu sync.Mutex
	inFlight, peak := 0, 0
	started := make(chan struct{}, n)
	release := make(chan struct{})

	vs := make([]*Value[int], n)
	for i := range vs {
		vs[i] = New(func(context.Context) (int, error) {
			mu.Lock()
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			mu.Unlock()

			started <- struct{}{}
			<-release // every value waits for every other to have started

			mu.Lock()
			inFlight--
			mu.Unlock()
			return 1, nil
		})
	}

	done := make(chan error, 1)
	var out []int
	go func() {
		var err error
		out, err = All(context.Background(), vs...)
		done <- err
	}()

	// All n must start before any is allowed to finish.
	for i := range n {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			mu.Lock()
			p := peak
			mu.Unlock()
			t.Fatalf("only %d of %d values started (peak concurrency %d): All is resolving them in sequence", i, n, p)
		}
	}
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("All: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("All did not return after every value was released")
	}
	if len(out) != n {
		t.Fatalf("got %d values, want %d", len(out), n)
	}
	if peak != n {
		t.Errorf("peak concurrency %d, want %d", peak, n)
	}
}

func TestOfIsAlreadyResolved(t *testing.T) {
	v := Of(42)
	if !v.Resolved() {
		t.Error("Of should be resolved immediately")
	}
	if got, err := v.Get(context.Background()); got != 42 || err != nil {
		t.Errorf("got %v, %v", got, err)
	}
}
