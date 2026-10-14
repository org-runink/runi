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

func BenchmarkDecodeJSONBaseline(b *testing.B) {
	jb, err := json.Marshal(benchDoc(2000))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out map[string]any
		if err := json.Unmarshal(jb, &out); err != nil {
			b.Fatal(err)
		}
	}
}

type benchFinding struct {
	ID       int     `json:"id"`
	Severity string  `json:"severity"`
	File     string  `json:"file"`
	Line     int     `json:"line"`
	Score    float64 `json:"score"`
}

type benchReport struct {
	Findings []benchFinding `json:"findings"`
}

func benchStructs(n int) benchReport {
	r := benchReport{Findings: make([]benchFinding, n)}
	for i := range r.Findings {
		r.Findings[i] = benchFinding{ID: i, Severity: []string{"low", "high", "medium"}[i%3],
			File: fmt.Sprintf("internal/pkg%d/file.go", i%40), Line: i % 900, Score: float64(i%97) / 8}
	}
	return r
}

func BenchmarkEncodeStruct(b *testing.B) {
	r := benchStructs(2000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeStruct(b *testing.B) {
	text, err := Encode(benchStructs(2000))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out benchReport
		if err := Decode(text, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeStructTab(b *testing.B) {
	r := benchStructs(2000)
	o := Options{Delimiter: '\t'}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := EncodeWith(r, o); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeStructJSONBaseline(b *testing.B) {
	r := benchStructs(2000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeStructJSONBaseline(b *testing.B) {
	jb, err := json.Marshal(benchStructs(2000))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out benchReport
		if err := json.Unmarshal(jb, &out); err != nil {
			b.Fatal(err)
		}
	}
}
