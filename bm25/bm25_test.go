package bm25

import (
	"math"
	"strings"
	"testing"
)

var corpus = []Document{
	{ID: "a", Text: "the cat sat on the mat"},
	{ID: "b", Text: "the dog sat on the log"},
	{ID: "c", Text: "cats and dogs living together"},
	{ID: "d", Text: "a treatise on the feeding habits of the domestic cat, at length, covering the cat in detail"},
	{ID: "e", Text: "quantum chromodynamics"},
}

func TestSearchRanksTheObviousAnswerFirst(t *testing.T) {
	ix := New(corpus, Options{})
	got := ix.Search("cat", 0)
	if len(got) == 0 {
		t.Fatal("no results for a term that is in the corpus")
	}
	if got[0].ID != "a" && got[0].ID != "d" {
		t.Fatalf("top hit for \"cat\" = %q; want a or d", got[0].ID)
	}
	for _, r := range got {
		if r.ID == "e" {
			t.Fatal("a document sharing no terms was returned")
		}
	}
}

func TestRarerTermsOutrankCommonOnes(t *testing.T) {
	// "quantum" appears in one document; "the" appears in most. A document
	// matching the rare term must outrank one matching only the common term.
	ix := New(corpus, Options{})
	rare := ix.Search("quantum", 1)
	common := ix.Search("the", 1)
	if len(rare) == 0 || len(common) == 0 {
		t.Fatal("expected hits for both queries")
	}
	if rare[0].Score <= common[0].Score {
		t.Fatalf("rare term scored %v, common term %v; rarity must win",
			rare[0].Score, common[0].Score)
	}
}

func TestLengthNormalisationPenalisesPadding(t *testing.T) {
	// Same term, same count, different document lengths. The shorter document
	// must win: that is what b controls.
	docs := []Document{
		{ID: "short", Text: "widget"},
		{ID: "long", Text: "widget " + strings.Repeat("filler ", 200)},
	}
	ix := New(docs, Options{})
	got := ix.Search("widget", 0)
	if len(got) != 2 {
		t.Fatalf("got %d results; want 2", len(got))
	}
	if got[0].ID != "short" {
		t.Fatalf("top hit = %q; the shorter document must rank higher", got[0].ID)
	}
}

func TestBZeroDisablesLengthNormalisation(t *testing.T) {
	docs := []Document{
		{ID: "short", Text: "widget"},
		{ID: "long", Text: "widget " + strings.Repeat("filler ", 200)},
	}
	ix := New(docs, Options{NoLengthNorm: true})
	got := ix.Search("widget", 0)
	if len(got) != 2 {
		t.Fatalf("got %d results; want 2", len(got))
	}
	if math.Abs(got[0].Score-got[1].Score) > 1e-12 {
		t.Fatalf("with length normalisation off the scores should match: %v vs %v",
			got[0].Score, got[1].Score)
	}

	// And the zero value must NOT disable it: that was a real bug.
	def := New(docs, Options{})
	dg := def.Search("widget", 0)
	if math.Abs(dg[0].Score-dg[1].Score) < 1e-9 {
		t.Fatal("Options{} disabled length normalisation; the zero value must mean the default")
	}
}

func TestIDFNeverGoesNegative(t *testing.T) {
	// A term in EVERY document is the case where the textbook IDF turns
	// negative and matching starts to hurt. It must not here.
	docs := make([]Document, 10)
	for i := range docs {
		docs[i] = Document{ID: string(rune('a' + i)), Text: "ubiquitous term here"}
	}
	ix := New(docs, Options{})
	if idf := ix.idf("ubiquitous"); idf < 0 {
		t.Fatalf("idf of a term in every document = %v; must stay >= 0", idf)
	}
	for _, r := range ix.Search("ubiquitous", 0) {
		if r.Score < 0 {
			t.Fatalf("document %q scored %v; matching must never reduce a score", r.ID, r.Score)
		}
	}
}

func TestTermsExplainTheScore(t *testing.T) {
	ix := New(corpus, Options{})
	got := ix.Search("cat mat", 0)
	if len(got) == 0 {
		t.Fatal("no results")
	}
	top := got[0]
	if len(top.Terms) == 0 {
		t.Fatal("Terms is empty; a result must be explainable")
	}
	var sum float64
	for term, c := range top.Terms {
		if term != "cat" && term != "mat" {
			t.Fatalf("Terms contains %q, which is not a query term", term)
		}
		sum += c
	}
	if math.Abs(sum-top.Score) > 1e-9 {
		t.Fatalf("term contributions sum to %v but the score is %v", sum, top.Score)
	}
}

func TestTopKAndOrderingAreStable(t *testing.T) {
	ix := New(corpus, Options{})
	all := ix.Search("the", 0)
	top2 := ix.Search("the", 2)
	if len(top2) != 2 {
		t.Fatalf("k=2 returned %d results", len(top2))
	}
	for i := range top2 {
		if top2[i].ID != all[i].ID {
			t.Fatalf("top-k disagrees with the full ranking at %d: %q vs %q", i, top2[i].ID, all[i].ID)
		}
	}
	// k larger than the number of hits returns what there is.
	if got := ix.Search("the", 999); len(got) != len(all) {
		t.Fatalf("oversized k returned %d; want %d", len(got), len(all))
	}
	// Repeated searches must not reorder: ties break by ID.
	for i := 0; i < 20; i++ {
		again := ix.Search("sat on", 0)
		for j := range again {
			if again[j].ID != ix.Search("sat on", 0)[j].ID {
				t.Fatal("ranking is not stable across calls")
			}
		}
	}
}

