// SPDX-License-Identifier: BSD-3-Clause

package budget_test

import (
	"context"
	"fmt"
	"time"

	"github.com/org-runink/runi/budget"
)

func Example() {
	plan := budget.Plan{
		Total:    3 * time.Minute,
		Margin:   5 * time.Second,
		MinSlice: 3 * time.Second,
		Phases: []budget.Phase{
			{Name: "search", Stage: 0, Cap: 90 * time.Second},
			{Name: "answer", Stage: 1, Cap: time.Minute, Floor: 40 * time.Second},
		},
	}
	// A fake clock, so the example is about the arithmetic.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b, err := budget.New(plan, func() time.Time { return now })
	if err != nil {
		panic(err)
	}

	fmt.Println("search may take", b.Allowance("search"))
	_, cancel, _ := b.Begin(context.Background(), "search")
	defer cancel()
	now = now.Add(150 * time.Second) // the search is slow and never returns
	fmt.Println("answer still gets", b.Allowance("answer"))

	for _, r := range b.Finalize() {
		fmt.Printf("%s: %s after %v\n", r.Phase.Name, r.Status, r.Elapsed)
	}
	// Output:
	// search may take 1m30s
	// answer still gets 25s
	// search: overrun after 2m30s
}
