package toon

import (
	"encoding/json"
	"fmt"
	"testing"
)

func benchDoc(n int) map[string]any {
	rows := make([]any, n)
	for i := range rows {
		rows[i] = map[string]any{
			"id": i, "severity": []string{"low", "high", "medium"}[i%3],
			"file": fmt.Sprintf("internal/pkg%d/file.go", i%40), "line": i % 900,
		}
	}
	return map[string]any{"findings": rows}
}

func BenchmarkEncode(b *testing.B) {
	doc := benchDoc(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeJSONBaseline(b *testing.B) {
	doc := benchDoc(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	text, err := Encode(benchDoc(2000))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out map[string]any
		if err := Decode(text, &out); err != nil {
			b.Fatal(err)
		}
	}
}
