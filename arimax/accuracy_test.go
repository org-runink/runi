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
