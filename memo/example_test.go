// SPDX-License-Identifier: BSD-3-Clause

package memo_test

import (
	"context"
	"fmt"
	"time"

	"github.com/org-runink/runi/memo"
)

func Example() {
	ctx := context.Background()
	// An injected clock lets the example move past the TTL without sleeping.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := memo.New[string, int](memo.Options{
		Capacity: 128,
		TTL:      time.Minute,
		Now:      func() time.Time { return now },
	})

	lookups := 0
	stock := func(context.Context) (int, error) {
		lookups++
		return 17, nil
	}

	key := memo.Hash("stock", map[string]any{"sku": "A-17", "warehouse": "east"})
	c.Do(ctx, key, stock)
	c.Do(ctx, key, stock) // served from the cache
	now = now.Add(2 * time.Minute)
	n, _ := c.Do(ctx, key, stock) // expired, so it runs again

	st := c.Stats()
	fmt.Printf("stock=%d lookups=%d hits=%d misses=%d expired=%d\n",
		n, lookups, st.Hits, st.Misses, st.Expired)
	// Output: stock=17 lookups=2 hits=1 misses=2 expired=1
}

func ExampleHash() {
	// Map keys are sorted before hashing, so field order cannot split the cache.
	a := memo.Hash("summarise", map[string]any{"doc": 42, "lang": "en"})
	b := memo.Hash("summarise", map[string]any{"lang": "en", "doc": 42})
	// Values are type-tagged: the string "42" is a different request from 42.
	c := memo.Hash("summarise", map[string]any{"doc": "42", "lang": "en"})

	// Free text that differs only in wrapping should be the same request.
	d := memo.Hash(memo.NormalizeSpace("  total  orders\nby region "))
	e := memo.Hash("total orders by region")

	fmt.Println(a == b, a == c, d == e)
	// Output: true false true
}
