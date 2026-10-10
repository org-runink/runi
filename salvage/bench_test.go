// SPDX-License-Identifier: BSD-3-Clause

package salvage

import (
	"fmt"
	"testing"
)

// The published comparison decodes 2,000 model replies, each one prose around
// a fenced JSON object, into a struct. These benchmarks are that workload, one
// reply at a time, so the figure can be reproduced with `go test -bench .`
// instead of a harness.

type benchVerdict struct {
	ID      int     `json:"id"`
	Verdict string  `json:"verdict"`
	Score   float64 `json:"score"`
	Notes   string  `json:"notes"`
}

func benchReplies(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(
			"Sure — here is the result you asked for.\n\n```json\n"+
				`{"id":%d,"verdict":"pass","score":%0.3f,"notes":"line %d of the reply"}`+
				"\n```\n\nLet me know if you need anything else.", i, float64(i)*0.37, i)
	}
	return out
}

var benchTexts = benchReplies(2000)

func BenchmarkDecodeReply(b *testing.B) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		var v benchVerdict
		if err := Decode(benchTexts[i%len(benchTexts)], &v); err != nil {
			b.Fatal(err)
		}
	}
}

// The same replies with the fast path switched off: what the package costs
// when a reply is one it hands to encoding/json.
func BenchmarkDecodeReplyGeneralPath(b *testing.B) {
	useFast = false
	defer func() { useFast = true }()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		var v benchVerdict
		if err := Decode(benchTexts[i%len(benchTexts)], &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScanReply(b *testing.B) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		Scan(benchTexts[i%len(benchTexts)])
	}
}

// A reply with no JSON in it at all: the whole text is prose, and the cost is
// the scan that finds nothing.
func BenchmarkDecodeProse(b *testing.B) {
	const prose = "I could not produce a card for this request, because the " +
		"input did not name a lane, a depot or a window. Ask again with one."
	b.ReportAllocs()
	for b.Loop() {
		var v benchVerdict
		_ = Decode(prose, &v)
	}
}
