// SPDX-License-Identifier: BSD-3-Clause

package bm25_test

import (
	"fmt"
	"sort"

	"github.com/org-runink/runi/bm25"
)

func Example() {
	docs := []bm25.Document{
		{ID: "kb-1", Text: "Payment failed with ERR-4021: card declined by issuer"},
		{ID: "kb-2", Text: "ERR-4022 means the payment gateway timed out; retry later"},
		{ID: "kb-3", Text: "How to update the card on file for a subscription"},
		{ID: "kb-4", Text: "Shipping times for international orders"},
	}
	ix := bm25.New(docs, bm25.Options{})

	// The exact code decides first place; kb-2 places only on the shared "err".
	for _, r := range ix.Search("err-4021 card declined", 2) {
		fmt.Printf("%s %.3f\n", r.ID, r.Score)
	}
	// Output:
	// kb-1 3.705
	// kb-2 0.646
}

func ExampleIndex_Search() {
	ix := bm25.New([]bm25.Document{
		{ID: "a", Text: "sensor offline after firmware update"},
		{ID: "b", Text: "firmware update schedule for gateways"},
		{ID: "c", Text: "battery replacement for outdoor sensor units"},
	}, bm25.Options{})

	top := ix.Search("sensor firmware", 1)[0]
	// Terms explains the rank; sort it, since map order is random.
	terms := make([]string, 0, len(top.Terms))
	for t := range top.Terms {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	fmt.Println("top:", top.ID)
	for _, t := range terms {
		fmt.Printf("  %s %.3f\n", t, top.Terms[t])
	}
	// Output:
	// top: a
	//   firmware 0.482
	//   sensor 0.482
}
