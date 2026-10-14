package memo

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// A miss used to cost six allocations of bookkeeping: a list element, the key
// boxed into an interface twice, an entry, the in-flight call and its channel.
// The recency list now lives in the entries, a full store reuses the entry it
// evicts, and the channel is made only when someone waits. What is left on an
// uncontended miss is the in-flight call itself, plus the entry while the store
// is filling. A hit allocates nothing.
func TestAllocationBudgets(t *testing.T) {
	ctx := context.Background()
	fn := func(context.Context) (int, error) { return 1, nil }
	keys := make([]string, 4096)
	for i := range keys {
		keys[i] = fmt.Sprint("key-", i)
	}

	full := New[string, int](Options{Capacity: 256, TTL: time.Minute})
	for _, k := range keys[:256] {
		full.Do(ctx, k, fn)
	}
	i := 256
	if a := testing.AllocsPerRun(1000, func() {
		full.Do(ctx, keys[i%len(keys)], fn)
		i++
	}); a > 1 {
		t.Errorf("miss at capacity: %v allocations, want at most 1", a)
	}

	hit := New[string, int](Options{Capacity: 256, TTL: time.Minute})
	hit.Do(ctx, "k", fn)
	if a := testing.AllocsPerRun(1000, func() { hit.Do(ctx, "k", fn) }); a != 0 {
		t.Errorf("hit: %v allocations, want 0", a)
	}

	growing := New[int, int](Options{})
	n := 0
	if a := testing.AllocsPerRun(1000, func() {
		growing.Do(ctx, n, fn)
		n++
	}); a > 2.5 { // the call and the entry; the map's own growth is amortised
		t.Errorf("miss below capacity: %v allocations, want about 2", a)
	}
}
