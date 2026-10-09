package season

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

// series builds level + slope·t + amp·sin(2πt/period) + Gaussian noise.
func series(n int, level, slope, amp float64, period int, noise float64, seed uint64) []float64 {
	r := rand.New(rand.NewPCG(seed, 7))
	y := make([]float64, n)
	for t := range y {
		y[t] = level + slope*float64(t) + noise*r.NormFloat64()
		if period > 0 {
			y[t] += amp * math.Sin(2*math.Pi*float64(t)/float64(period))
		}
	}
	return y
}

// --- Period ---

// A planted period is recovered, with or without a trend underneath.
func TestPeriodRecoversAPlantedCycle(t *testing.T) {
	for _, c := range []struct {
		period int
		slope  float64
	}{{7, 0}, {7, 0.5}, {12, 0}, {12, -0.3}, {24, 0.1}} {
		for seed := uint64(1); seed <= 5; seed++ {
			y := series(10*c.period+3, 50, c.slope, 3, c.period, 0.3, seed)
			if got := Period(y, 0); got != c.period {
				t.Errorf("period %d slope %v seed %d: got %d", c.period, c.slope, seed, got)
			}
		}
	}
}

// No season is found where there is none: white noise, and a trend plus
// noise. This is the test that keeps the detector from being a pattern-finder.
func TestPeriodFindsNoneInNonSeasonalSeries(t *testing.T) {
	for seed := uint64(1); seed <= 40; seed++ {
		if got := Period(series(200, 0, 0, 0, 0, 1, seed), 0); got != 0 {
			t.Errorf("white noise seed %d: found period %d", seed, got)
		}
		if got := Period(series(200, 10, 0.8, 0, 0, 1, seed), 0); got != 0 {
			t.Errorf("trend+noise seed %d: found period %d", seed, got)
		}
	}
}

// Too short to tell is answered with 0, not a confident guess.
func TestPeriodRefusesShortOrFlatSeries(t *testing.T) {
	if got := Period([]float64{1, 2, 1, 2, 1, 2, 1}, 0); got != 0 {
		t.Errorf("7 points: got %d, want 0 (below MinPeriodLength)", got)
	}
	if got := Period(make([]float64, 50), 0); got != 0 {
		t.Errorf("constant series: got %d", got)
	}
	// A period needs two full cycles in the series.
	if got := Period(series(20, 0, 0, 3, 15, 0.01, 1), 0); got == 15 {
		t.Error("found period 15 in 20 points (fewer than two cycles)")
	}
}

func TestPeriodRespectsMaxPeriod(t *testing.T) {
	y := series(240, 0, 0, 3, 24, 0.2, 3)
	if got := Period(y, 12); got == 24 {
		t.Errorf("maxPeriod 12 still returned 24")
	}
}

// --- FitFourier ---

// The amplitude of a planted cycle is recovered within 5%, including when the
// series is not a whole number of cycles (where the projection shortcut is
// biased; this is why the fit is least squares).
func TestFitFourierRecoversAmplitudeWithin5Percent(t *testing.T) {
	for _, n := range []int{140, 143, 151} {
		y := series(n, 10, 0, 3, 7, 0.3, uint64(n))
		f, err := FitFourier(y, 7, 3)
		if err != nil {
			t.Fatal(err)
		}
		if amp := f.Amplitude(1); math.Abs(amp-3)/3 > 0.05 {
			t.Errorf("n=%d: amplitude %.3f, want 3 ± 5%%", n, amp)
		}
		if math.Abs(f.Mean-10) > 0.2 {
			t.Errorf("n=%d: mean %.3f, want ~10", n, f.Mean)
		}
		if f.Amplitude(2) > 0.25 || f.Amplitude(3) > 0.25 {
			t.Errorf("n=%d: spurious harmonics %.3f %.3f", n, f.Amplitude(2), f.Amplitude(3))
		}
	}
}

func TestFitFourierValidatesAndCapsHarmonics(t *testing.T) {
	y := series(60, 0, 0, 1, 4, 0, 1)
	if _, err := FitFourier(y, 1, 2); err == nil {
		t.Error("period 1 accepted")
	}
	if _, err := FitFourier(y, 4, 0); err == nil {
		t.Error("0 harmonics accepted")
	}
	f, err := FitFourier(y, 4, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.A) != 2 {
		t.Errorf("harmonics not capped at period/2: %d", len(f.A))
	}
	if _, err := FitFourier([]float64{1, 2, 3}, 4, 2); !errors.Is(err, ErrTooShort) {
		t.Errorf("3 points: err = %v, want ErrTooShort", err)
	}
	if f.Amplitude(0) != 0 || f.Amplitude(9) != 0 {
		t.Error("amplitude of a harmonic the fit lacks should be 0")
	}
	if (Fourier{}).At(3) != 0 {
		t.Error("an empty fit should have no seasonal effect")
	}
}

