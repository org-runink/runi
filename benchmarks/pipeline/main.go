// Command pipeline runs the same first-pass analysis as benchmarks/pipeline.py,
// on the same CSV, and reports the same stages.
//
// Single operations are the weak form of a comparison: nobody's problem is one
// slow correlation. The question that decides an architecture is what a WHOLE
// pass over a dataset costs, and in particular what it costs a process that
// has to start first — a request handler, a CLI run, a per-tenant job — which
// pays its imports every single time.
//
// Two numbers, because both are real:
//
//	cold  what a process pays from launch. Go's is zero: the library is in
//	      the binary. Python's is importing pandas, numpy, scipy and
//	      statsmodels before the first row is read.
//	warm  the work alone, which is what a notebook pays after the first cell.
//
// The second half is the one a service cares about. The first is the one a
// notebook cares about. They point in opposite directions, and saying so is
// the only honest way to report this.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/org-runink/runi/season"
	"github.com/org-runink/runi/stats"
)

type stages struct {
	Load      float64 `json:"load"`
	Describe  float64 `json:"describe"`
	Correlate float64 `json:"correlate"`
	Trend     float64 `json:"trend"`
	GroupBy   float64 `json:"groupby"`
	Decompose float64 `json:"decompose"`
	Total     float64 `json:"total"`
}

type frame struct {
	value  []float64
	driver []float64
	group  []string
}

func load(path string) (frame, error) {
	f, err := os.Open(path)
	if err != nil {
		return frame{}, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.ReuseRecord = true
	if _, err := r.Read(); err != nil { // header
		return frame{}, err
	}
	out := frame{
		value:  make([]float64, 0, 200000),
		driver: make([]float64, 0, 200000),
		group:  make([]string, 0, 200000),
	}
	for {
		rec, err := r.Read()
		if err != nil {
			break
		}
		v, _ := strconv.ParseFloat(rec[1], 64)
		d, _ := strconv.ParseFloat(rec[2], 64)
		out.value = append(out.value, v)
		out.driver = append(out.driver, d)
		out.group = append(out.group, string(append([]byte(nil), rec[3]...)))
	}
	return out, nil
}

func runOnce(path string) (stages, error) {
	var s stages

	t0 := time.Now()
	df, err := load(path)
	if err != nil {
		return s, err
	}
	s.Load = time.Since(t0).Seconds()

	t0 = time.Now()
	_ = stats.Mean(df.value)
	_ = stats.StdDev(df.value)
	_ = stats.Min(df.value)
	_ = stats.Max(df.value)
	// All three from one ordering, which is what Quantiles is for and what
	// np.percentile(v, [50, 90, 99]) does on the other side.
	_ = stats.Quantiles(df.value, 0.50, 0.90, 0.99)
	s.Describe = time.Since(t0).Seconds()

	t0 = time.Now()
	_, _ = stats.Pearson(df.value, df.driver)
	_, _ = stats.Spearman(df.value, df.driver)
	s.Correlate = time.Since(t0).Seconds()

	t0 = time.Now()
	_ = stats.Trend(df.value)
	s.Trend = time.Since(t0).Seconds()

	// No groupby in runi, and there should not be one: this is a map and a
	// loop, which is what you would write, and it is here so the comparison
	// covers the whole pass rather than only the parts we have a package for.
	t0 = time.Now()
	sums := map[string]float64{}
	counts := map[string]int{}
	for i, g := range df.group {
		sums[g] += df.value[i]
		counts[g]++
	}
	keys := make([]string, 0, len(sums))
	for k := range sums {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	means := make([]float64, len(keys))
	for i, k := range keys {
		means[i] = sums[k] / float64(counts[k])
	}
	s.GroupBy = time.Since(t0).Seconds()

	// Like for like with statsmodels: the period is GIVEN and the trend-break
	// search is OFF, because seasonal_decompose does neither.
	t0 = time.Now()
	_, _ = season.Decompose(df.value[:4000], season.Options{Period: 24, MaxChangepoints: -1})
	s.Decompose = time.Since(t0).Seconds()

	s.Total = s.Load + s.Describe + s.Correlate + s.Trend + s.GroupBy + s.Decompose
	return s, nil
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	return c[len(c)/2]
}

func main() {
	here, _ := os.Getwd()
	path := filepath.Join(here, "data", "pipeline.csv")

	first, err := runOnce(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	runs := make([]stages, 5)
	for i := range runs {
		runs[i], _ = runOnce(path)
	}
	pick := func(f func(stages) float64) float64 {
		xs := make([]float64, len(runs))
		for i, r := range runs {
			xs[i] = f(r)
		}
		return median(xs)
	}
	warm := stages{
		Load:      pick(func(s stages) float64 { return s.Load }),
		Describe:  pick(func(s stages) float64 { return s.Describe }),
		Correlate: pick(func(s stages) float64 { return s.Correlate }),
		Trend:     pick(func(s stages) float64 { return s.Trend }),
		GroupBy:   pick(func(s stages) float64 { return s.GroupBy }),
		Decompose: pick(func(s stages) float64 { return s.Decompose }),
		Total:     pick(func(s stages) float64 { return s.Total }),
	}

	res := map[string]any{
		"library": "runi + the Go standard library",
		"go":      runtime.Version(),
		// Zero, and not as a rhetorical flourish: the packages are compiled
		// into the binary, so there is nothing to import at run time.
		"import_seconds":     0,
		"cold_total_seconds": first.Total,
		"warm":               warm,
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(here, "results_pipeline_runi.json"), b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	fmt.Println(string(b))
}
