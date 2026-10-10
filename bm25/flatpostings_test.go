package bm25

import (
	"fmt"
	"math"
	"math/bits"
	"math/rand/v2"
	"strings"
	"testing"
	"testing/quick"
)

// The index build was rewritten to read each token once -- splitting it out of
// the document, folding its case and hashing it in the same loop -- and to
// place the postings by counting sort instead of appending into a slice per
// term. Nothing about what an index MEANS changed, so the test for it is that
// the new build and the old one are indistinguishable: same ranking, same
// scores to the bit, over corpora chosen to hit every edge the layout could
// get wrong.
//
// referenceIndexXYZ is the previous implementation of New, kept here verbatim
// so the comparison is against code rather than against a memory of it.
func referenceIndexXYZ(docs []Document, opts Options) *Index {
	if opts.K1 <= 0 {
		opts.K1 = DefaultK1
	}
	switch {
	case opts.NoLengthNorm:
		opts.B = 0
	case opts.B <= 0 || opts.B > 1:
		opts.B = DefaultB
	}
	if opts.Tokenise == nil {
		opts.Tokenise = SimpleTokenise
		opts.simple = true
	}

	ix := &Index{
		opts:    opts,
		docIDs:  make([]string, len(docs)),
		lengths: make([]int, len(docs)),
		termID:  make(map[string]int32),
	}

	var tf []int32
	var seen []int32
	total := 0
	for i, d := range docs {
		ix.docIDs[i] = d.ID

		var terms []string
		if opts.simple {
			terms = SimpleTokenise(d.Text)
		} else {
			terms = opts.Tokenise(d.Text)
		}
		if len(opts.Stopwords) > 0 {
			out := terms[:0:0]
			for _, t := range terms {
				if _, stop := opts.Stopwords[t]; !stop {
					out = append(out, t)
				}
			}
			terms = out
		}
		ix.lengths[i] = len(terms)
		total += len(terms)

		for _, t := range terms {
			id, ok := ix.termID[t]
			if !ok {
				id = int32(len(ix.postings))
				ix.termID[t] = id
				ix.postings = append(ix.postings, nil)
			}
			for int(id) >= len(tf) {
				tf = append(tf, 0)
			}
			if tf[id] == 0 {
				seen = append(seen, id)
			}
			tf[id]++
		}
		for _, id := range seen {
			ix.postings[id] = append(ix.postings[id], posting{doc: int32(i), tf: tf[id]})
			tf[id] = 0
		}
		seen = seen[:0]
	}
	if len(docs) > 0 {
		ix.avgLen = float64(total) / float64(len(docs))
	}
	return ix
}

// sameAsReferenceXYZ fails unless the two builds agree about the corpus and
// about every query put to them.
func sameAsReferenceXYZ(t *testing.T, what string, docs []Document, opts Options, queries []string) {
	t.Helper()

	got := New(docs, opts)
	want := referenceIndexXYZ(docs, opts)

	if got.Len() != want.Len() {
		t.Fatalf("%s: Len %d, reference %d", what, got.Len(), want.Len())
	}
	if got.avgLen != want.avgLen {
		t.Fatalf("%s: avgLen %v, reference %v", what, got.avgLen, want.avgLen)
	}
	for i := range want.lengths {
		if got.lengths[i] != want.lengths[i] {
			t.Fatalf("%s: document %d has length %d, reference %d (%q)",
				what, i, got.lengths[i], want.lengths[i], docs[i].Text)
		}
	}

	// Every term the reference indexed must be indexed here, with the same
	// postings in the same order, and nothing extra may be reachable.
	for term, wid := range want.termID {
		gid, ok := got.termID[term]
		if !ok {
			t.Fatalf("%s: term %q is missing", what, term)
		}
		gp, wp := got.postings[gid], want.postings[wid]
		if len(gp) != len(wp) {
			t.Fatalf("%s: term %q has %d postings, reference %d", what, term, len(gp), len(wp))
		}
		for j := range wp {
			if gp[j] != wp[j] {
				t.Fatalf("%s: term %q posting %d is %+v, reference %+v", what, term, j, gp[j], wp[j])
			}
		}
		if got.idf(term) != want.idf(term) {
			t.Fatalf("%s: term %q idf %v, reference %v", what, term, got.idf(term), want.idf(term))
		}
	}
	for term := range got.termID {
		if _, ok := want.termID[term]; !ok {
			t.Fatalf("%s: term %q was indexed and should not have been", what, term)
		}
	}

	for _, q := range queries {
		for _, k := range []int{0, 1, 3, len(docs) + 1} {
			a, bref := got.Search(q, k), want.Search(q, k)
			if len(a) != len(bref) {
				t.Fatalf("%s: query %q k=%d returned %d hits, reference %d", what, q, k, len(a), len(bref))
			}
			for j := range bref {
				if a[j].ID != bref[j].ID {
					t.Fatalf("%s: query %q k=%d hit %d is %q, reference %q", what, q, k, j, a[j].ID, bref[j].ID)
				}
				// Identical arithmetic in an identical order: bit equality,
				// not a tolerance. A tolerance here would hide exactly the
				// bug a reordered postings list causes.
				if a[j].Score != bref[j].Score {
					t.Fatalf("%s: query %q k=%d hit %q scored %v, reference %v (diff %v)",
						what, q, k, a[j].ID, a[j].Score, bref[j].Score,
						math.Abs(a[j].Score-bref[j].Score))
				}
				if len(a[j].Terms) != len(bref[j].Terms) {
					t.Fatalf("%s: query %q hit %q explains %d terms, reference %d",
						what, q, a[j].ID, len(a[j].Terms), len(bref[j].Terms))
				}
				for term, c := range bref[j].Terms {
					if a[j].Terms[term] != c {
						t.Fatalf("%s: query %q hit %q term %q contributed %v, reference %v",
							what, q, a[j].ID, term, a[j].Terms[term], c)
					}
				}
			}
		}
	}
}

