// Command crossbench measures runi against the Python packages people reach
// for first. Every case here is one both sides genuinely implement; where a
// Python library has no equivalent the case is left out rather than faked.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"runtime"
	"time"

	"github.com/org-runink/runi/avro"
	"github.com/org-runink/runi/bm25"
	"github.com/org-runink/runi/season"
	"github.com/org-runink/runi/stats"
)

func med(d []time.Duration) float64 {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
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

// corpus builds synthetic documents. Synthetic, never real records: the data
// rule is that we open-source code, not anyone's text.
func corpus(n, words int, r *rand.Rand) []string {
	vocab := make([]string, 2000)
	for i := range vocab {
		vocab[i] = fmt.Sprintf("term%04d", i)
	}
	out := make([]string, n)
	for i := range out {
		b := make([]byte, 0, words*9)
		for w := 0; w < words; w++ {
			if w > 0 {
				b = append(b, ' ')
			}
			b = append(b, vocab[r.IntN(len(vocab))]...)
		}
		out[i] = string(b)
	}
	return out
}

func main() {
	res := map[string]any{"library": "runi", "go": runtime.Version(), "arch": runtime.GOARCH}
	r := rand.New(rand.NewPCG(7, 11))

	// ---- bm25: index then query, against rank_bm25 / sklearn ----
	docs := corpus(5000, 120, r)
	queries := make([]string, 200)
	for i := range queries {
		queries[i] = fmt.Sprintf("term%04d term%04d term%04d", r.IntN(2000), r.IntN(2000), r.IntN(2000))
	}
	bdocs := make([]bm25.Document, len(docs))
	for i, d := range docs {
		bdocs[i] = bm25.Document{ID: fmt.Sprint(i), Text: d}
	}
	var idx *bm25.Index
	res["bm25_index_s"] = timeIt(3, func() { idx = bm25.New(bdocs, bm25.Options{}) })
	res["bm25_query_s"] = timeIt(5, func() {
		for _, q := range queries {
			_ = idx.Search(q, 10)
		}
	})
	res["bm25_docs"], res["bm25_queries"] = len(docs), len(queries)

	// ---- stats: correlation + a t-test, against scipy.stats ----
	n := 200000
	xs, ys := make([]float64, n), make([]float64, n)
	for i := range xs {
		xs[i] = r.NormFloat64()
		ys[i] = 0.6*xs[i] + r.NormFloat64()
	}
	res["pearson_s"] = timeIt(5, func() { _, _ = stats.Pearson(xs, ys) })
	res["spearman_s"] = timeIt(3, func() { _, _ = stats.Spearman(xs, ys) })
	res["pearson_n"] = n
	trend := make([]float64, 100000)
	for i := range trend {
		trend[i] = 0.001*float64(i) + r.NormFloat64()
	}
	res["trend_s"] = timeIt(5, func() { _ = stats.Trend(trend) })
	res["trend_n"] = len(trend)

	// ---- season: decomposition, against statsmodels seasonal_decompose ----
	sdata := make([]float64, 4000)
	for i := range sdata {
		sdata[i] = 100 + 0.05*float64(i) + 10*math.Sin(2*math.Pi*float64(i)/24) + r.NormFloat64()
	}
	// Like for like with statsmodels.seasonal_decompose: the period is GIVEN to
	// both, and the trend-break search is OFF, because seasonal_decompose does
	// not look for breaks. Comparing our default against it measured a
	// different and larger job and reported the difference as a loss on this
	// one, which flattered neither library.
	res["season_decompose_s"] = timeIt(7, func() {
		_, _ = season.Decompose(sdata, season.Options{Period: 24, MaxChangepoints: -1})
	})
	// The classical moving-average decomposition: the same operation again,
	// computed the way seasonal_decompose computes it. This is the row the
	// comparison turns on, because it is like for like all the way down --
	// same method, same output, to 1e-10 on the interior.
	res["season_classical_s"] = timeIt(7, func() {
		_, _ = season.Classical(sdata, 24)
	})
	// What the extra work costs, reported separately rather than folded into
	// the comparison above: the BIC-priced changepoint search, and then
	// detecting the period as well instead of being told it.
	res["season_decompose_with_breaks_s"] = timeIt(7, func() {
		_, _ = season.Decompose(sdata, season.Options{Period: 24})
	})
	res["season_auto_s"] = timeIt(7, func() {
		_, _ = season.Decompose(sdata, season.Options{})
	})
	res["season_period_detect_s"] = timeIt(7, func() { _ = season.Period(sdata, 0) })
	res["season_n"] = len(sdata)

	// ---- avro: encode + decode, against fastavro ----
	type row struct {
		Name string
		Vals []float64
	}
	rows := make([]row, 20000)
	for i := range rows {
		rows[i] = row{Name: fmt.Sprintf("row-%06d", i), Vals: []float64{r.Float64(), r.Float64(), r.Float64()}}
	}
	const schema = `{"type":"record","name":"R","fields":[{"name":"name","type":"string"},{"name":"vals","type":{"type":"array","items":"double"}}]}`
	marshal := func(e *avro.Encoder, v row) { e.String(v.Name); e.Float64Array(v.Vals) }
	unmarshal := func(d *avro.Decoder) (row, error) {
		var v row
		var err error
		if v.Name, err = d.String(); err != nil {
			return v, err
		}
		v.Vals, err = d.Float64Array()
		return v, err
	}
	var buf bytes.Buffer
	res["avro_write_s"] = timeIt(3, func() {
		buf.Reset()
		_ = avro.WriteOCF(&buf, schema, "null", rows, marshal)
	})
	body := append([]byte(nil), buf.Bytes()...)
	res["avro_read_s"] = timeIt(3, func() { _, _, _ = avro.ReadOCF(bytes.NewReader(body), unmarshal) })
	res["avro_rows"], res["avro_bytes"] = len(rows), len(body)

	b, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(b))
	_ = os.WriteFile("results_runi_cross.json", b, 0o644)
}
