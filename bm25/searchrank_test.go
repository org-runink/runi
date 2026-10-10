package bm25

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
	"testing/quick"
)

// Search was rewritten to score into a flat slice and to build the per-term
// breakdown only for the documents it returns, instead of allocating a map per
// document it touched. What a search MEANS did not change, so the test for it
// is that the new one and the old one cannot be told apart: same hits, in the
// same order, with scores identical to the bit and breakdowns identical key
// for key.
//
// referenceSearchXYZ is the previous implementation, kept verbatim so the
// comparison is against code rather than against a memory of it.
func referenceSearchXYZ(ix *Index, query string, k int) []Result {
	terms := ix.terms(query)
	if len(terms) == 0 || len(ix.docIDs) == 0 {
		return nil
	}

	scores := make(map[int]float64)
	contrib := make(map[int]map[string]float64)

	for _, term := range terms {
		id, ok := ix.termID[term]
		if !ok {
			continue
		}
		idf := ix.idf(term)
		for _, p := range ix.postings[id] {
			doc := int(p.doc)
			f := float64(p.tf)
			dl := float64(ix.lengths[doc])
			norm := 1.0
			if ix.avgLen > 0 {
				norm = 1 - ix.opts.B + ix.opts.B*dl/ix.avgLen
			}
			s := idf * (f * (ix.opts.K1 + 1)) / (f + ix.opts.K1*norm)
			scores[doc] += s
			if contrib[doc] == nil {
				contrib[doc] = make(map[string]float64)
			}
			contrib[doc][term] += s
		}
	}

	out := make([]Result, 0, len(scores))
	for doc, s := range scores {
		out = append(out, Result{ID: ix.docIDs[doc], Score: s, Terms: contrib[doc]})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].ID < out[b].ID
	})
	if len(out) == 0 {
		return nil
	}
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out
}

// searchSameAsReferenceXYZ fails unless the two searches are indistinguishable.
//
// The old implementation ranks by (score, ID) and sorts unstably, so its order
// is only defined up to documents agreeing on BOTH. Every corpus below gives
// its documents distinct IDs, which makes the order total and the comparison
// exact.
func searchSameAsReferenceXYZ(t *testing.T, what string, ix *Index, queries []string, ks []int) {
	t.Helper()
	for _, q := range queries {
		for _, k := range ks {
			got := ix.Search(q, k)
			want := referenceSearchXYZ(ix, q, k)

			if (got == nil) != (want == nil) {
				t.Fatalf("%s: query %q k=%d returned nil=%v, reference nil=%v",
					what, q, k, got == nil, want == nil)
			}
			if len(got) != len(want) {
				t.Fatalf("%s: query %q k=%d returned %d hits, reference %d",
					what, q, k, len(got), len(want))
			}
			for i := range want {
				if got[i].ID != want[i].ID {
					t.Fatalf("%s: query %q k=%d hit %d is %q, reference %q (order: %s vs %s)",
						what, q, k, i, got[i].ID, want[i].ID, idsOfXYZ(got), idsOfXYZ(want))
				}
				// Bit equality, not a tolerance. A tolerance would hide
				// exactly the bug that summing in a different order causes.
				if got[i].Score != want[i].Score {
					t.Fatalf("%s: query %q k=%d hit %q scored %v (%#x), reference %v (%#x), diff %v",
						what, q, k, got[i].ID, got[i].Score, math.Float64bits(got[i].Score),
						want[i].Score, math.Float64bits(want[i].Score),
						math.Abs(got[i].Score-want[i].Score))
				}
				if len(got[i].Terms) != len(want[i].Terms) {
					t.Fatalf("%s: query %q k=%d hit %q explains %v, reference %v",
						what, q, k, got[i].ID, got[i].Terms, want[i].Terms)
				}
				for term, c := range want[i].Terms {
					gc, ok := got[i].Terms[term]
					if !ok {
						t.Fatalf("%s: query %q k=%d hit %q is missing term %q",
							what, q, k, got[i].ID, term)
					}
					if gc != c {
						t.Fatalf("%s: query %q k=%d hit %q term %q contributed %v, reference %v",
							what, q, k, got[i].ID, term, gc, c)
					}
				}
				// The breakdown must still add up to the score it explains.
				var sum float64
				for _, c := range got[i].Terms {
					sum += c
				}
				if math.Abs(sum-got[i].Score) > 1e-9 {
					t.Fatalf("%s: query %q hit %q: terms sum to %v but score is %v",
						what, q, got[i].ID, sum, got[i].Score)
				}
			}
		}
	}
}

