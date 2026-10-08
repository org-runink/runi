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

	// 3. Fit on the training half only.
	m, err := arimax.Fit(yTrain, xTrain, 1, arimax.Order{P: 1, D: 1, Q: 1})
	if err != nil {
		panic(err)
	}

	// 4. Forecast the held-out span, with 95% intervals.
	point, lo, hi, err := m.Forecast(len(yTest), xTest, 0.05)
	if err != nil {
		panic(err)
	}

	// 5. Score it, and check the intervals were honest.
	inside := 0
	for i := range yTest {
		if yTest[i] >= lo[i] && yTest[i] <= hi[i] {
			inside++
		}
	}
	fmt.Printf("rmse<1=%t coverage=%d/%d\n",
		arimax.RMSE(yTest, point) < 1.0, inside, len(yTest))

	// Output:
	// n=100 mean=40.6
	// rmse<1=true coverage=20/20
}

func loadSeries() (y, x []float64) {
	y = make([]float64, 100)
	x = make([]float64, 100)
	for i := range y {
		x[i] = float64(i % 7)
		y[i] = 10 + 0.5*float64(i) + 2*x[i]
	}
	return y, x
}