func docsOfXYZ(texts ...string) []Document {
	out := make([]Document, len(texts))
	for i, s := range texts {
		out[i] = Document{ID: fmt.Sprintf("d%d", i), Text: s}
	}
	return out
}

// Every edge the new layout could plausibly get wrong, named so a failure says
// which one it was.
func TestIndexFlatPostingsAgreesOnEdgeCasesXYZ(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("alpha beta gamma ", 2000))
	cases := []struct {
		name    string
		docs    []Document
		queries []string
	}{
		{"empty corpus", nil, []string{"alpha", ""}},
		{"empty corpus, not nil", []Document{}, []string{"alpha"}},
		{"one empty document", docsOfXYZ(""), []string{"", "alpha"}},
		{"all documents empty", docsOfXYZ("", "", ""), []string{"alpha"}},
		{"empty among real", docsOfXYZ("alpha", "", "beta", ""), []string{"alpha", "beta"}},
		{"document of separators only", docsOfXYZ("   !!! ---", "alpha"), []string{"alpha", "!!!"}},
		{"single term documents", docsOfXYZ("alpha", "beta", "gamma"), []string{"alpha", "beta", "delta"}},
		{"one term, every document", docsOfXYZ("alpha", "alpha", "alpha", "alpha"), []string{"alpha"}},
		{"one term, exactly one document", docsOfXYZ("alpha", "beta", "beta", "beta"), []string{"alpha", "beta"}},
		{"term in half the corpus", docsOfXYZ("alpha x", "alpha y", "beta x", "beta y"), []string{"alpha", "x"}},
		{"duplicate identical documents", docsOfXYZ("alpha beta", "alpha beta", "alpha beta"), []string{"alpha beta"}},
		{"very long document", docsOfXYZ(long, "alpha"), []string{"alpha", "gamma"}},
		{"repeated term in one document", docsOfXYZ("alpha alpha alpha alpha alpha", "alpha"), []string{"alpha"}},
		{"terms differing only by case", docsOfXYZ("Alpha ALPHA alpha aLpHa", "BETA beta"), []string{"alpha", "ALPHA", "Beta"}},
		{"case in the query only", docsOfXYZ("alpha beta"), []string{"ALPHA", "Beta BETA"}},
		{"non-ASCII documents", docsOfXYZ("le café naïve über", "日本語 テスト", "Ω ß İstanbul"), []string{"café", "日本語", "ß"}},
		{"ASCII and non-ASCII mixed", docsOfXYZ("the quick brown fox", "le café naïve", "quick café"), []string{"quick", "café", "quick café"}},
		{"non-ASCII after ASCII in one document", docsOfXYZ("hello wörld hello", "hello"), []string{"hello", "wörld"}},
		{"non-ASCII first in one document", docsOfXYZ("ürsula hello hello", "hello"), []string{"hello", "ürsula"}},
		{"non-ASCII separator run", docsOfXYZ("alpha — beta", "alpha"), []string{"alpha", "beta"}},
		{"digits and identifiers", docsOfXYZ("ERR-4021 v2 999", "ERR-4022 v2"), []string{"err 4021", "v2", "4022"}},
		{"duplicate document IDs", []Document{{ID: "same", Text: "alpha"}, {ID: "same", Text: "alpha beta"}}, []string{"alpha", "beta"}},
		{"empty document ID", []Document{{ID: "", Text: "alpha"}, {ID: "z", Text: "alpha"}}, []string{"alpha"}},
	}

	optsets := []struct {
		name string
		opts Options
	}{
		{"defaults", Options{}},
		{"no length norm", Options{NoLengthNorm: true}},
		{"k1 and b set", Options{K1: 2.5, B: 0.3}},
		{"stopwords", Options{Stopwords: map[string]struct{}{"alpha": {}, "the": {}, "café": {}}}},
		{"custom tokeniser", Options{Tokenise: func(s string) []string { return strings.Fields(strings.ToUpper(s)) }}},
		{"custom tokeniser and stopwords", Options{
			Tokenise:  func(s string) []string { return strings.Fields(strings.ToUpper(s)) },
			Stopwords: map[string]struct{}{"ALPHA": {}},
		}},
	}

	for _, c := range cases {
		for _, o := range optsets {
			sameAsReferenceXYZ(t, c.name+" / "+o.name, c.docs, o.opts, c.queries)
		}
	}
}