func idsOfXYZ(rs []Result) string {
	ids := make([]string, len(rs))
	for i, r := range rs {
		ids[i] = r.ID
	}
	return strings.Join(ids, ",")
}

var allKsXYZ = []int{-1, 0, 1, 2, 3, 5, 10, 1000}

// The edges a lazily built breakdown and a flat score slice could get wrong,
// named so a failure says which one it was.
func TestSearchRankAgreesOnEdgeCasesXYZ(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("alpha beta gamma ", 2000))

	cases := []struct {
		name    string
		docs    []Document
		queries []string
	}{
		{"ordinary corpus", corpus, []string{
			"cat", "cat mat", "the", "quantum", "cats dogs", "zzz", "cat zzz",
			"cat cat", "the the the", "cat cat mat", "", "  !!!  ",
		}},
		{"empty corpus", nil, []string{"alpha", ""}},
		{"one document", docsOfXYZ("alpha beta"), []string{"alpha", "beta alpha", "zzz"}},
		{"one empty document", docsOfXYZ(""), []string{"", "alpha"}},
		{"empty among real", docsOfXYZ("alpha", "", "beta", ""), []string{"alpha", "alpha beta"}},
		{"single term documents", docsOfXYZ("alpha", "beta", "gamma"), []string{"alpha", "alpha beta gamma"}},
		{"term in every document", docsOfXYZ("alpha", "alpha", "alpha", "alpha", "alpha"),
			[]string{"alpha", "alpha alpha"}},
		{"term in exactly one document", docsOfXYZ("alpha", "beta", "beta", "beta"),
			[]string{"alpha", "beta", "alpha beta"}},
		{"term in half", docsOfXYZ("alpha x", "alpha y", "beta x", "beta y"),
			[]string{"alpha", "x", "alpha x"}},
		{"identical documents", docsOfXYZ("alpha beta", "alpha beta", "alpha beta", "alpha beta"),
			[]string{"alpha", "alpha beta"}},
		{"very long document", docsOfXYZ(long, "alpha", "gamma"), []string{"alpha", "gamma", "alpha gamma"}},
		{"repeated term in a document", docsOfXYZ("alpha alpha alpha alpha", "alpha", "beta"),
			[]string{"alpha", "alpha beta"}},
		{"case only differences", docsOfXYZ("Alpha ALPHA", "alpha", "BETA"),
			[]string{"alpha", "ALPHA", "Beta", "AlPhA beta"}},
		{"non-ASCII", docsOfXYZ("le café naïve", "日本語 テスト", "café quick"),
			[]string{"café", "日本語", "café quick", "café café"}},
		{"ASCII and non-ASCII mixed", docsOfXYZ("the quick brown fox", "le café naïve", "quick café"),
			[]string{"quick", "café", "quick café"}},
		{"one term matches, one does not", docsOfXYZ("alpha", "beta"), []string{"alpha zzz", "zzz alpha"}},
		{"no term matches", docsOfXYZ("alpha", "beta"), []string{"zzz", "zzz yyy"}},
	}

	optsets := []struct {
		name string
		opts Options
	}{
		{"defaults", Options{}},
		{"no length norm", Options{NoLengthNorm: true}},
		{"k1 and b set", Options{K1: 2.5, B: 0.3}},
		{"stopwords", Options{Stopwords: map[string]struct{}{"alpha": {}, "the": {}}}},
		{"custom tokeniser", Options{Tokenise: func(s string) []string { return strings.Fields(strings.ToUpper(s)) }}},
	}

	for _, c := range cases {
		for _, o := range optsets {
			ix := New(c.docs, o.opts)
			searchSameAsReferenceXYZ(t, c.name+" / "+o.name, ix, c.queries, allKsXYZ)
		}
	}
}

