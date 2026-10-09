package arimax

import "math"

// Order is the (p,d,q) specification: autoregressive terms, differences, and
// moving-average terms.
type Order struct{ P, D, Q int }

// Model is a fitted ARIMAX. Use Fit to build one.
type Model struct {
	Order Order

	// Beta are the exogenous coefficients, one per column of X, in column
	// order. Nil when the model has no regressors.
	Beta []float64

	// Intercept of the regression stage.
	Intercept float64

	// AR and MA are the fitted ARMA parameters of the regression errors.
	AR, MA []float64

	// Sigma2 is the residual variance of the ARMA stage.
	Sigma2 float64

	// Residuals of the fitted model, for diagnostics. Pass them to LjungBox.
	Residuals []float64

	y    []float64 // the original (undifferenced) series
	errs []float64 // regression errors n_t, undifferenced
}

// Fit estimates an ARIMAX model of y with optional exogenous regressors.
//
// x is row-major with one row per observation of y and k columns, or nil for a
// plain ARIMA. The form fitted is regression with ARIMA errors, as described in
// the package documentation; the estimator is conditional least squares over
// the regression and ARMA parameters jointly, reached by alternating an exact
// generalised-least-squares solve for the coefficients on prewhitened data
// with a Nelder-Mead refit of the ARMA. It is not exact maximum likelihood.
func Fit(y []float64, x []float64, k int, ord Order) (*Model, error) {
	n := len(y)
	if ord.P < 0 || ord.D < 0 || ord.Q < 0 {
		return nil, errOrder
	}
	// Only what the stages below need before the ARMA fit: enough points to
	// regress on and to difference. Whether the order itself is affordable is
	// fitARMA's rule, and it is left there rather than restated here — stated
	// twice, the stricter copy wins silently and the real one is never reached,
	// so the error that users actually get comes from a line no test can enter.
	if n <= ord.D+2 {
		return nil, errShort
	}
	if k > 0 && len(x) != n*k {
		return nil, errExogLen
	}

	m := &Model{Order: ord, y: append([]float64(nil), y...)}

	// Stage 1: regress y on [1, X] by QR. With no regressors this reduces to
	// the mean, which is still worth removing before the ARMA stage.
	cols := k + 1
	design := make([]float64, n*cols)
	for i := 0; i < n; i++ {
		design[i*cols] = 1
		for j := 0; j < k; j++ {
			design[i*cols+1+j] = x[i*k+j]
		}
	}
	// olsQR overwrites the design it factors, and stage 3 needs the original
	// columns to prewhiten, so the factorisation works on a scratch copy.
	scratch := make([]float64, len(design))
	copy(scratch, design)
	coef, err := olsQR(scratch, y, n, cols)
	if err != nil {
		return nil, err
	}

	// Regression errors n_t = y_t − β'X_t − c.
	errs := regressErrors(y, x, n, k, coef)

	// Stage 2: ARMA on the differenced regression errors.
	a, err := fitARMA(Difference(errs, ord.D), ord.P, ord.Q)
	if err != nil {
		return nil, err
	}

	// Stage 3: alternate the two blocks until the coefficients settle. Stages 1
	// and 2 on their own estimate β as if the errors were independent, which
	// costs it about half its precision on the persistent errors this package
	// targets. See refine.
	st := &fitState{
		y: y, x: x, design: design,
		n: n, k: k, cols: cols,
		ord: ord, coef: coef, errs: errs, arma: a,
	}
	st.refine()

	m.Intercept = st.coef[0]
	if k > 0 {
		m.Beta = append([]float64(nil), st.coef[1:]...)
	}
	m.errs = st.errs
	m.AR, m.MA, m.Sigma2, m.Residuals = st.arma.phi, st.arma.theta, st.arma.sigma2, st.arma.resid
	return m, nil
}

// Forecast predicts h steps beyond the fitted series.
//
// xFuture is row-major with h rows and the same number of columns as the X used
// to fit, or nil when the model has no regressors. It is REQUIRED: an ARIMAX
// forecast cannot be produced without the exogenous values for the horizon, and
// this returns an error rather than silently assuming they are zero or held
// flat — both of which produce a confident, wrong answer.
//
// The returned intervals are (1−alpha) prediction intervals from the model's
// psi-weights. They assume Gaussian errors and KNOWN parameters, and they
// condition on xFuture being correct, so where xFuture is itself a forecast the
// true uncertainty is wider than reported.
func (m *Model) Forecast(h int, xFuture []float64, alpha float64) (point, lo, hi []float64, err error) {
	if h <= 0 {
		return nil, nil, nil, nil
	}
	k := len(m.Beta)
	if k > 0 && len(xFuture) != h*k {
		return nil, nil, nil, errFuture
	}

	// Forecast the ARMA part on the differenced error series, then integrate.
	d := Difference(m.errs, m.Order.D)
	a := &arma{phi: m.AR, theta: m.MA, sigma2: m.Sigma2, resid: m.Residuals}
	df := armaForecast(d, a, h)

	tail := m.errs
	if m.Order.D > 0 {
		tail = m.errs[len(m.errs)-m.Order.D:]
	}
	nf := integrate(df, tail, m.Order.D)

	point = make([]float64, h)
	for i := 0; i < h; i++ {
		v := m.Intercept + nf[i]
		for j := 0; j < k; j++ {
			v += m.Beta[j] * xFuture[i*k+j]
		}
		point[i] = v
	}

	z := zCritical(alpha)
	psi := psiWeights(a, h)
	lo = make([]float64, h)
	hi = make([]float64, h)
	var cum float64
	for i := 0; i < h; i++ {
		cum += psi[i] * psi[i]
		se := math.Sqrt(m.Sigma2 * cum)
		lo[i] = point[i] - z*se
		hi[i] = point[i] + z*se
	}
	return point, lo, hi, nil
}

// armaForecast extends a zero-mean ARMA series h steps.
func armaForecast(y []float64, a *arma, h int) []float64 {
	p, q := len(a.phi), len(a.theta)
	hist := append([]float64(nil), y...)
	// Residuals align with the tail of y: cssResiduals drops the first p.
	errs := make([]float64, len(y))
	copy(errs[len(y)-len(a.resid):], a.resid)

	out := make([]float64, h)
	for s := 0; s < h; s++ {
		v := 0.0
		for i := 0; i < p; i++ {
			idx := len(hist) - 1 - i
			if idx >= 0 {
				v += a.phi[i] * hist[idx]
			}
		}
		for j := 0; j < q; j++ {
			idx := len(errs) - 1 - j
			if idx >= 0 {
				v += a.theta[j] * errs[idx]
			}
		}
		out[s] = v
		hist = append(hist, v)
		errs = append(errs, 0) // future shocks have expectation zero
	}
	return out
}

// zCritical returns the two-sided normal critical value for the common levels,
// via a rational approximation to the inverse normal CDF (Acklam). Exact to
// about 1e-9 over the range that matters here.
func zCritical(alpha float64) float64 {
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.05
	}
	return -invNormCDF(alpha / 2)
}

func invNormCDF(p float64) float64 {
	a := [6]float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := [5]float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := [6]float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	dd := [4]float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	const plow = 0.02425
	switch {
	case p < plow:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((dd[0]*q+dd[1])*q+dd[2])*q+dd[3])*q + 1)
	case p > 1-plow:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((dd[0]*q+dd[1])*q+dd[2])*q+dd[3])*q + 1)
	default:
		q := p - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	}
}
