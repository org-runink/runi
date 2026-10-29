// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// models.toml is a realistic sampling configuration: eight models, each an
// array-of-tables entry with a multi-line prompt, a sampling table and a
// limits table, plus defaults with an inline table.
func benchInput(b testing.TB) []byte {
	data, err := os.ReadFile("testdata/models.toml")
	if err != nil {
		b.Fatal(err)
	}
	return data
}

type benchSampling struct {
	Temperature       float64  `toml:"temperature" json:"temperature"`
	TopP              float64  `toml:"top_p" json:"top_p"`
	TopK              int      `toml:"top_k" json:"top_k"`
	MinP              float64  `toml:"min_p" json:"min_p"`
	RepetitionPenalty float64  `toml:"repetition_penalty" json:"repetition_penalty"`
	PresencePenalty   float64  `toml:"presence_penalty" json:"presence_penalty"`
	FrequencyPenalty  float64  `toml:"frequency_penalty" json:"frequency_penalty"`
	MaxTokens         int      `toml:"max_tokens" json:"max_tokens"`
	Seed              int64    `toml:"seed" json:"seed"`
	Stop              []string `toml:"stop" json:"stop"`
}

type benchModel struct {
	Name     string        `toml:"name" json:"name"`
	ID       string        `toml:"id" json:"id"`
	Tags     []string      `toml:"tags" json:"tags"`
	Enabled  bool          `toml:"enabled" json:"enabled"`
	System   string        `toml:"system" json:"system"`
	Sampling benchSampling `toml:"sampling" json:"sampling"`
	Limits   struct {
		Context  int     `toml:"context" json:"context"`
		MaxBatch int     `toml:"max_batch" json:"max_batch"`
		Timeout  float64 `toml:"timeout_s" json:"timeout_s"`
	} `toml:"limits" json:"limits"`
}

type benchConfig struct {
	Version  int       `toml:"version" json:"version"`
	Updated  time.Time `toml:"updated" json:"updated"`
	Defaults struct {
		Temperature float64        `toml:"temperature" json:"temperature"`
		TopP        float64        `toml:"top_p" json:"top_p"`
		MaxTokens   int            `toml:"max_tokens" json:"max_tokens"`
		Stream      bool           `toml:"stream" json:"stream"`
		Stop        []string       `toml:"stop" json:"stop"`
		Retry       map[string]any `toml:"retry" json:"retry"`
	} `toml:"defaults" json:"defaults"`
	Models []benchModel `toml:"models" json:"models"`
}

func TestBenchInputDecodes(t *testing.T) {
	var cfg benchConfig
	if err := unmarshal(benchInput(t), &cfg, true); err != nil { // strict: every key has a field
		t.Fatal(err)
	}
	if len(cfg.Models) != 8 || cfg.Models[2].Name != "coder" || cfg.Models[2].Sampling.Seed != 0x5EED ||
		cfg.Models[7].Limits.Timeout != 150 || cfg.Defaults.Retry["attempts"] != int64(3) {
		t.Fatalf("got %+v", cfg)
	}
}

func BenchmarkParse(b *testing.B) {
	data := benchInput(b)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStruct(b *testing.B) {
	data := benchInput(b)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var cfg benchConfig
		if err := Unmarshal(data, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalMap(b *testing.B) {
	data := benchInput(b)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var cfg map[string]any
		if err := Unmarshal(data, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJSONBaseline decodes the same configuration, written as JSON,
// with encoding/json into the same struct: the cost of the format the
// standard library already reads.
func BenchmarkJSONBaseline(b *testing.B) {
	var cfg benchConfig
	if err := Unmarshal(benchInput(b), &cfg); err != nil {
		b.Fatal(err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out benchConfig
		if err := json.Unmarshal(data, &out); err != nil {
			b.Fatal(err)
		}
	}
}

// The README states these allocation counts for reading the sampling
// configuration in testdata: 703 to Parse it, 647 to decode it into structs
// and 713 into a map. Allocation counts are deterministic where timings are
// not, so they are what is pinned; a change that gave back the parser's
// savings (interned keys, scanned small tables, integers read without a
// string) would break these budgets.
func TestAllocationBudgets(t *testing.T) {
	data := benchInput(t)
	for _, tc := range []struct {
		name   string
		budget float64
		run    func()
	}{
		{"Parse", 720, func() { _, _ = Parse(data) }},
		{"Unmarshal into structs", 665, func() { var c benchConfig; _ = Unmarshal(data, &c) }},
		{"Unmarshal into a map", 730, func() { var c map[string]any; _ = Unmarshal(data, &c) }},
	} {
		if got := testing.AllocsPerRun(5, tc.run); got > tc.budget {
			t.Errorf("%s: %.0f allocations, budget %.0f", tc.name, got, tc.budget)
		}
	}
}
