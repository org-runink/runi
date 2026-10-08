// Command runibench fits runi/arimax to the same CSV files the statsmodels
// harness reads, and writes results_runi.json beside them.
//
// Same data, same model order, same held-out horizon. Run benchmarks/gen.py
// first.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/org-runink/runi/arimax"
)

type truth struct {
	Intercept float64 `json:"intercept"`
	Beta      float64 `json:"beta"`
	Phi       float64 `json:"phi"`
	Theta     float64 `json:"theta"`
	Sigma     float64 `json:"sigma"`
}

type meta struct {
	Truth truth                     `json:"truth"`
	Sets  map[string]map[string]int `json:"sets"`
}

func readCSV(path string) (xs, ys []float64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	for i, rec := range rows {
		if i == 0 {
			continue // header
		}
		x, e1 := strconv.ParseFloat(rec[0], 64)
		y, e2 := strconv.ParseFloat(rec[1], 64)
		if e1 != nil || e2 != nil {
			return nil, nil, fmt.Errorf("row %d: bad float", i)
		}
		xs = append(xs, x)
		ys = append(ys, y)
	}
	return xs, ys, nil
}

type accuracyResult struct {
	Trials         int     `json:"trials"`
	SecondsTotal   float64 `json:"seconds_total"`
	SecondsPerFit  float64 `json:"seconds_per_fit"`
	BetaBias       float64 `json:"beta_bias"`
	BetaRMSE       float64 `json:"beta_rmse"`
	PhiBias        float64 `json:"phi_bias"`
	PhiRMSE        float64 `json:"phi_rmse"`
	CoveragePct    float64 `json:"coverage_pct"`
	CoveragePoints int     `json:"coverage_points"`
	ForecastRMSE   float64 `json:"forecast_rmse"`
	NaiveRMSE      float64 `json:"naive_rmse"`
}

func runAccuracy(dir string, m meta) accuracyResult {
	cfg := m.Sets["accuracy"]
	trials, n, h := cfg["trials"], cfg["n"], cfg["horizon"]
	xs, ys, err := readCSV(filepath.Join(dir, "accuracy.csv"))
	if err != nil {
		panic(err)
	}

	var bErr, pErr, fRMSE, nRMSE []float64
	cover, pts := 0, 0
	start := time.Now()
	for i := 0; i < trials; i++ {
		off := i * n
		x := xs[off : off+n]
		y := ys[off : off+n]
		xtr, ytr := x[:n-h], y[:n-h]
		xte, yte := x[n-h:], y[n-h:]

		mod, err := arimax.Fit(ytr, xtr, 1, arimax.Order{P: 1, D: 0, Q: 1})
		if err != nil {
			continue
		}
		if len(mod.Beta) > 0 {
			bErr = append(bErr, mod.Beta[0]-m.Truth.Beta)
		}
		if len(mod.AR) > 0 {
			pErr = append(pErr, mod.AR[0]-m.Truth.Phi)
		}
		pt, lo, hi, err := mod.Forecast(h, xte, 0.05)
		if err != nil {
			continue
		}
		var se, sn float64
		for j := 0; j < h; j++ {
			if yte[j] >= lo[j] && yte[j] <= hi[j] {
				cover++
			}
			pts++
			se += (yte[j] - pt[j]) * (yte[j] - pt[j])
			d := yte[j] - ytr[len(ytr)-1]
			sn += d * d
		}
		fRMSE = append(fRMSE, math.Sqrt(se/float64(h)))
		nRMSE = append(nRMSE, math.Sqrt(sn/float64(h)))
	}
	el := time.Since(start).Seconds()

	return accuracyResult{
		Trials: len(bErr), SecondsTotal: el,
		SecondsPerFit: el / math.Max(float64(len(bErr)), 1),
		BetaBias:      mean(bErr), BetaRMSE: rms(bErr),
		PhiBias: mean(pErr), PhiRMSE: rms(pErr),
		CoveragePct:    100 * float64(cover) / math.Max(float64(pts), 1),
		CoveragePoints: pts,
		ForecastRMSE:   mean(fRMSE), NaiveRMSE: mean(nRMSE),
	}
}

func runTiming(dir string) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for _, n := range []int{500, 2000, 10000} {
		x, y, err := readCSV(filepath.Join(dir, fmt.Sprintf("timing_%d.csv", n)))
		if err != nil {
			panic(err)
		}
		reps := 10
		if n > 2000 {
			reps = 3
		}
		_, _ = arimax.Fit(y, x, 1, arimax.Order{P: 1, D: 0, Q: 1}) // warm up
		var ts []float64
		for r := 0; r < reps; r++ {
			t0 := time.Now()
			_, _ = arimax.Fit(y, x, 1, arimax.Order{P: 1, D: 0, Q: 1})
			ts = append(ts, time.Since(t0).Seconds())
		}
		sort.Float64s(ts)
		out[strconv.Itoa(n)] = map[string]float64{
			"seconds_median": ts[len(ts)/2], "reps": float64(reps),
		}
	}
	return out
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func rms(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	var s float64
	for _, x := range v {
		s += x * x
	}
	return math.Sqrt(s / float64(len(v)))
}

func main() {
	here, _ := os.Getwd()
	dir := filepath.Join(here, "data")
	mb, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "run benchmarks/gen.py first:", err)
		os.Exit(1)
	}
	var m meta
	if err := json.Unmarshal(mb, &m); err != nil {
		panic(err)
	}

	res := map[string]any{
		"library":        "runi/arimax",
		"version":        "v0.1.7",
		"estimator":      "staged regression with ARIMA errors, conditional sum of squares",
		"import_seconds": 0,
		"accuracy":       runAccuracy(dir, m),
		"timing":         runTiming(dir),
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(here, "results_runi.json"), b, 0o644); err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
