// Command restbench benchmarks the packages the first comparison left out.
//
// Six of the twelve packages had no Python figure at all — salvage, chain,
// lazy, toon, tablelog and budget — which made the published table a selection
// of our best cases rather than a comparison. This measures five of them
// against what a Python author would actually reach for, and says plainly that
// the sixth has no counterpart.
//
// Every workload here is identical on both sides, and the Python half is in
// benchmarks/restbench.py.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/org-runink/runi/chain"
	"github.com/org-runink/runi/lazy"
	"github.com/org-runink/runi/salvage"
	"github.com/org-runink/runi/tablelog"
	"github.com/org-runink/runi/toon"
)

func med(d []time.Duration) float64 {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2].Seconds()
}

func timeIt(reps int, f func()) float64 {
	f() // warm
	out := make([]time.Duration, reps)
	for i := range out {
		t := time.Now()
		f()
		out[i] = time.Since(t)
	}
	return med(out)
}

// replies builds model-reply-shaped texts: prose, a fenced JSON object, prose.
func replies(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(
			"Sure — here is the result you asked for.\n\n```json\n"+
				`{"id":%d,"verdict":"pass","score":%0.3f,"notes":"line %d of the reply"}`+
				"\n```\n\nLet me know if you need anything else.", i, float64(i)*0.37, i)
	}
	return out
}

type verdict struct {
	ID      int     `json:"id"`
	Verdict string  `json:"verdict"`
	Score   float64 `json:"score"`
	Notes   string  `json:"notes"`
}

func main() {
	res := map[string]any{"library": "runi", "go": runtime.Version()}

	// ---- salvage: pull JSON out of 2,000 model replies ----
	texts := replies(2000)
	res["salvage_n"] = len(texts)
	res["salvage_s"] = timeIt(5, func() {
		for _, t := range texts {
			var v verdict
			_ = salvage.Decode(t, &v)
		}
	})

	// ---- chain: seal then verify 20,000 records ----
	const nrec = 20000
	bodies := make([][]byte, nrec)
	for i := range bodies {
		bodies[i] = []byte(fmt.Sprintf(`{"event":%d,"actor":"svc","action":"write"}`, i))
	}
	res["chain_n"] = nrec
	var links []chain.Link
	res["chain_seal_s"] = timeIt(5, func() {
		links = make([]chain.Link, 0, nrec)
		prev := ""
		for _, b := range bodies {
			l := chain.Seal(chain.Bound, prev, b)
			links = append(links, l)
			prev = l.Hash
		}
	})
	res["chain_verify_s"] = timeIt(5, func() { _, _ = chain.Verify(chain.Bound, links) })

	// ---- lazy: five 80 ms values, resolved together ----
	res["lazy_values"] = 5
	res["lazy_each_ms"] = 80
	res["lazy_s"] = timeIt(3, func() {
		vs := make([]*lazy.Value[int], 5)
		for i := range vs {
			i := i
			vs[i] = lazy.New(func(context.Context) (int, error) {
				time.Sleep(80 * time.Millisecond)
				return i, nil
			})
		}
		_, _ = lazy.All(context.Background(), vs...)
	})

	// ---- toon vs JSON: 2,000 uniform rows, time AND size ----
	rows := make([]any, 2000)
	for i := range rows {
		rows[i] = map[string]any{
			"id": i, "severity": []string{"low", "high", "medium"}[i%3],
			"file": fmt.Sprintf("internal/pkg%d/file.go", i%40), "line": i % 900,
		}
	}
	doc := map[string]any{"findings": rows}
	res["toon_rows"] = len(rows)
	var toonText string
	res["toon_encode_s"] = timeIt(5, func() { toonText, _ = toon.Encode(doc) })
	res["toon_bytes"] = len(toonText)
	res["toon_decode_s"] = timeIt(5, func() {
		var back map[string]any
		_ = toon.Decode(toonText, &back)
	})
	// Go's own encoding/json on the same document. Without this the TOON rows
	// compare a Go implementation against CPython's C json module and measure
	// the C, not the format. This isolates what the format costs.
	var jb []byte
	res["gojson_encode_s"] = timeIt(5, func() { jb, _ = json.Marshal(doc) })
	res["gojson_bytes"] = len(jb)
	res["gojson_decode_s"] = timeIt(5, func() {
		var back map[string]any
		_ = json.Unmarshal(jb, &back)
	})

	// ---- tablelog: 10,000 rows in 100 commits, then read back ----
	const trows, tbatch = 10000, 100
	res["tablelog_rows"] = trows
	res["tablelog_write_s"] = timeIt(3, func() {
		st := tablelog.NewMemStore()
		tb, err := tablelog.Open(st, "bench", "events")
		if err != nil {
			panic(err)
		}
		ctx := context.Background()
		batch := make([]tablelog.Row, 0, tbatch)
		for i := 0; i < trows; i++ {
			batch = append(batch, tablelog.Row{
				Key:     fmt.Sprintf("k%06d", i),
				Payload: []byte(fmt.Sprintf(`{"event":%d,"kind":"write"}`, i)),
			})
			if len(batch) == tbatch {
				if _, err := tb.Put(ctx, batch...); err != nil {
					panic(err)
				}
				batch = batch[:0]
			}
		}
	})
	// read back
	{
		st := tablelog.NewMemStore()
		tb, _ := tablelog.Open(st, "bench", "events")
		ctx := context.Background()
		batch := make([]tablelog.Row, 0, tbatch)
		for i := 0; i < trows; i++ {
			batch = append(batch, tablelog.Row{Key: fmt.Sprintf("k%06d", i), Payload: []byte(`{"x":1}`)})
			if len(batch) == tbatch {
				_, _ = tb.Put(ctx, batch...)
				batch = batch[:0]
			}
		}
		res["tablelog_read_s"] = timeIt(3, func() { _, _ = tb.Scan(ctx, "") })
	}

	res["budget_note"] = "no Python counterpart benchmarked: a deadline split across " +
		"phases with floors and per-phase contexts has no single library to compare against"

	b, _ := json.MarshalIndent(res, "", "  ")
	here, _ := os.Getwd()
	_ = os.WriteFile(filepath.Join(here, "results_rest_runi.json"), b, 0o644)
	fmt.Println(string(b))
	_ = strings.TrimSpace
}