// Ties are where a flat scoring pass goes wrong: it reaches documents in a
// different order than a map walk did, so a tie-break that is not really doing
// the work silently flips. Every document here scores exactly the same, and the
// IDs deliberately sort against the document order.
func TestSearchRankBreaksTiesByIDXYZ(t *testing.T) {
	const n = 40
	docs := make([]Document, n)
	for i := range docs {
		// d39, d38, ... d00: document order is the reverse of ID order.
		docs[i] = Document{ID: fmt.Sprintf("d%02d", n-1-i), Text: "alpha beta"}
	}
	ix := New(docs, Options{})

	for _, k := range allKsXYZ {
		got := ix.Search("alpha", k)
		want := referenceSearchXYZ(ix, "alpha", k)
		if len(got) != len(want) {
			t.Fatalf("k=%d: %d hits, reference %d", k, len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].Score != want[i].Score {
				t.Fatalf("k=%d hit %d: %+v, reference %+v", k, i, got[i], want[i])
			}
		}
		// And the order really is by ID ascending, not by document number.
		for i := 1; i < len(got); i++ {
			if got[i-1].ID >= got[i].ID {
				t.Fatalf("k=%d: ties are not in ID order: %s", k, idsOfXYZ(got))
			}
		}
	}

	// Ties among SOME of the documents, with a clear winner and a clear loser,
	// so the tie-break has to work in the middle of the ranking.
	mixed := append([]Document{{ID: "zwinner", Text: "alpha alpha alpha"}}, docs...)
	mixed = append(mixed, Document{ID: "aloser", Text: "alpha " + strings.Repeat("pad ", 50)})
	mi := New(mixed, Options{})
	searchSameAsReferenceXYZ(t, "ties with a winner", mi,
		[]string{"alpha", "alpha beta", "beta"}, allKsXYZ)
	if top := mi.Search("alpha", 1); len(top) != 1 || top[0].ID != "zwinner" {
		t.Fatalf("top hit = %v; want zwinner despite the ID sorting last", top)
	}
}

// The two ways of accumulating a score must agree with each other and with the
// old implementation. A selective query over a corpus large enough to make the
// flat slice a bad deal takes the map; everything else takes the slice.
func TestSearchRankBothAccumulatorsXYZ(t *testing.T) {
	// 4,000 documents, a term in exactly one of them: 4000/64 = 62 > 1, so
	// this is the sparse path.
	const n = 4000
	docs := make([]Document, n)
	for i := range docs {
		docs[i] = Document{ID: fmt.Sprintf("d%05d", i), Text: "filler words here"}
	}
	docs[1234].Text = "needle filler"
	docs[3999].Text = "needle haystack filler words"
	ix := New(docs, Options{})

	if got := ix.Search("needle", 10); len(got) != 2 {
		t.Fatalf("needle matched %d documents; want 2", len(got))
	}
	searchSameAsReferenceXYZ(t, "sparse accumulator", ix,
		[]string{"needle", "haystack", "needle haystack", "needle zzz", "needle needle"}, allKsXYZ)

	// The same corpus with a query that reaches every document takes the
	// dense path: hits is 4,000, and 4000/64 = 62 <= 4000.
	searchSameAsReferenceXYZ(t, "dense accumulator", ix,
		[]string{"filler", "filler words", "filler needle"}, allKsXYZ)

	// Straddle the threshold itself from both sides, in case the arithmetic
	// of the switch is wrong at the boundary.
	for _, hits := range []int{n/denseWhen - 1, n / denseWhen, n/denseWhen + 1} {
		sub := make([]Document, n)
		for i := range sub {
			sub[i] = Document{ID: fmt.Sprintf("s%05d", i), Text: "filler"}
		}
		for i := 0; i < hits && i < n; i++ {
			sub[i].Text = "needle filler"
		}
		si := New(sub, Options{})
		searchSameAsReferenceXYZ(t, fmt.Sprintf("threshold at %d hits", hits), si,
			[]string{"needle", "filler", "needle filler"}, allKsXYZ)
	}
}

// A score of exactly zero would be indistinguishable from "not scored" if the
// score slice were the thing recording which documents were reached. It is
// not -- a separate index does that -- but the invariant is worth asserting:
// a term that every document holds still contributes a positive amount, so
// matching never costs a document anything.
func TestSearchRankContributionsStayPositiveXYZ(t *testing.T) {
	for _, n := range []int{1, 2, 3, 17, 500} {
		docs := make([]Document, n)
		for i := range docs {
			docs[i] = Document{ID: fmt.Sprintf("d%04d", i), Text: "ubiquitous term"}
		}
		ix := New(docs, Options{})
		if idf := ix.idf("ubiquitous"); idf <= 0 {
			t.Fatalf("n=%d: idf of a term in every document = %v; must stay above zero", n, idf)
		}
		got := ix.Search("ubiquitous", 0)
		if len(got) != n {
			t.Fatalf("n=%d: %d hits; every document holds the term", n, len(got))
		}
		for _, r := range got {
			if !(r.Score > 0) {
				t.Fatalf("n=%d: %q scored %v; a contribution is always positive", n, r.ID, r.Score)
			}
			for term, c := range r.Terms {
				if !(c > 0) {
					t.Fatalf("n=%d: %q term %q contributed %v", n, r.ID, term, c)
				}
			}
		}
	}

	// The same at the other extreme: one enormously long document beside
	// short ones drives the length normalisation as far as it goes.
	docs := []Document{
		{ID: "huge", Text: "needle " + strings.Repeat("pad ", 20000)},
		{ID: "tiny", Text: "needle"},
	}
	ix := New(docs, Options{})
	for _, r := range ix.Search("needle", 0) {
		if !(r.Score > 0) {
			t.Fatalf("%q scored %v", r.ID, r.Score)
		}
	}
	searchSameAsReferenceXYZ(t, "extreme lengths", ix, []string{"needle", "pad", "needle pad"}, allKsXYZ)
}

