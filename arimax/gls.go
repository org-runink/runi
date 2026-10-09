package arimax

import "math"

// Feasible generalised least squares for the regression block of the fit.
//
// The staged fit — ordinary least squares for the coefficients, then an ARMA
// fit to whatever is left over — estimates the coefficients as if the errors
// were independent. They are not: that is the whole premise of the model. OLS
// stays unbiased under autocorrelated errors but it is no longer efficient,
// and for the persistent errors this package targets the lost efficiency is
// large. With AR(1)+MA(1) errors at phi=0.6, theta=0.3 the variance of an OLS
// coefficient is gamma_0 = sigma^2(1+2*phi*theta+theta^2)/(1-phi^2) per
// observation against sigma^2 / sum(whitening coefficients squared) for the
// GLS one — a factor of about four, so twice the standard error. That factor
// was the entire measured accuracy gap against an exact-likelihood fit.
//
// The fix is to stop fitting the two blocks once each and instead alternate
// them: prewhiten with the ARMA just fitted, re-solve the regression on the
// whitened data, refit the ARMA to the new errors, repeat. See refine.

const (
	// maxRefine caps the alternation. Feasible GLS converges geometrically, and
	// it is measured, not assumed: on the benchmark series every reported
	// figure — beta bias and RMSE, phi RMSE, interval coverage, forecast RMSE —
	// is identical to five decimals at two passes and at eight, so the cap only
	// buys robustness on series that are slower to settle. Four is where the
	// cost of the passes nobody needs stops being worth paying.
	maxRefine = 4

	// refineTol is the relative coefficient movement below which another pass
	// cannot change the answer at the precision anyone reads it to.
	refineTol = 1e-8

	// refineStep is the initial simplex offset for a WARM-started ARMA refit.
	// It is a tenth of the cold-start offset: after the first pass the
	// parameters are already close, and a simplex the size of the cold one
	// would spend its whole budget shrinking back to where it began.
	refineStep = 0.01
)

// cssFilter applies the composite linear filter that turns a level series into
// the residuals the ARMA stage minimises: d differences, then the whitening
// recursion of cssResiduals with zero pre-sample errors.
//
// It is LINEAR in its input, because both differencing and the recursion are
// linear and the recursion's initial conditions are fixed at zero rather than
// estimated. That is the property the GLS step rests on: filtering y − D*coef
// gives exactly filter(y) − filter(D)*coef, so a least-squares solve on the
// filtered design minimises the very same conditional sum of squares that
// fitARMA minimises, and not merely something similar to it.
func cssFilter(z []float64, d int, phi, theta []float64) []float64 {
	return cssResiduals(Difference(z, d), phi, theta)
}

// glsStep re-solves the regression coefficients on prewhitened data and
// returns the replacement for the last cols−skip of them.
//
// design is row-major, n rows by cols columns, and is NOT modified. skip is
// the number of leading columns held fixed: one when d > 0, because the
// intercept column differences to exactly zero and so is both unidentified by
// this objective and a rank-deficient column in it.
func glsStep(y, design []float64, n, cols, skip, d int, phi, theta []float64) ([]float64, error) {
	w := cols - skip
	fy := cssFilter(y, d, phi, theta)
	rows := len(fy)
	if rows <= w {
		// Differencing and conditioning both consume observations, so a design
		// that OLS could solve in levels can be over-determined once filtered.
		// Refusing leaves the staged estimate in place, which is a worse
		// estimate but a real one.
		return nil, errShort
	}
	fd := make([]float64, rows*w)
	col := make([]float64, n)
	for j := 0; j < w; j++ {
		for i := 0; i < n; i++ {
			col[i] = design[i*cols+skip+j]
		}
		fc := cssFilter(col, d, phi, theta)
		for i := 0; i < rows; i++ {
			fd[i*w+j] = fc[i]
		}
	}
	return olsQR(fd, fy, rows, w)
}

// regressErrors returns n_t = y_t − c − beta'X_t for the given coefficients,
// intercept first.
func regressErrors(y, x []float64, n, k int, coef []float64) []float64 {
	e := make([]float64, n)
	for i := 0; i < n; i++ {
		fit := coef[0]
		for j := 0; j < k; j++ {
			fit += coef[1+j] * x[i*k+j]
		}
		e[i] = y[i] - fit
	}
	return e
}

// fitState is the working set of one Fit: the data, the current coefficients,
// the current regression errors and the current ARMA of those errors.
type fitState struct {
	y, x, design []float64
	n, k, cols   int
	skip         int
	ord          Order
	coef         []float64
	errs         []float64
	arma         *arma
}

// refine alternates the two blocks of the fit until the coefficients stop
// moving: hold the ARMA parameters and solve the regression block exactly by
// generalised least squares on prewhitened data, then hold the regression and
// refit the ARMA block to the new errors.
//
// This is block coordinate descent on ONE objective — the conditional sum of
// squares — rather than two separate fits, and that is the difference from the
// staged estimator it replaces. The conditional sum of squares cannot rise
// across a pass: the GLS half-step is the exact minimiser of it over the
// coefficients at fixed phi and theta (cssFilter is linear, so the filtered
// problem is an ordinary least-squares problem in the same residuals), and the
// ARMA half-step is warm started from the current parameters, which
// Nelder-Mead keeps as a simplex vertex and therefore can only improve on.
//
// There is deliberately no runtime check that the objective fell.
// TestPropertyRefineNeverRaisesTheConditionalSumOfSquares measures it instead:
// a guard that cannot fire is a guard no test can enter, and it would report a
// property it never actually verified.
//
// It is not exact maximum likelihood. The objective conditions on the first p
// observations and on zero pre-sample shocks instead of running a Kalman
// filter over the exact likelihood, so the two differ by the information in
// those initial conditions — O(1) terms against a sum of n.
func (s *fitState) refine() {
	if s.ord.P == 0 && s.ord.Q == 0 {
		return // no ARMA to whiten with: GLS would reproduce the OLS fit exactly
	}
	s.skip = 0
	if s.ord.D > 0 {
		s.skip = 1
	}
	if s.cols == s.skip {
		return // a differenced fit with no regressors: no coefficient to solve for
	}
	for it := 0; it < maxRefine; it++ {
		g, err := glsStep(s.y, s.design, s.n, s.cols, s.skip, s.ord.D, s.arma.phi, s.arma.theta)
		if err != nil {
			return
		}
		cand := make([]float64, len(s.coef))
		copy(cand, s.coef)
		copy(cand[s.skip:], g)

		var move, scale float64
		for i := range cand {
			if d := math.Abs(cand[i] - s.coef[i]); d > move {
				move = d
			}
			if a := math.Abs(cand[i]); a > scale {
				scale = a
			}
		}

		start := make([]float64, 0, s.ord.P+s.ord.Q)
		start = append(start, s.arma.phi...)
		start = append(start, s.arma.theta...)
		s.coef = cand
		s.errs = regressErrors(s.y, s.x, s.n, s.k, cand)
		s.arma = cssFit(Difference(s.errs, s.ord.D), s.ord.P, s.ord.Q, start, refineStep)

		if move <= refineTol*(1+scale) {
			return
		}
	}
}
