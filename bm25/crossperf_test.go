package bm25

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// crossCorpusXYZ reproduces the corpus shape benchmarks/crossbench/main.go
// builds for bm25_index_s and bm25_query_s: 5,000 documents of 120 words drawn
// from a 2,000-term vocabulary, and 200 three-term queries.
func crossCorpusXYZ(n, words int) ([]Document, []string) {
	r := rand.New(rand.NewPCG(7, 11))
	vocab := make([]string, 2000)
	for i := range vocab {
		vocab[i] = fmt.Sprintf("term%04d", i)
	}
	docs := make([]Document, n)
	for i := range docs {
		b := make([]byte, 0, words*9)
		for w := 0; w < words; w++ {
			if w > 0 {
				b = append(b, ' ')
			}
			b = append(b, vocab[r.IntN(len(vocab))]...)
		}
		docs[i] = Document{ID: fmt.Sprint(i), Text: string(b)}
	}
	queries := make([]string, 200)
	for i := range queries {
		queries[i] = fmt.Sprintf("term%04d term%04d term%04d", r.IntN(2000), r.IntN(2000), r.IntN(2000))
	}
	return docs, queries
}

func BenchmarkCrossIndexBuildXYZ(b *testing.B) {
	docs, _ := crossCorpusXYZ(5000, 120)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New(docs, Options{})
	}
}

func BenchmarkCrossQueryXYZ(b *testing.B) {
	docs, queries := crossCorpusXYZ(5000, 120)
	ix := New(docs, Options{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			_ = ix.Search(q, 10)
		}
	}
}

// The same queries asking for every hit rather than the top ten: the case that
// gains least from building a breakdown only for what is returned.
func BenchmarkCrossQueryAllXYZ(b *testing.B) {
	docs, queries := crossCorpusXYZ(5000, 120)
	ix := New(docs, Options{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			_ = ix.Search(q, 0)
		}
	}
}

// A selective query over a corpus large enough that a score slice the size of
// it would cost more than the search does: the case the accumulator switch
// exists for.
func BenchmarkCrossQuerySelectiveXYZ(b *testing.B) {
	docs, _ := crossCorpusXYZ(200000, 12)
	for i := range docs {
		if i%20000 == 0 {
			docs[i].Text += " needle"
		}
	}
	ix := New(docs, Options{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 200; j++ {
			_ = ix.Search("needle", 10)
		}
	}
}
