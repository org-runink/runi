package arimax

// Difference applies d-th order differencing: the series of successive
// changes, repeated d times. It is how a trending series is made stationary,
// and it is the "I" in ARIMA.
//
// Each pass shortens the series by one, so the result has len(y)−d elements.
// Difference(y, 0) returns a copy.
func Difference(y []float64, d int) []float64 {
	out := make([]float64, len(y))
	copy(out, y)
	for k := 0; k < d; k++ {
		if len(out) < 2 {
			return nil
		}
		next := make([]float64, len(out)-1)
		for i := 1; i < len(out); i++ {
			next[i-1] = out[i] - out[i-1]
		}
		out = next
	}
	return out
}

// SeasonalDifference applies one seasonal difference of period m: y_t − y_{t−m}.
// Use it for weekly (m=7) or monthly (m=12) patterns before, or instead of,
// ordinary differencing.
func SeasonalDifference(y []float64, m int) []float64 {
	if m <= 0 || len(y) <= m {
		return nil
	}
	out := make([]float64, len(y)-m)
	for i := m; i < len(y); i++ {
		out[i-m] = y[i] - y[i-m]
	}
	return out
}

// integrate converts forecasts of a d-times-differenced series back to levels.
//
// tail holds the last d observations of the ORIGINAL (undifferenced) series,
// oldest first. For d >= 2 it is not enough to add the tail values back: the
// inner level needs the last DIFFERENCE at that level, not the last value, so
// the tail is differenced down first to recover one base per level.
func integrate(diffs []float64, tail []float64, d int) []float64 {
	out := make([]float64, len(diffs))
	copy(out, diffs)
	if d == 0 {
		return out
	}
	if len(tail) < d {
		return nil
	}

	// base[k] is the last value of the k-th differenced series.
	base := make([]float64, d)
	cur := append([]float64(nil), tail...)
	base[0] = cur[len(cur)-1]
	for k := 1; k < d; k++ {
		nxt := make([]float64, len(cur)-1)
		for i := 1; i < len(cur); i++ {
			nxt[i-1] = cur[i] - cur[i-1]
		}
		cur = nxt
		base[k] = cur[len(cur)-1]
	}

	// Cumulatively sum from the innermost level outwards.
	for k := d - 1; k >= 0; k-- {
		run := base[k]
		for i := range out {
			run += out[i]
			out[i] = run
		}
	}
	return out
}
