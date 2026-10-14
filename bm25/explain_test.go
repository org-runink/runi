package bm25

import (
	"strconv"
	"strings"
	"testing"
)

// A result's Terms map is sized by the terms that document matched, not by
// the query. Sized by a twenty-term query, every map took several
// allocations where the one or two terms a document held fit in one; that
// made a k=0 search allocate more than v0.8.0 did. A wide query over
// documents that each hold one or two of its terms is the case that shows it.
func TestExplainIsSizedByTheDocument(t *testing.T) {
	var query []string
	for i := range 20 {
		query = append(query, "q"+strconv.Itoa(i))
	}
	docs := make([]Document, 50)
	for i := range docs {
		docs[i] = Document{ID: strconv.Itoa(i), Text: query[i%20] + " filler words " + query[(i+7)%20]}
	}
	ix := New(docs, Options{})
	q := strings.Join(query, " ")
	res := ix.Search(q, 0)
	if len(res) != len(docs) {
		t.Fatalf("%d results, want %d", len(res), len(docs))
	}
	for _, r := range res {
		if len(r.Terms) != 2 {
			t.Fatalf("%s: Terms %v, want the two terms it holds", r.ID, r.Terms)
		}
	}
	// A two-entry map is two allocations on Go 1.24 to 1.26, so a result
	// costs about 2.2 with Search's own few spread over fifty. Sized by the
	// query, it was 4.2.
	perResult := testing.AllocsPerRun(20, func() { ix.Search(q, 0) }) / float64(len(docs))
	if perResult > 3 {
		t.Fatalf("%.2f allocations per result, want at most 3", perResult)
	}
}