// A stopword list that swallows the whole corpus leaves an index with no terms
// at all, which is the degenerate case for an arena sized by a count.
func TestIndexFlatPostingsEmptyVocabularyXYZ(t *testing.T) {
	stop := map[string]struct{}{"alpha": {}, "beta": {}}
	ix := New(docsOfXYZ("alpha beta", "beta alpha alpha"), Options{Stopwords: stop})
	if ix.Len() != 2 {
		t.Fatalf("Len = %d; want 2", ix.Len())
	}
	for i, n := range ix.lengths {
		if n != 0 {
			t.Fatalf("document %d has length %d; every term is a stopword", i, n)
		}
	}
	if ix.avgLen != 0 {
		t.Fatalf("avgLen = %v; want 0", ix.avgLen)
	}
	if got := ix.Search("alpha beta", 0); got != nil {
		t.Fatalf("search returned %v; want nothing", got)
	}
	if _, indexed := ix.termID["alpha"]; indexed {
		t.Fatal("a stopword was indexed")
	}
	sameAsReferenceXYZ(t, "all stopwords", docsOfXYZ("alpha beta", "beta alpha alpha"),
		Options{Stopwords: stop}, []string{"alpha", "beta", "gamma"})
}

// The interning table grows by rehashing. A vocabulary large enough to force
// several rehashes must still find every term it interned.
func TestIndexFlatPostingsSurvivesRehashingXYZ(t *testing.T) {
	const terms = 5000
	var sb strings.Builder
	for i := 0; i < terms; i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "term%06d", i)
	}
	docs := docsOfXYZ(sb.String(), "term000000 term004999", "term002500 term002500")
	queries := []string{"term000000", "term004999", "term002500", "term000000 term002500 term004999"}
	sameAsReferenceXYZ(t, "wide vocabulary", docs, Options{}, queries)

	ix := New(docs, Options{})
	if len(ix.termID) != terms {
		t.Fatalf("indexed %d terms; want %d", len(ix.termID), terms)
	}
	for id, p := range ix.postings {
		if len(p) == 0 {
			t.Fatalf("term id %d has no postings", id)
		}
	}
}

// Tokens longer than eight bytes share a hash when they share a suffix, which
// is the case the probe loop exists for. Build a corpus of nothing but those.
func TestIndexFlatPostingsHandlesHashCollisionsXYZ(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		// Same last eight bytes, different prefix: distinct terms whose
		// rotate-and-xor hashes are forced close together.
		fmt.Fprintf(&sb, "prefix%03dsameending", i)
	}
	docs := docsOfXYZ(sb.String(), "prefix000sameending prefix399sameending", "prefix200sameending")
	sameAsReferenceXYZ(t, "colliding suffixes", docs, Options{},
		[]string{"prefix000sameending", "prefix200sameending", "prefix399sameending", "sameending"})
}

