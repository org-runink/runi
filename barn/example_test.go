// SPDX-License-Identifier: BSD-3-Clause

package barn_test

import (
	"context"
	"fmt"
	"time"

	"github.com/org-runink/runi/barn"
)

func Example() {
	ctx := context.Background()
	// A fixed clock keeps the example's row timestamps reproducible.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t, err := barn.Open(barn.NewMemStore(), "shop", "orders",
		barn.WithClock(func() time.Time { return now }))
	if err != nil {
		panic(err)
	}

	v1, _ := t.Put(ctx,
		barn.Row{Key: "order/1", Payload: []byte(`{"status":"open"}`)},
		barn.Row{Key: "order/2", Payload: []byte(`{"status":"open"}`)},
	)
	v2, _ := t.Put(ctx, barn.Row{Key: "order/1", Payload: []byte(`{"status":"shipped"}`)})
	v3, _ := t.Delete(ctx, "order/2")
	fmt.Println("versions:", v1, v2, v3)

	rows, _ := t.Scan(ctx, "order/")
	for _, r := range rows {
		fmt.Printf("%s v%d %s\n", r.Key, r.Version, r.Payload)
	}
	// Output:
	// versions: 1 2 3
	// order/1 v2 {"status":"shipped"}
}

func ExampleTable_Snapshot() {
	ctx := context.Background()
	t, _ := barn.Open(barn.NewMemStore(), "shop", "sensors")
	t.Put(ctx, barn.Row{Key: "sensor/a", Payload: []byte("21.5")})
	t.Put(ctx, barn.Row{Key: "sensor/a", Payload: []byte("23.0")})

	// Every commit stays readable, so "what did we believe at v1" has an answer.
	old, err := t.Snapshot(ctx, 1)
	if err != nil {
		panic(err)
	}
	r, _, _ := old.Get(ctx, "sensor/a")
	cur, _, _ := t.Get(ctx, "sensor/a")
	fmt.Printf("v1: %s, head: %s\n", r.Payload, cur.Payload)
	// Output: v1: 21.5, head: 23.0
}
