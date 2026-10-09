package arimax

import (
	"math"
	"testing"
)

// TestParameterRecoveryAccuracy measures how closely Fit recovers KNOWN
// parameters across many independent series. This is the honest way to state
// accuracy for an estimator: generate from a model whose truth you control,
// then report the bias and spread of what comes back.
func TestParameterRecoveryAccuracy(t *testing.T) {
	const (
		trials   = 200
		n        = 500
		truePhi  = 0.60
		trueBeta = 2.50
	)
	var sumPhi, sumBeta, sumPhi2, sumBeta2 float64
	ok := 0
	for s := 0; s < trials; s++ {
		y, x := synth(uint64(s)+1000, n, truePhi, trueBeta)
		m, err := Fit(y, x, 1, Order{P: 1})
		if err != nil {
			continue
		}
		dp := m.AR[0] - truePhi
		db := m.Beta[0] - trueBeta
		sumPhi += dp
		sumBeta += db
		sumPhi2 += dp * dp
		sumBeta2 += db * db
		ok++
	}
	if ok < trials/2 {
		t.Fatalf("only %d/%d fits succeeded", ok, trials)
	}
	biasPhi := sumPhi / float64(ok)
	biasBeta := sumBeta / float64(ok)
	rmsePhi := math.Sqrt(sumPhi2 / float64(ok))
	rmseBeta := math.Sqrt(sumBeta2 / float64(ok))
	t.Logf("n=%d trials=%d  AR(1) bias=%+.4f rmse=%.4f | beta bias=%+.4f rmse=%.4f",
		n, ok, biasPhi, rmsePhi, biasBeta, rmseBeta)

	if math.Abs(biasBeta) > 0.05 {
		t.Errorf("beta bias %.4f exceeds 0.05", biasBeta)
	}
	if math.Abs(biasPhi) > 0.10 {
		t.Errorf("AR(1) bias %.4f exceeds 0.10", biasPhi)
	}
	// The spread, not only the centre. A biasless estimator with twice the
	// variance is a worse estimator, and bias alone cannot see that.
	//
	// This case does NOT discriminate between estimators, and that is worth
	// knowing: synth's x is a smooth sinusoid, so it is strongly
	// autocorrelated, and against autocorrelated errors ordinary least squares
	// is already close to efficient — measured at 0.0087 both with the
	// generalised-least-squares refinement and with it removed. The
	// efficiency loss appears when x is NOT autocorrelated, which is what
	// TestBetaPrecisionUnderCorrelatedErrors measures. The bound here is a
	// spread guard, not a check on the refinement.
	if rmseBeta > 0.015 {
		t.Errorf("beta rmse %.4f exceeds 0.015", rmseBeta)
	}
}

// TestIntervalCoverage measures EMPIRICAL coverage of the 95% prediction
// intervals: over many series, how often does the realised value actually fall
// inside the interval? This is the number that matters when a forecast gates a
// decision — a 95% interval that covers 70% of the time is worse than no
// interval, because it invites confident wrong answers.
func TestIntervalCoverage(t *testing.T) {
	const (
		trials = 300
		n      = 400
		h      = 6
	)
	inside, total := 0, 0
	for s := 0; s < trials; s++ {
		y, x := synth(uint64(s)+5000, n+h, 0.6, 2.5)
		fitY, fitX := y[:n], x[:n]
		m, err := Fit(fitY, fitX, 1, Order{P: 1})
		if err != nil {
			continue
		}
		_, lo, hi, err := m.Forecast(h, x[n:n+h], 0.05)
		if err != nil {
			continue
		}
		for i := 0; i < h; i++ {
			total++
			if y[n+i] >= lo[i] && y[n+i] <= hi[i] {
				inside++
			}
		}
	}
	if total == 0 {
		t.Fatal("no forecasts produced")
	}
	cov := 100 * float64(inside) / float64(total)
	t.Logf("95%% interval empirical coverage: %.1f%% over %d forecast points (h=1..%d)", cov, total, h)
	if cov < 85 || cov > 99.5 {
		t.Errorf("coverage %.1f%% is outside the acceptable 85-99.5%% band for a nominal 95%% interval", cov)
	}
}

