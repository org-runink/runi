// Command eventmesh measures the shape an event handler actually has, rather
// than the speed of one call.
//
// An event mesh — a Kubernetes controller, a webhook fan-in, a change feed —
// does not receive distinct work. It receives the SAME object several times:
// once per watch, per replica, per retry, per relist. The question that
// decides whether the handler keeps up is not how fast one handler runs. It is
// how many times the expensive part runs for work that was already in flight.
//
// So this counts executions, not nanoseconds, across the three things a Go
// service actually does about it:
//
//  1. nothing — handle every event;
//  2. a mutex and a map, which is what almost everyone writes;
//  3. runi/memo.
//
// Strategy 2 is the interesting one. It looks like a cache and it is a cache,
// but it does not deduplicate CONCURRENT work: every goroutine that arrives
// while the first is still working finds the map empty and starts again. That
// is the stampede, and it is invisible in a sequential test.
//
// No third-party code: the alternatives are implemented here so the comparison
// needs no dependency the module refuses to take.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/org-runink/runi/memo"
)

// handlerCost stands in for what an event handler really does: a call to the
// API server, a webhook, a model. It is I/O, so it is modelled as latency
// rather than as CPU.
const handlerCost = 2 * time.Millisecond

type result struct {
	Strategy     string  `json:"strategy"`
	Events       int     `json:"events"`
	DistinctKeys int     `json:"distinct_keys"`
	Executions   int64   `json:"handler_executions"`
	WallSeconds  float64 `json:"wall_seconds"`
	PerEventUS   float64 `json:"per_event_us"`
}

// burst releases every event at once, which is what a relist or a leader
// change looks like, and is the case the three strategies disagree about.
func burst(events, keys int, handle func(ctx context.Context, key string) error) (int64, float64) {
	var ran int64
	_ = ran
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	done.Add(events)

	for i := 0; i < events; i++ {
		key := fmt.Sprintf("object-%d", i%keys)
		go func() {
			defer done.Done()
			start.Wait()
			_ = handle(context.Background(), key)
		}()
	}
	time.Sleep(50 * time.Millisecond) // let every goroutine park on start
	t0 := time.Now()
	start.Done()
	done.Wait()
	return 0, time.Since(t0).Seconds()
}

func main() {
	const (
		events = 2000
		keys   = 50
	)
	var out []result

	// 1. No deduplication: the handler runs once per event, by definition.
	{
		var ran int64
		work := func(ctx context.Context, key string) error {
			atomic.AddInt64(&ran, 1)
			time.Sleep(handlerCost)
			return nil
		}
		_, secs := burst(events, keys, work)
		out = append(out, result{"no deduplication", events, keys, atomic.LoadInt64(&ran), secs,
			secs * 1e6 / float64(events)})
	}

	// 2. A mutex and a map. Correct, obvious, and it still stampedes: every
	//    caller that arrives before the first one stores finds nothing there.
	{
		var ran int64
		var mu sync.Mutex
		cache := map[string]struct{}{}
		work := func(ctx context.Context, key string) error {
			mu.Lock()
			_, hit := cache[key]
			mu.Unlock()
			if hit {
				return nil
			}
			atomic.AddInt64(&ran, 1)
			time.Sleep(handlerCost)
			mu.Lock()
			cache[key] = struct{}{}
			mu.Unlock()
			return nil
		}
		_, secs := burst(events, keys, work)
		out = append(out, result{"mutex and a map", events, keys, atomic.LoadInt64(&ran), secs,
			secs * 1e6 / float64(events)})
	}

	// 3. runi/memo: the callers that arrive during an execution wait on it
	//    instead of starting their own.
	{
		var ran int64
		store := memo.New[string, struct{}](memo.Options{Capacity: 1024, TTL: time.Minute})
		work := func(ctx context.Context, key string) error {
			_, err := store.Do(ctx, key, func(context.Context) (struct{}, error) {
				atomic.AddInt64(&ran, 1)
				time.Sleep(handlerCost)
				return struct{}{}, nil
			})
			return err
		}
		_, secs := burst(events, keys, work)
		out = append(out, result{"runi/memo", events, keys, atomic.LoadInt64(&ran), secs,
			secs * 1e6 / float64(events)})
	}

	doc := map[string]any{
		"scenario":         "one burst of events over a small set of objects, as a relist or a leader change produces",
		"go":               runtime.Version(),
		"handler_cost_ms":  handlerCost.Milliseconds(),
		"ideal_executions": 50,
		"results":          out,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	here, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(here, "results_eventmesh.json"), b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	fmt.Println(string(b))
}
