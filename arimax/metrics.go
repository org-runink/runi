package arimax

import "math"

// MAE is the mean absolute error between actual and predicted.
func MAE(actual, pred []float64) float64 {
	n := min(len(actual), len(pred))
	if n == 0 {
		return math.NaN()
	}
	var s float64
	for i := 0; i < n; i++ {
		s += math.Abs(actual[i] - pred[i])
	}
	return s / float64(n)
}

// RMSE is the root mean squared error between actual and predicted.
func RMSE(actual, pred []float64) float64 {
	n := min(len(actual), len(pred))
	if n == 0 {
		return math.NaN()
	}
	var s float64
	for i := 0; i < n; i++ {
		d := actual[i] - pred[i]
		s += d * d
	}
	return math.Sqrt(s / float64(n))
}

// MAPE is the mean absolute percentage error, in percent.
//
// Observations where actual is zero are SKIPPED, not treated as infinite, and
// the count of skipped points is returned. MAPE is undefined at zero and the
// usual silent handling (dropping them, or letting the result become +Inf) has
// misled people reading a dashboard. For channel data with genuine zero-spend
// days this matters: check skipped before quoting the figure.
func MAPE(actual, pred []float64) (pct float64, skipped int) {
	n := min(len(actual), len(pred))
	if n == 0 {
		return math.NaN(), 0
	}
	var s float64
	used := 0
	for i := 0; i < n; i++ {
		if actual[i] == 0 {
			skipped++
			continue
		}
		s += math.Abs((actual[i] - pred[i]) / actual[i])
		used++
	}
	if used == 0 {
		return math.NaN(), skipped
	}
	return 100 * s / float64(used), skipped
}