// TestForecastBeatsNaive compares h-step forecasts against the naive
// last-value carry-forward on held-out data. A forecaster that cannot beat
// "tomorrow equals today" is not earning its complexity.
func TestForecastBeatsNaive(t *testing.T) {
	const (
		trials = 100
		n      = 400
		h      = 6
	)
	var sumModel, sumNaive float64
	ok := 0
	for s := 0; s < trials; s++ {
		y, x := synth(uint64(s)+9000, n+h, 0.6, 2.5)
		m, err := Fit(y[:n], x[:n], 1, Order{P: 1})
		if err != nil {
			continue
		}
		pt, _, _, err := m.Forecast(h, x[n:n+h], 0.05)
		if err != nil {
			continue
		}
		naive := make([]float64, h)
		for i := range naive {
			naive[i] = y[n-1]
		}
		sumModel += RMSE(y[n:n+h], pt)
		sumNaive += RMSE(y[n:n+h], naive)
		ok++
	}
	if ok == 0 {
		t.Fatal("no trials completed")
	}
	mm, nn := sumModel/float64(ok), sumNaive/float64(ok)
	t.Logf("mean RMSE over %d held-out windows (h=%d): ARIMAX %.4f vs naive %.4f (%.1f%% better)",
		ok, h, mm, nn, 100*(nn-mm)/nn)
	if mm >= nn {
		t.Errorf("ARIMAX RMSE %.4f does not beat naive %.4f", mm, nn)
	}
}

// synthARMA11 generates the model the package is benchmarked on: a regression
// on one independent exogenous regressor whose errors are ARMA(1,1). Both
// series are scaled to unit variance, so the coefficient standard errors below
// can be checked against theory rather than only against each other.
//
// It deliberately draws x independently of the errors. That is the case where
// OLS is still unbiased and purely INEFFICIENT, which is the loss the
// generalised-least-squares refinement exists to recover; a test on correlated
// x would confound efficiency with bias.
func synthARMA11(seed uint64, n int, phi, theta, beta float64) (y, x []float64) {
	const unit = 1.7320508075688772 // sqrt(3): scales lcg's U(-1,1) to variance 1
	g := lcg(seed)
	y = make([]float64, n)
	x = make([]float64, n)
	var nt, prev float64
	for i := 0; i < n; i++ {
		x[i] = g.next() * unit
		e := g.next() * unit
		nt = phi*nt + e + theta*prev
		prev = e
		y[i] = 1 + beta*x[i] + nt
	}
	return y, x
}

// TestBetaPrecisionUnderCorrelatedErrors pins the precision of the exogenous
// coefficient, which is the number the generalised-least-squares refinement in
// refine was added to fix and the number this package was measurably worse at
// than an exact-likelihood fit.
//
// The bar is theory, not a watermark. With errors ARMA(1,1) at phi=0.6,
// theta=0.3 and unit-variance x, the asymptotic standard error of beta is
//
//	OLS:  sqrt(gamma_0 / n)      gamma_0 = (1 + 2*phi*theta + theta^2)/(1 - phi^2) = 2.2656
//	GLS:  sqrt(1 / (n * sum a_j^2))   a = (1 - phi B)/(1 + theta B) = 1, -0.9, 0.27, ...
//
// which at n=500 is 0.0673 for OLS against 0.0325 for GLS — a factor of 2.07.
// A fit that estimates beta as if the errors were independent lands near the
// first figure and CANNOT reach the second, so a floor between them fails the
// moment the refinement is removed or stops being reached. Measured: 0.0329
// with it, 0.0614 without.
func TestBetaPrecisionUnderCorrelatedErrors(t *testing.T) {
	const (
		trials    = 200
		n         = 500
		truePhi   = 0.60
		trueTheta = 0.30
		trueBeta  = 2.50

		// Halfway to the OLS standard error would already be a large
		// regression; this sits well inside that and well above the GLS one.
		maxRMSE = 0.042
		maxBias = 0.020
	)
	var sum, sumSq float64
	ok := 0
	for s := 0; s < trials; s++ {
		y, x := synthARMA11(uint64(s)+31000, n, truePhi, trueTheta, trueBeta)
		m, err := Fit(y, x, 1, Order{P: 1, Q: 1})
		if err != nil {
			continue
		}
		d := m.Beta[0] - trueBeta
		sum += d
		sumSq += d * d
		ok++
	}
	if ok < trials {
		t.Fatalf("only %d/%d fits succeeded", ok, trials)
	}
	bias := sum / float64(ok)
	rmse := math.Sqrt(sumSq / float64(ok))
	t.Logf("beta over %d trials at n=%d: bias=%+.5f rmse=%.5f (GLS theory 0.0325, OLS theory 0.0673)",
		ok, n, bias, rmse)

	if rmse > maxRMSE {
		t.Errorf("beta rmse %.5f exceeds %.3f: the coefficient is being estimated as if the errors were independent", rmse, maxRMSE)
	}
	if math.Abs(bias) > maxBias {
		t.Errorf("beta bias %+.5f exceeds %.3f", bias, maxBias)
	}
}