func TestEmptyCasesReturnNothing(t *testing.T) {
	ix := New(corpus, Options{})
	if got := ix.Search("", 0); got != nil {
		t.Fatalf("empty query returned %v; want nil", got)
	}
	if got := ix.Search("   !!! ", 0); got != nil {
		t.Fatalf("query of only separators returned %v; want nil", got)
	}
	if got := ix.Search("zzzznotpresent", 0); got != nil {
		t.Fatalf("query with no matching term returned %v; want nil", got)
	}

	empty := New(nil, Options{})
	if empty.Len() != 0 {
		t.Fatalf("Len = %d; want 0", empty.Len())
	}
	if got := empty.Search("anything", 0); got != nil {
		t.Fatalf("search of an empty index returned %v; want nil", got)
	}
}

func TestEmptyDocumentsAreIndexedButNeverMatch(t *testing.T) {
	ix := New([]Document{{ID: "blank", Text: ""}, {ID: "real", Text: "content"}}, Options{})
	if ix.Len() != 2 {
		t.Fatalf("Len = %d; want 2 — an empty document is still a document", ix.Len())
	}
	for _, r := range ix.Search("content", 0) {
		if r.ID == "blank" {
			t.Fatal("an empty document matched")
		}
	}
}

func TestSimpleTokenise(t *testing.T) {
	got := SimpleTokenise("ERR-4021: Widget v2 failed, twice!")
	want := []string{"err", "4021", "widget", "v2", "failed", "twice"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d = %q; want %q", i, got[i], want[i])
		}
	}
	if got := SimpleTokenise(""); len(got) != 0 {
		t.Fatalf("tokenising empty text = %v; want nothing", got)
	}
}

func TestExactIdentifiersAreDistinguished(t *testing.T) {
	// The case the package documentation claims BM25 handles and an embedding
	// does not: two near-identical error codes must not be interchangeable.
	docs := []Document{
		{ID: "4021", Text: "ERR-4021 disk controller timeout"},
		{ID: "4022", Text: "ERR-4022 network controller timeout"},
	}
	ix := New(docs, Options{})
	got := ix.Search("ERR-4022", 1)
	if len(got) == 0 {
		t.Fatal("no result")
	}
	if got[0].ID != "4022" {
		t.Fatalf("top hit = %q; want the exact code 4022", got[0].ID)
	}
}

func TestStopwordsAreRemovedFromBothSides(t *testing.T) {
	stop := map[string]struct{}{"the": {}, "on": {}}
	ix := New(corpus, Options{Stopwords: stop})

	if got := ix.Search("the", 0); got != nil {
		t.Fatalf("a stopword query returned %v; want nothing", got)
	}
	if _, indexed := ix.termID["the"]; indexed {
		t.Fatal("a stopword was indexed")
	}
	// Non-stopword search still works.
	if got := ix.Search("cat", 0); len(got) == 0 {
		t.Fatal("stopwords broke ordinary search")
	}
}

func TestCustomTokeniserAppliesToQueriesToo(t *testing.T) {
	// If a custom tokeniser were applied only to documents, nothing would ever
	// match. This asserts both sides go through it.
	upper := func(s string) []string { return strings.Fields(strings.ToUpper(s)) }
	ix := New([]Document{{ID: "x", Text: "hello world"}}, Options{Tokenise: upper})
	got := ix.Search("hello", 0)
	if len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("custom tokeniser result = %v; want one hit on x", got)
	}
}

func TestInvalidOptionsFallBackToDefaults(t *testing.T) {
	for _, o := range []Options{{K1: -1}, {K1: 0}, {B: -0.5}, {B: 1.5}, {B: 0}} {
		ix := New(corpus, o)
		if ix.opts.K1 <= 0 {
			t.Fatalf("K1 = %v; want the default", ix.opts.K1)
		}
		if ix.opts.B < 0 || ix.opts.B > 1 {
			t.Fatalf("B = %v; want the default", ix.opts.B)
		}
		if got := ix.Search("cat", 1); len(got) == 0 {
			t.Fatal("index with corrected options returned nothing")
		}
	}
}

func TestRepeatedTermsSaturate(t *testing.T) {
	// k1 bounds how much repetition helps: ten occurrences must not score ten
	// times one occurrence.
	docs := []Document{
		{ID: "one", Text: "widget " + strings.Repeat("pad ", 9)},
		{ID: "ten", Text: strings.Repeat("widget ", 10)},
	}
	ix := New(docs, Options{})
	got := ix.Search("widget", 0)
	if len(got) != 2 {
		t.Fatalf("got %d results; want 2", len(got))
	}
	var one, ten float64
	for _, r := range got {
		if r.ID == "one" {
			one = r.Score
		} else {
			ten = r.Score
		}
	}
	if ten <= one {
		t.Fatalf("more occurrences scored lower: ten=%v one=%v", ten, one)
	}
	if ten > 10*one {
		t.Fatalf("ten occurrences scored %v against %v for one; saturation is not happening", ten, one)
	}
}
