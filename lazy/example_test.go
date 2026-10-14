// SPDX-License-Identifier: BSD-3-Clause

package lazy_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/org-runink/runi/lazy"
)

func Example() {
	ctx := context.Background()
	calls := 0
	report := lazy.New(func(context.Context) (string, error) {
		calls++
		return "42 orders shipped", nil
	})
	// Nothing has run yet: a value nobody needs costs nothing.
	fmt.Println("resolved:", report.Resolved())

	// Start it as soon as it is likely to be needed; it runs while we do other work.
	report.Start(ctx)

	// Get waits for that same run, and every caller shares it.
	a, _ := report.Get(ctx)
	b, _ := report.Get(ctx)
	fmt.Println(a, "/", b, "/ calls:", calls)
	// Output:
	// resolved: false
	// 42 orders shipped / 42 orders shipped / calls: 1
}

func ExampleThen() {
	ctx := context.Background()
	doc := lazy.New(func(context.Context) (string, error) {
		return "sensor 7 reported a fault", nil
	})
	// Both derived values reuse one fetch of doc, and neither runs until asked for.
	words := lazy.Map(doc, func(s string) int { return len(strings.Fields(s)) })
	upper := lazy.Then(doc, func(_ context.Context, s string) (string, error) {
		return strings.ToUpper(s), nil
	})

	got, err := lazy.All(ctx, words, lazy.Map(upper, func(s string) int { return len(s) }))
	fmt.Println(got, err)
	// Output: [5 25] <nil>
}
