package arimax_test

import (
	"fmt"

	"github.com/org-runink/runi/arimax"
	"github.com/org-runink/runi/stats"
)

// Example_pipeline is the walkthrough printed in the README, kept here so that
// `go test` fails if the documented example stops compiling or stops working.
// A README example that no longer runs is worse than none.
func Example_pipeline() {
	y, x := loadSeries()

	// 1. Look at it first.
	s := stats.Describe(y)
	fmt.Printf("n=%d mean=%.1f\n", s.N, s.Mean)

	// 2. Split WITHOUT shuffling — order carries information here.
	k := stats.SplitIndex(len(y), 0.8)
	yTrain, yTest := y[:k], y[k:]
	xTrain, xTest := x[:k], x[k:]

	// 3. Fit on the training half only. The order has to be the order the data
	//    has: see loadSeries for why fitting a different one makes step 5
	//    unreadable. Difference and SeasonalDifference, plus ACF and PACF, are
	//    how you choose it for a real series.
	m, err := arimax.Fit(yTrain, xTrain, 1, arimax.Order{P: 1, D: 0, Q: 1})
	if err != nil {
		panic(err)
	}

	// 4. Forecast the held-out span, with 95% intervals.
	point, lo, hi, err := m.Forecast(len(yTest), xTest, 0.05)
	if err != nil {
		panic(err)
	}

	// 5. Score it, and count how many realised values the intervals held.
	//    Twenty consecutive points from ONE series is not a coverage estimate —
	//    the forecast errors are correlated, so a calibrated 95% interval
	//    routinely holds 17 of them and occasionally all 20. The empirical
	//    figure is measured over many independent series in
	//    TestIntervalCoverage, and quoted in the package documentation.
	inside := 0
	for i := range yTest {
		if yTest[i] >= lo[i] && yTest[i] <= hi[i] {
			inside++
		}
	}
	fmt.Printf("rmse<1=%t held=%d/%d\n",
		arimax.RMSE(yTest, point) < 1.0, inside, len(yTest))

	// Output:
	// n=100 mean=15.8
	// rmse<1=true held=17/20
}

func loadSeries() (y, x []float64) {
	const n = 100
	y = make([]float64, n)
	x = make([]float64, n)

	// A deterministic pseudo-random shock, so the example's printed output is
	// fixed, over errors that really are the ARMA(1,1) step 3 fits.
	//
	// Both halves of that matter, and the original version of this example got
	// them both wrong. A noise-free series makes step 5 meaningless: the fit
	// becomes near-exact, the residual variance it reports collapses towards
	// zero, and the interval ends up narrower than its own rounding error, so
	// it covers nothing or everything for reasons that have nothing to do with
	// calibration. And a series whose errors are not the order being fitted has
	// no calibrated interval to find at all — a deterministic trend under d=1,
	// for instance, leaves a constant in the differenced errors that a
	// zero-mean ARMA can only chase by driving its AR root to the stationarity
	// bound, and the forecast then wanders off with nothing to pull it back.
	seed := uint64(7)
	shock := func() float64 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return (float64(seed>>11)/float64(1<<53)*2 - 1) * 0.5
	}

	var nt, prev float64
	for i := range y {
		e := shock()
		nt = 0.6*nt + e + 0.3*prev
		prev = e
		x[i] = float64(i % 7)
		y[i] = 10 + 2*x[i] + nt
	}
	return y, x
}
