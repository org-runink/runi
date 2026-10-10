package bm25

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

func benchCorpus(n int) []Document {
	r := rand.New(rand.NewPCG(1, 2))
	vocab := make([]string, 400)
	for i := range vocab {
		vocab[i] = fmt.Sprintf("term%d", i)
	}
	out := make([]Document, n)
	for i := range out {
		w := make([]string, 60+r.IntN(60))
		for j := range w {
			w[j] = vocab[r.IntN(len(vocab))]
		}
		out[i] = Document{ID: fmt.Sprintf("d%d", i), Text: strings.Join(w, " ")}
	}
	return out
}

func BenchmarkIndexBuild(b *testing.B) {
	docs := benchCorpus(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New(docs, Options{})
	}
}
