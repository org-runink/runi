// Command verify_classical writes series and the components season.Classical
// splits them into, so that verify_classical.py can check them against
// statsmodels.tsa.seasonal.seasonal_decompose.
//
// The series are written out rather than generated on both sides, at full
// float64 precision, so that the two libraries are provably given the same
// numbers -- the same reason benchmarks/gen.py writes the forecasting CSVs.
// They land in benchmarks/data/, which is generated and not stored.
package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"

	"github.com/org-runink/runi/season"
)

func write(name string, cols ...[]float64) error {
	var b strings.Builder
	for i := range cols[0] {
		for j, c := range cols {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatFloat(c[i], 'g', -1, 64))
		}
		b.WriteByte('\n')
	}
	return os.WriteFile(name, []byte(b.String()), 0o644)
}

func main() {
	if err := os.MkdirAll("data", 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Even and odd periods, a short series at exactly two cycles, and the
	// n=4,000 case the benchmark measures.
	cases := []struct {
		n, period int
	}{{4000, 24}, {4000, 25}, {500, 7}, {300, 12}, {48, 24}, {97, 48}}
	for _, c := range cases {
		r := rand.New(rand.NewPCG(uint64(c.n), uint64(c.period)))
		x := make([]float64, c.n)
		for i := range x {
			x[i] = 100 + 0.05*float64(i) +
				10*math.Sin(2*math.Pi*float64(i)/float64(c.period)) + r.NormFloat64()
		}
		d, err := season.Classical(x, c.period)
		if err != nil {
			fmt.Fprintf(os.Stderr, "n=%d p=%d: %v\n", c.n, c.period, err)
			os.Exit(1)
		}
		tag := fmt.Sprintf("%d_%d", c.n, c.period)
		if err := write("data/vc_x_"+tag+".csv", x); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := write("data/vc_go_"+tag+".csv", d.Trend, d.Seasonal, d.Residual); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote n=%d period=%d\n", c.n, c.period)
	}
}
