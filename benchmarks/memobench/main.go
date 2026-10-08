// Command memobench runs the same experiment as benchmarks/bench_lru.py
// against runi/memo, so the two sets of numbers are comparable.
//
// Same work function (5ms), same caller counts, and the same thing counted:
// how many times the expensive function actually ran.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/org-runink/runi/memo"
)

const workMS = 5

type stampede struct {
	Callers          int     `json:"callers"`
	WallSeconds      float64 `json:"wall_seconds"`
	FunctionRanTimes int64   `json:"function_ran_times"`
}

// runStampede releases `callers` goroutines onto one cold key simultaneously
// and counts how many times the expensive function actually executed.
func runStampede(callers int) stampede {
	store := memo.New[string, int](memo.Options{Capacity: 1024})
	var ran int64

	var release sync.WaitGroup
	var done sync.WaitGroup
	release.Add(1)
	done.Add(callers)

	for i := 0; i < callers; i++ {
		go func() {
			defer done.Done()
			release.Wait()
			_, _ = store.Do(context.Background(), "cold", func(ctx context.Context) (int, error) {
				atomic.AddInt64(&ran, 1)
				time.Sleep(workMS * time.Millisecond)
				return 42, nil
			})
		}()
	}

	// Give every goroutine time to park on release.Wait() so they genuinely
	// start together, the same thing Python's Barrier does.
	time.Sleep(20 * time.Millisecond)
	t0 := time.Now()
	release.Done()
	done.Wait()

	return stampede{
		Callers:          callers,
		WallSeconds:      time.Since(t0).Seconds(),
		FunctionRanTimes: atomic.LoadInt64(&ran),
	}
}

func benchHit(reps int) float64 {
	store := memo.New[string, int](memo.Options{Capacity: 1024})
	ctx := context.Background()
	fn := func(ctx context.Context) (int, error) { return 2, nil }
	_, _ = store.Do(ctx, "k", fn) // prime
	t0 := time.Now()
	for i := 0; i < reps; i++ {
		_, _ = store.Do(ctx, "k", fn)
	}
	return float64(time.Since(t0).Nanoseconds()) / float64(reps)
}

func benchMiss(n int) float64 {
	store := memo.New[string, int](memo.Options{Capacity: 1024})
	ctx := context.Background()
	fn := func(ctx context.Context) (int, error) { return 2, nil }

	// Keys are built BEFORE the timer starts: formatting is not what we are
	// measuring, and Python's loop does not pay for it either.
	keys := make([]string, n)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	t0 := time.Now()
	for i := 0; i < n; i++ {
		_, _ = store.Do(ctx, keys[i], fn)
	}
	return float64(time.Since(t0).Nanoseconds()) / float64(n)
}

func main() {
	res := map[string]any{
		"library":           "runi/memo",
		"go":                runtime.Version(),
		"hit_ns":            benchHit(200000),
		"miss_ns":           benchMiss(50000),
		"stampede_8":        runStampede(8),
		"stampede_64":       runStampede(64),
		"has_ttl":           true,
		"has_single_flight": true,
		"work_ms":           workMS,
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	here, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(here, "results_memo.json"), b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	fmt.Println(string(b))
}