// findPosting is the only part of the breakdown that is not simply the scoring
// loop run again, so it gets its own test: every document in a term's
// postings, and every document not in them.
func TestSearchRankFindPostingXYZ(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 7, 8, 9, 64, 65} {
		post := make([]posting, n)
		for i := range post {
			post[i] = posting{doc: int32(i * 2), tf: int32(i + 1)} // even documents only
		}
		for doc := int32(-1); doc <= int32(2*n+1); doc++ {
			got, ok := findPosting(post, doc)
			wantOK := doc >= 0 && doc%2 == 0 && int(doc/2) < n
			if ok != wantOK {
				t.Fatalf("n=%d: findPosting(%d) found=%v; want %v", n, doc, ok, wantOK)
			}
			if ok && (got.doc != doc || got.tf != doc/2+1) {
				t.Fatalf("n=%d: findPosting(%d) = %+v", n, doc, got)
			}
		}
	}
}

// Randomised corpora and queries, biased towards the shapes that break a
// ranking: a tiny vocabulary so documents tie, repeated query terms, terms
// that match nothing, and k on both sides of the number of hits.
func TestSearchRankAgreesOnRandomCorporaXYZ(t *testing.T) {
	r := rand.New(rand.NewPCG(907, 908))
	words := []string{"alpha", "beta", "gamma", "ALPHA", "a", "1", "café", "the", "zzz"}
	for i := 0; i < 1500; i++ {
		n := 1 + r.IntN(25)
		docs := make([]Document, n)
		for j := range docs {
			w := r.IntN(12)
			parts := make([]string, w)
			for p := range parts {
				parts[p] = words[r.IntN(len(words))]
			}
			docs[j] = Document{ID: fmt.Sprintf("d%03d", j), Text: strings.Join(parts, " ")}
		}
		opts := Options{}
		switch r.IntN(4) {
		case 1:
			opts.NoLengthNorm = true
		case 2:
			opts.Stopwords = map[string]struct{}{"the": {}, "alpha": {}}
		case 3:
			opts.K1, opts.B = 0.5+r.Float64()*2, 0.05+r.Float64()*0.9
		}
		ix := New(docs, opts)

		queries := make([]string, 3)
		for qi := range queries {
			qn := 1 + r.IntN(4)
			qp := make([]string, qn)
			for p := range qp {
				qp[p] = words[r.IntN(len(words))]
			}
			queries[qi] = strings.Join(qp, " ")
		}
		searchSameAsReferenceXYZ(t, fmt.Sprintf("random %d", i), ix, queries,
			[]int{0, 1, 2, r.IntN(n + 2), n, n + 5})
	}
}

// testing/quick over arbitrary strings, which is where the generators above do
// not reach: control bytes, invalid UTF-8, queries of pure punctuation.
func TestSearchRankQuickAgreesWithReferenceXYZ(t *testing.T) {
	f := func(texts []string, q string, k int8) bool {
		if len(texts) > 16 {
			texts = texts[:16]
		}
		docs := make([]Document, len(texts))
		for i, s := range texts {
			docs[i] = Document{ID: fmt.Sprintf("d%03d", i), Text: s}
		}
		for _, opts := range []Options{{}, {NoLengthNorm: true}, {Stopwords: map[string]struct{}{"a": {}}}} {
			ix := New(docs, opts)
			got, want := ix.Search(q, int(k)), referenceSearchXYZ(ix, q, int(k))
			if len(got) != len(want) || (got == nil) != (want == nil) {
				return false
			}
			for i := range want {
				if got[i].ID != want[i].ID || got[i].Score != want[i].Score {
					return false
				}
				if len(got[i].Terms) != len(want[i].Terms) {
					return false
				}
				for term, c := range want[i].Terms {
					if got[i].Terms[term] != c {
						return false
					}
				}
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1500}); err != nil {
		t.Fatal(err)
	}
}