// At reproduces the planted shape.
func TestFourierAtTracksTheCycle(t *testing.T) {
	y := series(140, 0, 0, 2, 10, 0, 1)
	f, err := FitFourier(y, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	for tt := 0; tt < 20; tt++ {
		want := 2 * math.Sin(2*math.Pi*float64(tt)/10)
		if got := f.At(tt); math.Abs(got-want) > 1e-6 {
			t.Fatalf("At(%d) = %.6f, want %.6f", tt, got, want)
		}
	}
}

// --- Changepoints ---

func TestChangepointsFindsAPlantedSlopeBreak(t *testing.T) {
	for seed := uint64(1); seed <= 10; seed++ {
		r := rand.New(rand.NewPCG(seed, 1))
		y := make([]float64, 120)
		for i := range y {
			if i < 60 {
				y[i] = 0.5 * float64(i)
			} else {
				y[i] = 30 - 0.4*float64(i-60)
			}
			y[i] += 0.5 * r.NormFloat64()
		}
		cps := Changepoints(y, 3)
		if len(cps) != 1 || abs(cps[0]-60) > 3 {
			t.Errorf("seed %d: changepoints %v, want one near 60", seed, cps)
		}
	}
}

func TestChangepointsFindsALevelShift(t *testing.T) {
	y := series(100, 10, 0, 0, 0, 0.3, 2)
	for i := 70; i < 100; i++ {
		y[i] += 6
	}
	cps := Changepoints(y, 3)
	if len(cps) != 1 || abs(cps[0]-70) > 2 {
		t.Errorf("changepoints %v, want one near 70", cps)
	}
}

// No breaks are invented in a smooth series: a noisy straight line, pure
// noise, and an exact line all come back with none.
func TestChangepointsInventsNoBreaks(t *testing.T) {
	for seed := uint64(1); seed <= 40; seed++ {
		if cps := Changepoints(series(150, 5, 0.3, 0, 0, 1, seed), 5); len(cps) != 0 {
			t.Errorf("noisy line seed %d: invented %v", seed, cps)
		}
		if cps := Changepoints(series(150, 0, 0, 0, 0, 1, seed), 5); len(cps) != 0 {
			t.Errorf("noise seed %d: invented %v", seed, cps)
		}
	}
	if cps := Changepoints(series(80, 1, 2, 0, 0, 0, 1), 5); len(cps) != 0 {
		t.Errorf("exact line: invented %v", cps)
	}
}

func TestChangepointsShortOrDisabled(t *testing.T) {
	if cps := Changepoints([]float64{1, 2, 3, 10, 11, 12, 13}, 3); cps != nil {
		t.Errorf("7 points (< two minimum segments): %v", cps)
	}
	if cps := Changepoints(series(100, 0, 1, 0, 0, 0.1, 1), 0); cps != nil {
		t.Errorf("maxK 0: %v", cps)
	}
}

// --- Decompose / Forecast ---

// Trend + season + noise is split back into its parts, and the components add
// up to the data exactly.
func TestDecomposeSeparatesTrendAndSeason(t *testing.T) {
	y := series(168, 100, 0.25, 5, 7, 0.4, 11)
	d, err := Decompose(y, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Period != 7 {
		t.Fatalf("period %d, want 7", d.Period)
	}
	if len(d.Changepoints) != 0 {
		t.Errorf("a straight trend gained breaks: %v", d.Changepoints)
	}
	for i := range y {
		if s := d.Trend[i] + d.Seasonal[i] + d.Residual[i]; math.Abs(s-y[i]) > 1e-9 {
			t.Fatalf("components do not add up at %d: %v vs %v", i, s, y[i])
		}
	}
	if sd := stdev(d.Residual); sd > 0.6 {
		t.Errorf("residual sd %.3f, want near the planted 0.4", sd)
	}
}

// The forecast follows the planted signal: mean absolute error over the next
// two weeks stays within 1.0 (noise sd is 0.4).
func TestForecastFollowsTheSignal(t *testing.T) {
	n, h := 168, 14
	y := series(n, 100, 0.25, 5, 7, 0.4, 5)
	d, err := Decompose(y, Options{})
	if err != nil {
		t.Fatal(err)
	}
	fc := d.Forecast(h)
	if len(fc) != h {
		t.Fatalf("forecast length %d", len(fc))
	}
	mae := 0.0
	for i, v := range fc {
		tt := float64(n + i)
		truth := 100 + 0.25*tt + 5*math.Sin(2*math.Pi*tt/7)
		mae += math.Abs(v - truth)
	}
	if mae /= float64(h); mae > 1.0 {
		t.Errorf("forecast MAE %.3f, want <= 1.0", mae)
	}
	if d.Forecast(0) != nil {
		t.Error("a 0-step forecast should be nil")
	}
}

// The trend is continued from the last segment after a break.
func TestForecastContinuesTheLastSegment(t *testing.T) {
	y := make([]float64, 120)
	for i := range y {
		if i < 60 {
			y[i] = float64(i)
		} else {
			y[i] = 60 - 2*float64(i-60)
		}
	}
	d, err := Decompose(y, Options{Period: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Changepoints) != 1 {
		t.Fatalf("changepoints %v", d.Changepoints)
	}
	if fc := d.Forecast(1)[0]; math.Abs(fc-(60-2*60)) > 1e-6 {
		t.Errorf("next value %.4f, want -60 (the falling segment continued)", fc)
	}
}

func TestDecomposeOptionsAndEdges(t *testing.T) {
	if _, err := Decompose([]float64{1, 2, 3}, Options{}); !errors.Is(err, ErrTooShort) {
		t.Errorf("3 points: %v", err)
	}
	if _, err := Decompose([]float64{math.NaN(), math.NaN(), math.NaN(), math.NaN()}, Options{}); err == nil {
		t.Error("an all-NaN series was decomposed")
	}
	// A forced period larger than half the series is dropped rather than fitted.
	d, err := Decompose(series(20, 1, 0, 2, 7, 0.1, 1), Options{Period: 15})
	if err != nil || d.Period != 0 {
		t.Errorf("period 15 in 20 points: period %d err %v, want 0", d.Period, err)
	}
	// Changepoints can be switched off.
	y := series(100, 0, 1, 0, 0, 0.1, 1)
	for i := 50; i < 100; i++ {
		y[i] += 20
	}
	if d, _ := Decompose(y, Options{Period: -1, MaxChangepoints: -1}); len(d.Changepoints) != 0 {
		t.Errorf("MaxChangepoints -1 still found %v", d.Changepoints)
	}
	// A short series still decomposes, with no season.
	if d, err := Decompose([]float64{1, 2, 3, 4, 5, 6}, Options{}); err != nil || d.Period != 0 {
		t.Errorf("6 points: period %d err %v", d.Period, err)
	}
}

// Gaps: a missing value takes the last real one; leading gaps take the first
// real value (not zero, which would drag the trend).
func TestFillHandlesGaps(t *testing.T) {
	got := fill([]float64{math.NaN(), math.NaN(), 5, math.NaN(), 7})
	want := []float64{5, 5, 5, 5, 7}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fill = %v, want %v", got, want)
		}
	}
	if z := fill([]float64{math.NaN(), math.NaN()}); z[0] != 0 || z[1] != 0 {
		t.Errorf("all-NaN fill = %v", z)
	}
	y := series(140, 50, 0, 3, 7, 0.2, 9)
	y[10], y[40], y[41] = math.NaN(), math.NaN(), math.NaN()
	if got := Period(y, 0); got != 7 {
		t.Errorf("period with gaps: %d, want 7", got)
	}
}

func TestSolveReportsSingular(t *testing.T) {
	if _, ok := solve([][]float64{{1, 2}, {2, 4}}, []float64{1, 2}); ok {
		t.Error("a singular system was solved")
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func stdev(v []float64) float64 {
	m := 0.0
	for _, x := range v {
		m += x
	}
	m /= float64(len(v))
	s := 0.0
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(v)))
}

// The documented false-season rate holds: over 500 white-noise series of 200
// points, fewer than 1% are given a season.
func TestPeriodFalseSeasonRateUnderOnePercent(t *testing.T) {
	const runs = 500
	found := 0
	for seed := uint64(1000); seed < 1000+runs; seed++ {
		if Period(series(200, 0, 0, 0, 0, 1, seed), 0) != 0 {
			found++
		}
	}
	if rate := float64(found) / runs; rate >= 0.01 {
		t.Errorf("white noise given a season in %d of %d runs (%.1f%%), documented < 1%%", found, runs, 100*rate)
	}
	t.Logf("false seasons: %d of %d", found, runs)
}

// --- Classical ---

// The refusals, each of which is a question the method cannot answer rather
// than a number it could have guessed.
func TestClassicalRefusals(t *testing.T) {
	y := series(200, 100, 0.05, 10, 24, 1, 5)
	for _, p := range []int{-7, 0, 1} {
		if _, err := Classical(y, p); err == nil {
			t.Errorf("Classical(period=%d) returned a decomposition", p)
		}
	}
	// Two full cycles is the floor: below it some phase of the season has no
	// point at all to average.
	if _, err := Classical(y[:47], 24); !errors.Is(err, ErrTooShort) {
		t.Errorf("47 points at period 24: err = %v, want ErrTooShort", err)
	}
	if _, err := Classical(y[:48], 24); err != nil {
		t.Errorf("48 points at period 24 is exactly two cycles: %v", err)
	}
	nan := make([]float64, 100)
	for i := range nan {
		nan[i] = math.NaN()
	}
	if _, err := Classical(nan, 12); err == nil {
		t.Error("a series of NaN returned a decomposition")
	}
}

// An odd period takes a plain average of period points; an even one takes
// period+1 with half weight at each end. Both are checked against the
// definition computed the slow, obvious way, which is what the running window
// sum in Classical is an optimisation of.
func TestClassicalMatchesTheDefinition(t *testing.T) {
	for _, p := range []int{2, 3, 4, 7, 12, 24, 25} {
		y := series(10*p+5, 50, 0.3, 8, p, 0.7, uint64(p))
		d, err := Classical(y, p)
		if err != nil {
			t.Fatalf("p=%d: %v", p, err)
		}
		h := p / 2
		for t0 := h; t0 < len(y)-h; t0++ {
			want := 0.0
			if p%2 == 0 {
				want += 0.5 * (y[t0-h] + y[t0+h])
				for j := t0 - h + 1; j < t0+h; j++ {
					want += y[j]
				}
			} else {
				for j := t0 - h; j <= t0+h; j++ {
					want += y[j]
				}
			}
			want /= float64(p)
			if got := d.Trend[t0]; math.Abs(got-want) > 1e-9 {
				t.Fatalf("p=%d: trend[%d] = %v, the centred average is %v", p, t0, got, want)
			}
		}
		// The ends hold the nearest defined average, and nothing else.
		for t0 := 0; t0 < h; t0++ {
			if d.Trend[t0] != d.Trend[h] {
				t.Fatalf("p=%d: trend[%d] = %v, want the held %v", p, t0, d.Trend[t0], d.Trend[h])
			}
		}
		last := len(y) - 1 - h
		for t0 := last + 1; t0 < len(y); t0++ {
			if d.Trend[t0] != d.Trend[last] {
				t.Fatalf("p=%d: trend[%d] = %v, want the held %v", p, t0, d.Trend[t0], d.Trend[last])
			}
		}
	}
}

// The season repeats, exactly, because it is one value per phase and nothing
// else. A caller indexing it a cycle apart must get the same number.
func TestClassicalSeasonIsOnePatternRepeated(t *testing.T) {
	const p = 18
	y := series(13*p, 10, -0.2, 4, p, 0.5, 11)
	d, err := Classical(y, p)
	if err != nil {
		t.Fatal(err)
	}
	for i := range y {
		if j := i + p; j < len(y) && d.Seasonal[i] != d.Seasonal[j] {
			t.Fatalf("season[%d] = %v but season[%d] = %v", i, d.Seasonal[i], j, d.Seasonal[j])
		}
	}
}

// Holes are forward-filled, as everywhere else here, so a series with gaps
// still decomposes and still adds up -- to the FILLED series, which is what
// the components describe.
func TestClassicalFillsHoles(t *testing.T) {
	const p = 12
	y := series(8*p, 30, 0.1, 5, p, 0.4, 13)
	holed := append([]float64(nil), y...)
	holed[0], holed[1], holed[40], holed[len(holed)-1] = math.NaN(), math.NaN(), math.NaN(), math.NaN()
	d, err := Classical(holed, p)
	if err != nil {
		t.Fatal(err)
	}
	filled := fill(holed)
	for i := range filled {
		sum := d.Trend[i] + d.Seasonal[i] + d.Residual[i]
		if math.Abs(sum-filled[i]) > 1e-9 {
			t.Fatalf("point %d: components sum to %v, filled series is %v", i, sum, filled[i])
		}
	}
}
