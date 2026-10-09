package arimax

import "math"

// ACF returns the sample autocorrelation function of y for lags 0..maxLag.
// The value at index 0 is always 1. Use it to read the moving-average order:
// an MA(q) series has an ACF that cuts off after lag q.
//
// A constant series has zero variance, so every autocorrelation beyond lag 0 is
// 0/0 and undefined. Rather than return NaNs, ACF reports lag 0 as 1 — which is
// true by definition and keeps the documented contract — and every later lag as
// 0, which reads as "no structure" to the order-selection it feeds.
func ACF(y []float64, maxLag int) []float64 {
	n := len(y)
	if n == 0 || maxLag < 0 {
		return nil
	}
	if maxLag >= n {
		maxLag = n - 1
	}
	m := mean(y)
	var c0 float64
	for _, v := range y {
		d := v - m
		c0 += d * d
	}
	out := make([]float64, maxLag+1)
	if c0 == 0 {
		// Constant series: lag 0 is 1 by definition, the rest stay 0.
		out[0] = 1
		return out
	}
	for k := 0; k <= maxLag; k++ {
		var ck float64
		for i := k; i < n; i++ {
			ck += (y[i] - m) * (y[i-k] - m)
		}
		out[k] = ck / c0
	}
	return out
}

// PACF returns the sample partial autocorrelation function for lags 0..maxLag,
// computed by the Durbin–Levinson recursion. Index 0 is 1. Use it to read the
// autoregressive order: an AR(p) series has a PACF that cuts off after lag p.
func PACF(y []float64, maxLag int) []float64 {
	r := ACF(y, maxLag)
	if len(r) == 0 {
		return nil
	}
	// ACF clamps maxLag to the lags the series can actually supply, so the
	// sequence it returns may be shorter than the caller asked for. The
	// recursion must be run over what came back, not over what was requested:
	// passing the original maxLag on indexes past the end of r, and PACF
	// panicked for every caller who asked for more lags than the series has
	// points -- which is the ordinary way to ask for "as many as there are".
	return levinson(r, len(r)-1)
}

// levinson runs the Durbin–Levinson recursion over an autocorrelation sequence.
// It is separate from PACF because its one guard — a prediction variance that
// has collapsed to nothing — cannot be reached through PACF: for a sample
// autocorrelation sequence computed from real data the recursion keeps every
// reflection coefficient strictly inside (-1, 1), so the variance stays
// positive. The guard is for a degenerate sequence, such as the perfectly
// correlated r = [1, 1, 1, …] a caller could construct, where the first step
// already leaves nothing to divide by. Splitting it out is what lets that case
// be tested with the input it exists for, instead of being asserted about.
func levinson(r []float64, maxLag int) []float64 {
	out := make([]float64, len(r))
	out[0] = 1
	if maxLag == 0 {
		return out
	}
	phi := make([]float64, maxLag+1)
	prev := make([]float64, maxLag+1)
	phi[1] = r[1]
	out[1] = r[1]
	v := 1 - r[1]*r[1]
	for k := 2; k <= maxLag; k++ {
		copy(prev, phi)
		num := r[k]
		for j := 1; j < k; j++ {
			num -= prev[j] * r[k-j]
		}
		if math.Abs(v) < 1e-300 {
			// Nothing left to divide by: the series is perfectly predictable
			// from the lags already used, so every further lag adds nothing.
			break
		}
		pk := num / v
		phi[k] = pk
		for j := 1; j < k; j++ {
			phi[j] = prev[j] - pk*prev[k-j]
		}
		out[k] = pk
		v *= 1 - pk*pk
	}
	return out
}

// LjungBox computes the Ljung–Box Q statistic on the first h lags of the
// residual autocorrelations, with dof degrees of freedom consumed by the fitted
// model (p+q). A large Q means the residuals still carry structure, so the
// model is misspecified.
//
// It returns Q and the degrees of freedom. The package deliberately does not
// return a p-value: that needs a chi-squared CDF, and shipping an approximation
// of one invites people to read significance off a number that is only roughly
// right. Compare Q against a chi-squared table with the returned dof.
func LjungBox(resid []float64, h, dof int) (q float64, df int) {
	n := len(resid)
	if n == 0 || h <= 0 {
		return 0, 0
	}
	r := ACF(resid, h)
	for k := 1; k <= h && k < len(r); k++ {
		q += r[k] * r[k] / float64(n-k)
	}
	q *= float64(n) * float64(n+2)
	df = h - dof
	if df < 1 {
		df = 1
	}
	return q, df
}