// A randomised corpus, biased hard towards the boundaries: short documents,
// repeated terms, a tiny vocabulary so terms saturate, non-ASCII, and case.
func TestIndexFlatPostingsAgreesOnRandomCorporaXYZ(t *testing.T) {
	r := rand.New(rand.NewPCG(905, 906))
	words := []string{
		"alpha", "ALPHA", "Alpha", "beta", "gamma", "a", "1", "x9",
		"café", "über", "日本語", "err-4021", "the", "longertokenthaneightbytes",
		"longertokenthateightbytes", "", "   ", "!!!",
	}
	for i := 0; i < 2000; i++ {
		n := r.IntN(8)
		docs := make([]Document, n)
		for j := range docs {
			w := r.IntN(10)
			parts := make([]string, w)
			for p := range parts {
				parts[p] = words[r.IntN(len(words))]
			}
			docs[j] = Document{ID: fmt.Sprintf("d%d", j), Text: strings.Join(parts, " ")}
		}
		queries := []string{
			words[r.IntN(len(words))],
			words[r.IntN(len(words))] + " " + words[r.IntN(len(words))],
		}
		opts := Options{}
		switch r.IntN(4) {
		case 1:
			opts.NoLengthNorm = true
		case 2:
			opts.Stopwords = map[string]struct{}{"alpha": {}, "the": {}}
		case 3:
			opts.K1, opts.B = 0.5+r.Float64()*2, 0.1+r.Float64()*0.8
		}
		sameAsReferenceXYZ(t, fmt.Sprintf("random corpus %d", i), docs, opts, queries)
	}
}

// testing/quick over arbitrary strings, which is where the generators above do
// not reach: control bytes, lone continuation bytes, invalid UTF-8.
func TestIndexFlatPostingsQuickAgreesWithReferenceXYZ(t *testing.T) {
	f := func(texts []string, q string) bool {
		if len(texts) > 24 {
			texts = texts[:24]
		}
		docs := make([]Document, len(texts))
		for i, s := range texts {
			docs[i] = Document{ID: fmt.Sprintf("d%d", i), Text: s}
		}
		for _, opts := range []Options{
			{},
			{NoLengthNorm: true},
			{Stopwords: map[string]struct{}{"a": {}}},
		} {
			got, want := New(docs, opts), referenceIndexXYZ(docs, opts)
			if len(got.termID) != len(want.termID) {
				return false
			}
			for i := range want.lengths {
				if got.lengths[i] != want.lengths[i] {
					return false
				}
			}
			for term, wid := range want.termID {
				gid, ok := got.termID[term]
				if !ok {
					return false
				}
				gp, wp := got.postings[gid], want.postings[wid]
				if len(gp) != len(wp) {
					return false
				}
				for j := range wp {
					if gp[j] != wp[j] {
						return false
					}
				}
			}
			a, b := got.Search(q, 5), want.Search(q, 5)
			if len(a) != len(b) {
				return false
			}
			for j := range b {
				if a[j].ID != b[j].ID || a[j].Score != b[j].Score {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatal(err)
	}
}

// hashString and the scanner must agree, or a term reached by one route would
// not be found by the other -- the bug that splits a term into two ids and
// halves its document frequency.
func TestIndexFlatPostingsHashRoutesAgreeXYZ(t *testing.T) {
	for _, s := range []string{"", "a", "alpha", "term0001", "eightbyt", "ninebytes",
		"longertokenthaneightbytes", "0", "9999999999999999"} {
		var h uint64
		for i := 0; i < len(s); i++ {
			h = bits.RotateLeft64(h, 8) ^ uint64(s[i])
		}
		if got := hashString(s); got != h {
			t.Fatalf("hashString(%q) = %#x; want %#x", s, got, h)
		}
	}

	// A probe is settled by comparing the strings, so a collision costs a
	// probe and never a wrong answer -- but a hash that collides often would
	// turn the table into a list. No two tokens of eight bytes or fewer may
	// share a hash at all.
	seen := map[uint64]string{}
	var gen func(prefix string, depth int)
	alpha := "aAz09_- "
	gen = func(prefix string, depth int) {
		h := hashString(prefix)
		if other, dup := seen[h]; dup && other != prefix {
			t.Fatalf("%q and %q share hash %#x", other, prefix, h)
		}
		seen[h] = prefix
		if depth == 0 {
			return
		}
		for _, c := range []byte(alpha) {
			gen(prefix+string(c), depth-1)
		}
	}
	gen("", 5)

	// The same token reached through scanASCII and through addTokens must come
	// back as one id, not two.
	ix := New([]Document{
		{ID: "ascii", Text: "shared alpha"},
		{ID: "unicode", Text: "shared café"},
	}, Options{})
	id, ok := ix.termID["shared"]
	if !ok {
		t.Fatal("term indexed by neither route")
	}
	if n := len(ix.postings[id]); n != 2 {
		t.Fatalf("shared term has %d postings; want 2 -- the two tokenising routes disagreed", n)
	}
}
