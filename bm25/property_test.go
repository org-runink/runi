package bm25

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

// A ranker is easy to test on an example and hard to trust on a corpus. These
// properties hold for any corpus and any query, so they also cover the term
// interning and the flat postings layout the fast path uses -- a mis-sized
// posting slice would show up here as a score that no longer matches the
// explanation beside it.

var vocab = strings.Fields("alpha beta gamma delta epsilon zeta eta theta iota kappa")

func randCorpus(r *rand.Rand, docs int) []Document {
	out := make([]Document, docs)
	for i := range out {
		words := make([]string, 1+r.IntN(30))
		for j := range words {
			words[j] = vocab[r.IntN(len(vocab))]
		}
		out[i] = Document{ID: fmt.Sprintf("d%d", i), Text: strings.Join(words, " ")}
	}
	return out
}

func randQuery(r *rand.Rand) string {
	terms := make([]string, 1+r.IntN(3))
	for i := range terms {
		terms[i] = vocab[r.IntN(len(vocab))]
	}
	return strings.Join(terms, " ")
}

// Results come back best-first, at most k of them, with finite positive
// scores. A caller that takes Results[0] is relying on all four.
func TestPropertyResultsAreOrderedBoundedAndFinite(t *testing.T) {
	r := rand.New(rand.NewPCG(61, 62))
	for i := 0; i < 1000; i++ {
		ix := New(randCorpus(r, 1+r.IntN(40)), Options{})
		k := 1 + r.IntN(10)
		res := ix.Search(randQuery(r), k)
		if len(res) > k {
			t.Fatalf("asked for %d, got %d", k, len(res))
		}
		for j, got := range res {
			if math.IsNaN(got.Score) || math.IsInf(got.Score, 0) {
				t.Fatalf("score %v is not a number", got.Score)
			}
			if got.Score <= 0 {
				t.Fatalf("returned a hit scoring %v", got.Score)
			}
			if j > 0 && res[j-1].Score < got.Score {
				t.Fatalf("out of order: %v before %v", res[j-1].Score, got.Score)
			}
		}
	}
}

// The per-term breakdown must add up to the score it explains. This is the
// difference between a ranking that can be audited and one that must be taken
// on faith, and it is the first thing a partial-sum bug would break.
func TestPropertyTermsSumToTheScore(t *testing.T) {
	r := rand.New(rand.NewPCG(63, 64))
	for i := 0; i < 1000; i++ {
		ix := New(randCorpus(r, 1+r.IntN(40)), Options{})
		for _, got := range ix.Search(randQuery(r), 10) {
			var sum float64
			for _, c := range got.Terms {
				sum += c
			}
			if math.Abs(sum-got.Score) > 1e-9 {
				t.Fatalf("terms sum to %v but score is %v (%v)", sum, got.Score, got.Terms)
			}
		}
	}
}

// Every hit contains at least one query term. BM25 cannot score a document
// that shares no term with the query, so a hit that does is an indexing bug.
func TestPropertyEveryHitSharesATermWithTheQuery(t *testing.T) {
	r := rand.New(rand.NewPCG(65, 66))
	for i := 0; i < 1000; i++ {
		docs := randCorpus(r, 1+r.IntN(40))
		byID := map[string]string{}
		for _, d := range docs {
			byID[d.ID] = d.Text
		}
		ix := New(docs, Options{})
		q := randQuery(r)
		qterms := SimpleTokenise(q)
		for _, got := range ix.Search(q, 10) {
			found := false
			for _, term := range SimpleTokenise(byID[got.ID]) {
				for _, qt := range qterms {
					if term == qt {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("%q matched %q with no shared term", got.ID, q)
			}
		}
	}
}

// Two identical documents must score identically. Interning assigns them
// different document slots, so this is a real check that a slot's identity
// never leaks into its score.
func TestPropertyIdenticalDocumentsScoreIdentically(t *testing.T) {
	r := rand.New(rand.NewPCG(67, 68))
	for i := 0; i < 1000; i++ {
		docs := randCorpus(r, 1+r.IntN(20))
		twin := Document{ID: "twin", Text: docs[0].Text}
		ix := New(append(docs, twin), Options{})
		q := randQuery(r)
		res := ix.Search(q, len(docs)+1)

		var a, b float64
		var seenA, seenB bool
		for _, got := range res {
			if got.ID == docs[0].ID {
				a, seenA = got.Score, true
			}
			if got.ID == "twin" {
				b, seenB = got.Score, true
			}
		}
		if seenA != seenB {
			t.Fatalf("one twin matched %q and the other did not", q)
		}
		if seenA && math.Abs(a-b) > 1e-12 {
			t.Fatalf("identical documents scored %v and %v", a, b)
		}
	}
}

// Searching is read-only: the same query on the same index gives the same
// answer every time, including the order of equal scores.
func TestPropertySearchIsDeterministic(t *testing.T) {
	r := rand.New(rand.NewPCG(69, 70))
	for i := 0; i < 500; i++ {
		ix := New(randCorpus(r, 1+r.IntN(30)), Options{})
		q := randQuery(r)
		first := ix.Search(q, 8)
		for rep := 0; rep < 3; rep++ {
			again := ix.Search(q, 8)
			if len(again) != len(first) {
				t.Fatalf("%d hits then %d", len(first), len(again))
			}
			for j := range first {
				if again[j].ID != first[j].ID || again[j].Score != first[j].Score {
					t.Fatalf("hit %d changed: %+v then %+v", j, first[j], again[j])
				}
			}
		}
	}
}

// Padding a document with terms the query never asks about can only lower its
// score, never raise it. This is what length normalisation is for, and it is
// the property a keyword-stuffed document would violate.
func TestPropertyPaddingNeverHelps(t *testing.T) {
	r := rand.New(rand.NewPCG(71, 72))
	for i := 0; i < 1000; i++ {
		q := vocab[r.IntN(3)] // query one of the first three terms
		body := q + " " + strings.Join([]string{vocab[3], vocab[4]}, " ")
		pad := strings.Repeat(" "+vocab[9], 1+r.IntN(30)) // never queried

		docs := append(randCorpus(r, 1+r.IntN(10)),
			Document{ID: "plain", Text: body},
			Document{ID: "padded", Text: body + pad},
		)
		ix := New(docs, Options{})

		var plain, padded float64
		for _, got := range ix.Search(q, len(docs)) {
			switch got.ID {
			case "plain":
				plain = got.Score
			case "padded":
				padded = got.Score
			}
		}
		if padded > plain+1e-12 {
			t.Fatalf("padding raised the score: plain %v, padded %v", plain, padded)
		}
	}
}

// IDF is never negative, whatever fraction of the corpus holds a term. The
// textbook formula goes negative past half the corpus, which would make a
// common term subtract from a score and reorder results nonsensically.
func TestPropertyScoresStayPositiveAsATermSaturates(t *testing.T) {
	r := rand.New(rand.NewPCG(73, 74))
	for n := 1; n <= 40; n++ {
		for hits := 1; hits <= n; hits++ {
			docs := make([]Document, n)
			for i := range docs {
				if i < hits {
					docs[i] = Document{ID: fmt.Sprintf("d%d", i), Text: "alpha beta"}
				} else {
					docs[i] = Document{ID: fmt.Sprintf("d%d", i), Text: "gamma delta"}
				}
			}
			ix := New(docs, Options{})
			for _, got := range ix.Search("alpha", n) {
				if got.Score < 0 {
					t.Fatalf("n=%d hits=%d: negative score %v", n, hits, got.Score)
				}
			}
			_ = r
		}
	}
}
