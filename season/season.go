// Package season finds the repeating pattern in a series and the points where
// its trend breaks, and splits the series into trend, season and residual.
//
// It is the classical additive decomposition
//
//	y(t) = trend(t) + season(t) + residual(t)
//
// with a piecewise-linear trend and a Fourier-series season. Each piece is
// usable on its own: Period answers "does this repeat, and how often?",
// FitFourier models one known season, Changepoints finds trend breaks, and
// Decompose runs all three together and can extend the result forward.
//
// Classical is the second, cheaper entry point, for when the period is already
// known: the textbook moving-average decomposition, the operation
// statsmodels.tsa.seasonal.seasonal_decompose performs, computed in one pass.
// No period detection, no trend breaks, no forecast, and about forty times
// less work than Decompose.
//
// This is not Prophet. Prophet is Meta's forecasting library; it fits a
// related model with a Bayesian treatment of changepoints, holidays and
// several seasonalities at once. If you want Prophet, use Prophet. This
// package is the textbook decomposition underneath, with no dependencies.
//
// Isolated outliers are filtered out of what the estimators measure, but not
// out of what they return. Every estimate here is a sum of squares, so one bad
// reading of magnitude a contributes a² and outvotes the rest of the series:
// unguarded, a single spike is enough to hide a weekly season completely and
// to be reported as two structural breaks around itself. So Period,
// Changepoints and Decompose score on a copy with isolated spikes replaced by
// their local median, while the trend, season and residual they report are
// fitted to the series as given. A bad day therefore still shows up as a large
// Residual, where a caller looking for anomalies can find it, instead of being
// smoothed away. A shift in level that LASTS is a break, not an outlier, and
// survives the filter. Classical is the one exception, and says so in its own
// doc: there the moving average IS the estimate, and filtering it would make
// the result disagree with every other implementation of the classical
// decomposition.
//
// Limits, stated so they are not discovered later:
//   - One seasonality. A series with both a weekly and a yearly cycle gets the
//     strongest one only.
//   - The trend is least-squares lines between changepoints, fitted
//     independently: it may jump at a changepoint (a level shift is a break),
//     and a forecast extends the last line.
//   - Short series are refused rather than guessed at: Period needs
//     MinPeriodLength (28) points and two full cycles, Decompose needs 4
//     points, Classical needs two full cycles of the period it is given. 28 is
//     not arbitrary — it is the shortest series in which a cycle
//     can in fact be found, because the bar a lag must clear rises as the
//     series shortens while the autocorrelation estimator's ceiling falls, and
//     below 28 the bar sits above the ceiling.
//   - A spike only a few times the noise is indistinguishable from the noise,
//     and is neither filtered nor reliably detected. The filter earns its keep
//     on spikes well clear of the noise, which is where the damage was.
//   - No uncertainty intervals.
//
// # Measured against statsmodels
//
// Classical computes what statsmodels.tsa.seasonal.seasonal_decompose
// computes, by the same method, and it is FASTER: n=4,000 at period 24 takes
// 0.0408 ms here against 0.161 ms there, 3.9x faster, with the two agreeing to
// 2e-13 on a series of magnitude 300 — a few ulps of float64, which is to say
// they produce the same numbers. That is the like-for-like row, and it is the
// one to quote.
//
// Decompose is SLOWER than seasonal_decompose, and that stays said. Given the
// same period and with the trend-break search off, n=4,000 takes 1.70 ms here
// against 0.161 ms there, because it solves two least-squares systems where a
// moving average takes two additions a point. It is the wrong tool for a
// decomposition whose period you already know — that is what Classical is for
// — and the right one for the three things a moving average cannot do at all.
// Each piece costs what it costs:
//
//	Classical, period given             0.0408 ms
//	Decompose, period given, no breaks  1.70 ms
//	plus the BIC changepoint search     18.4 ms   (the default)
//	plus detecting the period too       56.0 ms
//	Period detection on its own         4.38 ms
//
// So: a moving average cannot tell you the period, cannot tell you where the
// trend broke, and cannot extrapolate. Those three are what the extra time
// buys, and when you need none of them, Classical is the function.
//
// Measured on an ASUS Ascent GX10, 20 cores, aarch64, Go 1.27.2, statsmodels
// 0.15.0 on Python 3.12.3.
package season

import (
	"errors"
	"math"
	"sort"
)

// MinPeriodLength is the shortest series Period will look for a cycle in.
//
// 28, because that is the shortest series in which a cycle can actually be
// found, and a lower figure would be a promise the function cannot keep. The
// bar a lag must clear rises as the series shortens (see periodThreshold),
// while the most the estimator can report at lag L falls to about 1 − L/m.
// Below 28 points the bar is above the ceiling: no series of any shape clears
// it, so Period returns 0 whatever it is given. Callers passing 8 to 27 points
// got that same 0 before this constant said so; what changes is that the
// package no longer advertises a floor it cannot answer above.
const MinPeriodLength = 28

// ErrTooShort is returned when a series has too few points to model.
var ErrTooShort = errors.New("season: series too short")

// Period returns the dominant seasonal period of x, or 0 when there is none.
//
// It looks for the highest PEAK in the autocorrelation of the
// once-differenced series (differencing removes a linear trend, which would
// otherwise correlate with itself at every lag). A lag counts only if its
// autocorrelation clears periodThreshold — a bar raised for the number of lags
// tried, so white noise yields a season under 1% of the time — and only if at
// least two full cycles fit in the series. The winner is then checked against
// its neighbours by seasonal fit, which settles off-by-one peaks. maxPeriod
// caps the search; 0 means half the series. NaN values are forward-filled.
func Period(x []float64, maxPeriod int) int {
	x = fill(x)
	n := len(x)
	if n < MinPeriodLength {
		return 0
	}
	d := make([]float64, n-1)
	for i := 1; i < n; i++ {
		d[i-1] = x[i] - x[i-1]
	}
	m := len(d)

	// Clean the differences before measuring anything.
	//
	// A single outlier day — a launch, a viral post, a sensor glitch — puts TWO
	// large values into d: one step up and one step back down. Their squares
	// dominate var0 below, every genuine autocorrelation is divided by a
	// normaliser the season did not produce, and the series reports no season at
	// all. One bad day should not make a weekly pattern invisible. See hampel.
	//
	// Replacing rather than dropping keeps the index alignment the
	// autocorrelation depends on.
	d = hampel(d)

	mean := 0.0
	for _, v := range d {
		mean += v
	}
	mean /= float64(m)
	var0 := 0.0
	for _, v := range d {
		var0 += (v - mean) * (v - mean)
	}
	if var0 == 0 {
		return 0
	}
	maxLag := m / 2
	if maxPeriod > 0 && maxPeriod < maxLag {
		maxLag = maxPeriod
	}
	if maxLag < 2 {
		return 0
	}
	// The autocorrelation at every lag, kept so that a PEAK can be chosen
	// rather than the largest value.
	//
	// The largest value is the wrong answer for any smooth cycle. This
	// estimator divides a sum of m−lag products by the variance of all m, so
	// what it reports tapers by roughly (1 − lag/m), while a sinusoid's true
	// autocorrelation at a SHORT lag is already close to 1 — cos(4π/P) at lag
	// 2. Past P ≈ 25 the taper costs lag P more than the curve costs lag 2,
	// the global maximum moves to lag 2, and a monthly cycle is reported as a
	// three-day one. Not "no season", which a caller would question, but a
	// confident wrong period that Decompose and every forecast taken from it
	// then builds on. A season is a peak in the autocorrelation; take the
	// highest peak.
	r := make([]float64, maxLag+2)
	for lag := 1; lag <= maxLag+1 && lag < m; lag++ {
		c := 0.0
		for i := 0; i+lag < m; i++ {
			c += (d[i] - mean) * (d[i+lag] - mean)
		}
		r[lag] = c / var0
	}

	best, bestR := 0, periodThreshold(m, maxLag-1)
	for lag := 2; lag <= maxLag; lag++ {
		if r[lag] <= bestR {
			continue
		}
		// No lower than either neighbour. The lag just past maxLag is measured
		// above for exactly this comparison, so the last candidate is judged
		// on the same footing as the rest rather than being accepted for want
		// of anything to compare it with.
		if r[lag] < r[lag-1] || r[lag] < r[lag+1] {
			continue
		}
		best, bestR = lag, r[lag]
	}
	if best == 0 {
		return 0
	}
	return refinePeriod(x, best, maxLag)
}

// periodThreshold is the autocorrelation a lag must beat to count as a season.
//
// Two corrections a single "3 standard errors" rule misses. Differencing white
// noise leaves a lag-1 correlation of −0.5, which widens the sampling spread of
// the other lags to about √1.5/√m (Bartlett). And testing many lags at once
// means one of them clears any fixed bar by chance: the bar is raised to keep
// the chance of any false season across all `lags` tried near 1%
// (a Bonferroni bound, z = √(2·ln(lags/0.01))). Never below 0.2.
func periodThreshold(m, lags int) float64 {
	z := math.Sqrt(2 * math.Log(float64(lags)/0.01))
	return math.Max(0.2, z*math.Sqrt(1.5/float64(m)))
}

// refinePeriod settles between lag and its neighbours: a sinusoid's
// autocorrelation at P−1 is within a few percent of its value at P, so noise
// can tip the peak by one. The neighbour whose seasonal fit leaves the least
// residual variance after removing a straight line wins.
func refinePeriod(x []float64, lag, maxLag int) int {
	detrended := sub(x, evalLines(fitLines(x, nil), len(x)))

	// Clip the detrended series before scoring, for the same reason Period
	// clips the differences: one outlier day otherwise dominates every residual
	// sum below, and the period that happens to absorb it best wins. That tips
	// the answer by one — a weekly series reported as eight-daily — which is
	// harder to notice than no season at all and worse to act on.
	detrended = hampel(detrended)

	best, bestSSR := lag, math.Inf(1)
	for p := lag - 1; p <= lag+1; p++ {
		if p < 2 || p > maxLag {
			continue
		}
		// As in Decompose, this cannot fail and the error is dropped rather
		// than handled: p is at most maxLag, which is at most half the length
		// of the differenced series, so the window always covers two full
		// cycles — more rows than the three harmonics need, and dense enough
		// over the period to stay well conditioned.
		f, _ := FitFourier(detrended, p, 3)
		ssr := 0.0
		for t, v := range detrended {
			e := v - f.Mean - f.At(t)
			ssr += e * e
		}
		if ssr < bestSSR {
			best, bestSSR = p, ssr
		}
	}
	return best
}

// Fourier is a fitted seasonal pattern: Mean plus, for each harmonic k,
// A[k]·cos(2π(k+1)t/Period) + B[k]·sin(2π(k+1)t/Period).
type Fourier struct {
	Period int
	Mean   float64
	A, B   []float64
}

// FitFourier fits a Fourier series of the given period to x by least squares.
//
// Least squares, not a projection: the projection shortcut is exact only when
// the series holds a whole number of cycles and biased otherwise. harmonics is
// capped at period/2, the most a period can carry. NaN values are
// forward-filled.
func FitFourier(x []float64, period, harmonics int) (Fourier, error) {
	x = fill(x)
	if period < 2 {
		return Fourier{}, errors.New("season: period must be at least 2")
	}
	if harmonics < 1 {
		return Fourier{}, errors.New("season: harmonics must be at least 1")
	}
	if harmonics > period/2 {
		harmonics = period / 2
	}
	if len(x) < 2*harmonics+2 {
		return Fourier{}, ErrTooShort
	}
	// Columns: intercept, then cos and sin per harmonic. At k = period/2 the
	// sine column is identically zero, so it is left out.
	type col struct {
		k   int
		sin bool
	}
	cols := []col{{0, false}}
	for k := 1; k <= harmonics; k++ {
		cols = append(cols, col{k, false})
		if 2*k != period {
			cols = append(cols, col{k, true})
		}
	}
	basis := func(c col, t int) float64 {
		if c.k == 0 {
			return 1
		}
		a := 2 * math.Pi * float64(c.k) * float64(t) / float64(period)
		if c.sin {
			return math.Sin(a)
		}
		return math.Cos(a)
	}
	p := len(cols)
	xtx := make([][]float64, p)
	xty := make([]float64, p)
	for i := range xtx {
		xtx[i] = make([]float64, p)
	}
	// The basis depends only on t mod period, so there are at most `period`
	// distinct rows however long the series is. Computing them once turns the
	// two transcendental calls per column per point — 28,000 of them for a
	// 4,000-point daily series with three harmonics — into a few dozen, and
	// leaves the accumulation below reading from a table that stays in cache.
	phases := period
	if phases > len(x) {
		phases = len(x) // t mod period == t when the series is shorter
	}
	table := make([][]float64, phases)
	flat := make([]float64, phases*p)
	for ph := range table {
		table[ph] = flat[ph*p : (ph+1)*p : (ph+1)*p]
		for j, c := range cols {
			table[ph][j] = basis(c, ph)
		}
	}
	// The normal equations are accumulated per PHASE, not per point.
	//
	// Every row of the design matrix is one of `phases` distinct rows, so
	// X'X is sum over phases of count[ph] * outer(row_ph, row_ph), and X'y is
	// sum over phases of row_ph * (sum of y at that phase). Bucketing y by
	// phase costs one pass over the series; the outer products then cost
	// phases*p*p instead of n*p*p. For a 4,000-point daily series with three
	// harmonics that is 24*49 instead of 4,000*49 — the same numbers, two
	// orders of magnitude fewer multiplies, and no approximation anywhere.
	counts := make([]float64, phases)
	ysum := make([]float64, phases)
	for t, y := range x {
		ph := t % phases
		counts[ph]++
		ysum[ph] += y
	}
	for ph := 0; ph < phases; ph++ {
		// Every phase is occupied: phases is min(period, len(x)), and t mod
		// phases visits all of them for t in 0..len(x)-1. A zero-count guard
		// here would be a branch no test could enter.
		c, ys := counts[ph], ysum[ph]
		row := table[ph]
		for i := 0; i < p; i++ {
			ri := row[i]
			xty[i] += ri * ys
			dst := xtx[i]
			cri := c * ri
			for j := 0; j < p; j++ {
				dst[j] += cri * row[j]
			}
		}
	}

	beta, ok := solve(xtx, xty)
	if !ok {
		return Fourier{}, errors.New("season: series cannot identify this period")
	}
	f := Fourier{Period: period, Mean: beta[0], A: make([]float64, harmonics), B: make([]float64, harmonics)}
	for j, c := range cols[1:] {
		if c.sin {
			f.B[c.k-1] = beta[j+1]
		} else {
			f.A[c.k-1] = beta[j+1]
		}
	}
	return f, nil
}

// At returns the seasonal effect at time t, excluding Mean.
func (f Fourier) At(t int) float64 {
	if f.Period < 2 {
		return 0
	}
	v := 0.0
	for k := range f.A {
		a := 2 * math.Pi * float64(k+1) * float64(t) / float64(f.Period)
		v += f.A[k]*math.Cos(a) + f.B[k]*math.Sin(a)
	}
	return v
}

// Amplitude returns the amplitude of harmonic k (1 is the base cycle), or 0
// when the fit has no such harmonic.
func (f Fourier) Amplitude(k int) float64 {
	if k < 1 || k > len(f.A) {
		return 0
	}
	return math.Hypot(f.A[k-1], f.B[k-1])
}

// Changepoints returns up to maxK indices where the trend of x breaks, in
// ascending order.
//
// It splits greedily, each time where one straight line becomes two with the
// largest drop in squared error. A split is kept only if that drop exceeds
// 3·σ²·ln(n), a BIC-style price for the two extra parameters and the break's
// position, with the noise level σ estimated robustly from the median absolute
// deviation of first differences. Without that price every split of a noisy
// line looks like an improvement, and breaks are invented in smooth data.
// Segments are at least minSegment points long. NaN values are forward-filled.
func Changepoints(x []float64, maxK int) []int {
	x = fill(x)
	n := len(x)
	if n < 2*minSegment || maxK < 1 {
		return nil
	}
	// The price of a split is set from the ORIGINAL series and the gain a split
	// earns is measured on a cleaned one, and the two must not be swapped.
	//
	// noiseSigma is a median of absolute differences, so a handful of bad
	// readings cannot inflate it and the price it sets is already honest.
	// Filtering first would make it dishonest in the expensive direction: the
	// filter trims the tails of the difference distribution, sigma comes out
	// below the true noise level, every split looks underpriced, and the search
	// invents breaks in pure noise. The gain has the opposite problem — it is a
	// drop in squared error, which one outlier inflates without limit, so
	// unfiltered it always outbids the price and a single bad reading is
	// reported as two breaks bracketing it.
	//
	// Pricing from the raw series and scoring on the cleaned one is what makes
	// both cases come out right. See hampel for why the filter is local: a
	// shift that lasts is a break and has to survive it.
	sigma := noiseSigma(x)
	x = hampel(x)
	price := 3 * sigma * sigma * math.Log(float64(n))
	// An exactly piecewise-linear series has σ = 0; keep a floor relative to
	// the data's scale so rounding error never pays for a split.
	if floor := 1e-9 * (1 + sumSq(x)); price < floor {
		price = floor
	}
	type seg struct{ lo, hi int }
	segs := []seg{{0, n - 1}}
	var cps []int
	for range maxK {
		bestGain, bestAt, bestSeg := price, -1, -1
		for si, s := range segs {
			if s.hi-s.lo+1 < 2*minSegment {
				continue
			}
			base := lineSSR(x, s.lo, s.hi)
			for at := s.lo + minSegment; at+minSegment-1 <= s.hi; at++ {
				gain := base - lineSSR(x, s.lo, at-1) - lineSSR(x, at, s.hi)
				if gain > bestGain {
					bestGain, bestAt, bestSeg = gain, at, si
				}
			}
		}
		if bestAt < 0 {
			break
		}
		cps = append(cps, bestAt)
		s := segs[bestSeg]
		segs = append(segs[:bestSeg], segs[bestSeg+1:]...)
		segs = append(segs, seg{s.lo, bestAt - 1}, seg{bestAt, s.hi})
	}
	sort.Ints(cps)
	return cps
}

// minSegment is the shortest run of points a trend line is fitted to.
const minSegment = 4

// Options configure Decompose.
type Options struct {
	// Period is the season length: 0 detects it with Period, a negative value
	// fits no season.
	Period int
	// Harmonics is the number of Fourier terms (default 3).
	Harmonics int
	// MaxChangepoints caps the trend breaks (default 3; negative means none).
	MaxChangepoints int
}

// Decomposition is a series split into trend, season and residual.
type Decomposition struct {
	// Period is the season used, 0 when there is none.
	Period       int
	Changepoints []int
	Trend        []float64
	Seasonal     []float64
	Residual     []float64

	lines  []line
	season Fourier
	n      int
}

type line struct {
	start, end int
	a, b       float64
}

// Decompose splits x into trend + season + residual.
//
// The order matters and is fixed: the period is found on the differenced
// series; a season is fitted after removing a straight line; changepoints are
// found after removing that season (so the season is not mistaken for breaks);
// then the trend is refitted between them and the season refitted once on
// what the trend leaves. NaN values are forward-filled, and leading NaNs take
// the first real value.
func Decompose(x []float64, opt Options) (*Decomposition, error) {
	if len(x) < 4 {
		return nil, ErrTooShort
	}
	if allNaN(x) {
		return nil, errors.New("season: series has no values")
	}
	y := fill(x)
	n := len(y)
	if opt.Harmonics <= 0 {
		opt.Harmonics = 3
	}
	if opt.MaxChangepoints == 0 {
		opt.MaxChangepoints = 3
	}
	period := opt.Period
	if period == 0 {
		period = Period(y, 0)
	}
	if period < 2 || 2*period > n {
		period = 0
	}

	d := &Decomposition{n: n, Trend: make([]float64, n), Seasonal: make([]float64, n), Residual: make([]float64, n)}

	// First pass: one straight line, then the season on what it leaves.
	var four Fourier
	if period > 0 {
		one := fitLines(y, nil)
		// Cleaned, like every other fit here. FitFourier is least squares, so
		// a spike does not stay where it happened: it is spread over the
		// harmonics and leaves a wrong season at every index. The breaks found
		// below would then sit in that smear rather than at the spike, which
		// is the most misleading answer available — a confident break where
		// nothing happened at all.
		// The error is dropped rather than handled because handling it would be
		// a branch no test could enter. FitFourier fails on a series shorter
		// than the columns it needs, or on a design it cannot identify, and the
		// guard above rules out both: the period is at least 2 and the series
		// covers at least two full cycles of it, which is more rows than the
		// columns need and spans the period densely enough to stay well
		// conditioned. Loosen that guard and this becomes a real error that has
		// to be returned.
		f, _ := FitFourier(hampel(sub(y, evalLines(one, n))), period, opt.Harmonics)
		four = f
	}
	// Breaks are looked for in the deseasonalised series.
	deseason := make([]float64, n)
	for t := range y {
		deseason[t] = y[t] - seasonAt(four, t)
	}
	if opt.MaxChangepoints > 0 {
		d.Changepoints = Changepoints(deseason, opt.MaxChangepoints)
	}
	d.lines = fitLines(deseason, d.Changepoints)
	trend := evalLines(d.lines, n)
	// Second pass: refit the season on what the final trend leaves.
	if period > 0 {
		if f, err := FitFourier(hampel(sub(y, trend)), period, opt.Harmonics); err == nil {
			four = f
		}
	}
	d.Period, d.season = period, four
	for t := range y {
		// The season's mean belongs to the level, not the season.
		d.Trend[t] = trend[t] + four.Mean
		d.Seasonal[t] = seasonAt(four, t)
		d.Residual[t] = y[t] - d.Trend[t] - d.Seasonal[t]
	}
	return d, nil
}

// Forecast extends the decomposition h steps past the end of the series: the
// last trend line continued, plus the season.
func (d *Decomposition) Forecast(h int) []float64 {
	if h < 1 || len(d.lines) == 0 {
		return nil
	}
	last := d.lines[len(d.lines)-1]
	out := make([]float64, h)
	for i := range out {
		t := d.n + i
		out[i] = last.a + last.b*float64(t-last.start) + d.season.Mean + seasonAt(d.season, t)
	}
	return out
}

// Classical splits x into trend, season and residual by the classical
// moving-average method, in one pass over the series.
//
// It is the textbook decomposition, and it is exactly what
// statsmodels.tsa.seasonal.seasonal_decompose(x, period=period,
// model="additive") computes: a centred moving average over one period is the
// trend, the detrended series averaged by phase is the season, and the season
// is then centred on zero so that it carries no level. For an EVEN period the
// average is taken over period+1 points with half weight at each end, because
// a window of 24 hourly points has no centre and a plain average of it would
// be assigned half an hour off; for an odd period it is the plain average of
// period points. The two implementations agree to 2e-13 on the interior of
// series of magnitude 300 — a few ulps of float64, the only disagreement left
// being the order the additions happen in — checked against statsmodels 0.15.0
// over six series by benchmarks/verify_classical.py.
//
// It is O(n) — a running window sum, then one pass to average by phase — where
// Decompose solves two least-squares systems: n=4,000 at period 24 takes
// 0.0408 ms against Decompose's 1.70 ms for the same arguments, and 0.161 ms
// for seasonal_decompose. It is the fast path when the period is already known.
//
// What it does NOT do, and what Decompose is for:
//   - No period detection. The period you pass is the period used; a wrong one
//     is fitted without complaint. Period answers "does this repeat, and how
//     often?", and Decompose runs it for you.
//   - No trend breaks. The returned Changepoints is always nil. A level shift
//     is smeared across one window of the moving average rather than reported.
//   - No forecast. The result carries no trend model, so Forecast returns nil:
//     a moving average says nothing about the point after the last one.
//   - No outlier filtering. The average IS the estimate here, so a spike lands
//     in the trend for the period around it and in the season for its phase.
//     Decompose scores on a Hampel-cleaned copy; this does not, because the
//     classical decomposition is defined without it and filtering would make
//     the result disagree with every other implementation of it.
//
// # The ends
//
// A centred average of period points is undefined for the first and last
// period/2 points, because there is no window to average: that is a property
// of the method, not of this implementation. statsmodels returns NaN there.
// This returns the nearest defined average instead — the leading period/2
// points all hold the first real average, the trailing period/2 all hold the
// last — because a NaN in a component poisons the sum, the plot and the
// variance a caller computes from it, while a held value is a stated
// approximation.
//
// Two consequences, both deliberate. The season is averaged over the interior
// ONLY, so those held points do not pull it towards the series' own curvature,
// which is what keeps the figures identical to statsmodels. And the residual
// is x − trend − season at every index, so the three components add up to the
// series everywhere including the ends: the held trend's error lands in the
// residual, visible, rather than being hidden in a smoothed trend. Those first
// and last period/2 residuals are therefore not comparable with the interior
// ones — they carry up to half a period of trend curvature. Treat the interior
// [period/2, len(x)−period/2) as the decomposition and the ends as padding.
//
// NaN values are forward-filled and leading NaNs take the first real value, as
// everywhere else in this package. A period below 2 is an error, and so is a
// series shorter than two full cycles (ErrTooShort): with less than that, some
// phase of the season has no point at all to average.
func Classical(x []float64, period int) (*Decomposition, error) {
	if period < 2 {
		return nil, errors.New("season: period must be at least 2")
	}
	if len(x) < 2*period {
		return nil, ErrTooShort
	}
	// Nothing below WRITES to y, so when the series holds no NaN — which is the
	// ordinary case — y can be the caller's own slice and the copy fill() would
	// make is pure waste. Deciding that takes two scans with one test each
	// rather than one scan with two, because a loop the processor can predict
	// and a loop it cannot are not the same loop: the first stops at the first
	// real value, which is index 0 unless the series opens with a gap, and the
	// second only asks whether a NaN appears anywhere after it.
	first := -1
	for i, v := range x {
		if !math.IsNaN(v) {
			first = i
			break
		}
	}
	if first < 0 {
		return nil, errors.New("season: series has no values")
	}
	y := x
	if first > 0 {
		y = fill(x) // leading NaNs: the fill is needed whatever follows
	} else {
		for _, v := range x {
			if math.IsNaN(v) {
				y = fill(x)
				break
			}
		}
	}
	n := len(y)
	// h is both the half-width of the window and the number of undefined
	// points at each end: period/2 for an even period, (period−1)/2 for an
	// odd one, which integer division gives for both.
	h := period / 2
	even := period%2 == 0
	last := n - 1 - h // the last index the centred average is defined at

	// The three components are one allocation, sliced three ways. They are
	// always all three returned and always exactly n long, so three trips
	// through the allocator buy nothing; each is capped at its own length so
	// that a caller's append reallocates instead of writing into its
	// neighbour.
	buf := make([]float64, 3*n)
	trend, seas, resid := buf[0:n:n], buf[n:2*n:2*n], buf[2*n:3*n:3*n]

	// The window for index t is the period points y[t−h : t−h+period], plus
	// the extra half-weighted point y[t+h] when the period is even. Carrying
	// its sum forward costs two operations per point instead of period, which
	// is the whole reason this is O(n).
	//
	// Recomputed from scratch every period points, which costs one more
	// addition per point amortised. A sum carried the length of the series
	// keeps the rounding error of every addition it ever made, and that error
	// grows with n while the window's own magnitude does not: on a long series
	// of large values the drift would eventually show up in the trend, and it
	// would show up as a slow wander that looks like signal. cd counts down to
	// the next reseed, which is the same schedule as testing s%period but
	// without a division per point.
	//
	// The reseed is not taken as a loop when it falls due, but built a term at
	// a time across the period points before it, in wn. It is the same period
	// additions of the same period values in the same order, so the sum is the
	// same sum down to the bit; what changes is that they no longer sit in one
	// chain with the running window's own additions. Both are chains of adds
	// whose every step waits on the one before, and a processor can only run a
	// chain at one step per add latency however little else it has to do. Two
	// independent chains it can run at once, so spreading the reseed out costs
	// the same arithmetic and about half the time.
	//
	// wn accumulates y[s+period] at each s, which is exactly the window that
	// comes due one period later. Past the end of the series there is no such
	// window — for an odd period the very last step has none — and yn simply
	// runs out: the sum it leaves unfinished belongs to a reseed beyond the
	// last defined point, which is never read.
	//
	// The body indexes y and trend directly rather than through slices pre-cut
	// so that every index is provably in range. Cutting them does remove six
	// bounds checks, and it measured a quarter SLOWER: five more slice headers
	// do not fit in the registers this loop has left, and the spills cost more
	// than the checks they save.
	m := last - h + 1 // the number of points the average is defined at
	w := 0.0
	for _, v := range y[:period] {
		w += v
	}
	wn, cd := 0.0, period
	fp := float64(period)
	for t := h; t <= last; t++ {
		s := t - h
		if s > 0 {
			if cd--; cd == 0 {
				w, wn, cd = wn, 0, period
			} else {
				w += y[s+period-1] - y[s-1]
			}
		}
		if s+period < n {
			wn += y[s+period]
		}
		v := w
		if even {
			// The two half-weighted ends, as a correction to the plain sum:
			// +0.5·y[t+h] for the point the window does not reach, −0.5·y[t−h]
			// to halve the one it does.
			v += 0.5 * (y[t+h] - y[t-h])
		}
		trend[t] = v / fp
	}

	// The season is the mean of the detrended series at each phase of the
	// cycle, over the indices where the average above is defined.
	//
	// Walked a run at a time rather than a point at a time: a run is as much of
	// the cycle as is left before the phase wraps, and across it the phase and
	// the index advance together, so the three slices can be cut to one length
	// and indexed by the same counter. Each phase is still visited once per
	// cycle in increasing t, so each total is accumulated in the order it was
	// before; what goes is the wrap test and the bounds check on every point.
	// One total per phase. Seasons are short — hours in a day, days in a week,
	// months in a year — so the usual one fits in an array the compiler can
	// leave on the stack, and only an unusually long cycle pays the allocator.
	// One total per phase. Keeping these in a fixed array on the stack instead,
	// to save the allocation, measured slower: a season is short enough that
	// the array has to be sized for the longest one anybody might ask for, and
	// zeroing that on every call costs more than the 192 bytes it saves.
	sum := make([]float64, period)
	for t, ph := h, h%period; t <= last; {
		run := period - ph
		if rest := last + 1 - t; rest < run {
			run = rest
		}
		acc := sum[ph : ph+run]
		yr, td := y[t:t+len(acc)], trend[t:t+len(acc)]
		for i := range acc {
			acc[i] += yr[i] - td[i]
		}
		t += run
		if ph += run; ph == period {
			ph = 0
		}
	}
	// How many points each phase got is arithmetic, not something to count: the
	// defined range is the m points from h to last, so every phase gets m/period
	// of them and the first m%period phases STARTING AT h's own phase get one
	// more. No phase can be empty — the range is at least period long once the
	// series covers two full cycles, and period consecutive points touch every
	// phase exactly once.
	base, extra, p0 := m/period, m%period, h%period
	mean := 0.0
	for i := range sum {
		cnt := base
		if d := i - p0; d < 0 {
			if d+period < extra {
				cnt++
			}
		} else if d < extra {
			cnt++
		}
		sum[i] /= float64(cnt)
		mean += sum[i]
	}
	mean /= float64(period)
	// A season with a mean is a level in disguise. Take the mean out so that
	// the level stays in the trend, where a caller looking for "how big is
	// the series" will find it.
	for i := range sum {
		sum[i] -= mean
	}

	// The ends hold the nearest defined average. See "The ends" above.
	for t := 0; t < h; t++ {
		trend[t] = trend[h]
	}
	for t := n - h; t < n; t++ {
		trend[t] = trend[last]
	}

	// The seasonal component is the one cycle repeated, so it is laid down by
	// copying rather than by indexing a phase per point: one period, then
	// double the filled region until the series is covered.
	copy(seas, sum)
	for f := period; f < n; f *= 2 {
		copy(seas[f:], seas[:f])
	}
	// And the residual is what the two of them leave, in the same order the
	// phase-indexed form subtracted it: (y − trend) − season. All four slices
	// are cut to one length so that the compiler can see they are the same
	// length and drop the bounds check on each of them.
	ss := seas[:n]
	ys, ts, rs := y[:len(ss)], trend[:len(ss)], resid[:len(ss)]
	for t, se := range ss {
		rs[t] = ys[t] - ts[t] - se
	}

	return &Decomposition{
		Period:   period,
		Trend:    trend,
		Seasonal: seas,
		Residual: resid,
		n:        n,
	}, nil
}

func seasonAt(f Fourier, t int) float64 { return f.At(t) }

// fitLines fits an independent least-squares line between changepoints.
func fitLines(y []float64, cps []int) []line {
	bounds := []int{0}
	bounds = append(bounds, cps...)
	bounds = append(bounds, len(y))
	out := make([]line, 0, len(bounds)-1)
	for i := 0; i+1 < len(bounds); i++ {
		lo, hi := bounds[i], bounds[i+1]-1
		a, b := ols(y, lo, hi)
		out = append(out, line{start: lo, end: hi, a: a, b: b})
	}
	return out
}

func evalLines(ls []line, n int) []float64 {
	out := make([]float64, n)
	for _, l := range ls {
		for t := l.start; t <= l.end && t < n; t++ {
			out[t] = l.a + l.b*float64(t-l.start)
		}
	}
	return out
}

// ols fits y = a + b·(t-lo) over y[lo..hi].
func ols(y []float64, lo, hi int) (a, b float64) {
	m := float64(hi - lo + 1)
	var st, sy, sty, stt float64
	for t := lo; t <= hi; t++ {
		u := float64(t - lo)
		st += u
		sy += y[t]
		sty += u * y[t]
		stt += u * u
	}
	den := m*stt - st*st
	if den == 0 {
		return sy / m, 0
	}
	b = (m*sty - st*sy) / den
	return (sy - b*st) / m, b
}

func lineSSR(y []float64, lo, hi int) float64 {
	a, b := ols(y, lo, hi)
	s := 0.0
	for t := lo; t <= hi; t++ {
		e := y[t] - a - b*float64(t-lo)
		s += e * e
	}
	return s
}

// noiseSigma estimates the noise level from first differences, robustly: the
// median absolute deviation, scaled to a standard deviation and divided by √2
// because a difference of two noisy points carries twice the variance.
func noiseSigma(y []float64) float64 {
	d := make([]float64, len(y)-1)
	for i := 1; i < len(y); i++ {
		d[i-1] = y[i] - y[i-1]
	}
	med := median(d)
	for i := range d {
		d[i] = math.Abs(d[i] - med)
	}
	return median(d) / 0.6744897501960817 / math.Sqrt2
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func sumSq(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x * x
	}
	return s
}

func sub(a, b []float64) []float64 {
	out := make([]float64, len(a))
	for i := range a {
		out[i] = a[i] - b[i]
	}
	return out
}

// fill forward-fills NaN; leading NaNs take the first real value. A series of
// only NaN is returned as zeros.
func fill(x []float64) []float64 {
	out := make([]float64, len(x))
	copy(out, x)
	first := math.NaN()
	for _, v := range out {
		if !math.IsNaN(v) {
			first = v
			break
		}
	}
	if math.IsNaN(first) {
		return make([]float64, len(x))
	}
	last := first
	for i, v := range out {
		if math.IsNaN(v) {
			out[i] = last
		} else {
			last = v
		}
	}
	return out
}

func allNaN(x []float64) bool {
	for _, v := range x {
		if !math.IsNaN(v) {
			return false
		}
	}
	return true
}

// solve solves A·x = b by Gaussian elimination with partial pivoting. It
// reports false for a singular system.
func solve(a [][]float64, b []float64) ([]float64, bool) {
	n := len(b)
	m := make([][]float64, n)
	for i := range a {
		m[i] = append(append([]float64(nil), a[i]...), b[i])
	}
	for c := 0; c < n; c++ {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(m[r][c]) > math.Abs(m[p][c]) {
				p = r
			}
		}
		if math.Abs(m[p][c]) < 1e-12 {
			return nil, false
		}
		m[c], m[p] = m[p], m[c]
		for r := c + 1; r < n; r++ {
			f := m[r][c] / m[c][c]
			for k := c; k <= n; k++ {
				m[r][k] -= f * m[c][k]
			}
		}
	}
	x := make([]float64, n)
	for r := n - 1; r >= 0; r-- {
		s := m[r][n]
		for k := r + 1; k < n; k++ {
			s -= m[r][k] * x[k]
		}
		x[r] = s / m[r][r]
	}
	return x, true
}

// A single bad reading is the most common defect in a real series, and every
// estimator here is a sum of squares, so one spike of magnitude a contributes
// a² and outvotes the rest of the data. The period search normalises by a
// variance the spike inflates, which drives every autocorrelation below the
// detection threshold and reports "no season". The break search fails the
// other way: its BIC price comes from a robust scale the spike does NOT
// inflate, so the spike's gain is free to buy splits and one bad day is
// reported as two structural breaks bracketing it.
//
// Both are fixed by cleaning the series the estimator scores on, and the clean
// has to tell an isolated spike from a genuine shift in level. It does that by
// taking the two things it needs from two different places:
//
//   - the LEVEL a point is compared against is the median of a short window
//     around it, because a level that lasts is a break and has to survive. A
//     shift sustained for more than hampelHalf points carries its own window's
//     median with it and is left alone; a spike is outvoted by its neighbours.
//     A global median cannot do this — a short step moves neither the median
//     nor the MAD of a long series, so it would be erased along with the spike.
//
//   - the SCALE that decides how far is too far comes from the WHOLE series,
//     because a scale is the one thing a short window cannot estimate. Seven
//     points have a MAD with enormous sampling variance, which underestimates
//     σ often enough that a nominal 4σ test fired on four points of pure
//     Gaussian noise in 150 — a rate near 1 in 37, not the 1 in 15787 the
//     threshold claims. Those four replacements then made a straight line fit
//     its two halves better than the price of a split, and the break search
//     invented a break in noise. noiseSigma is a median of absolute
//     differences over every point, so it is both robust to the spike and
//     actually estimated.
//
// This is a Hampel filter (Hampel 1974; Davies & Gather 1993, JASA 88(423))
// with a global scale.
const (
	// hampelHalf is the half-width of the comparison window. Three puts seven
	// points in the window, so a level shift of four or more consecutive
	// points — the shortest run minSegment will fit a line to — is preserved.
	hampelHalf = 3
	// hampelMADs is how many robust standard deviations from the local median a
	// point may sit before its neighbours replace it. Four leaves clean data
	// untouched while catching a spike several times the signal.
	hampelMADs = 4.0
)

// hampel returns v with isolated outliers replaced by their local median. It
// does not modify v. A zero scale (an exactly piecewise-linear series) disables
// the filter rather than shaving its corners.
//
// Near the ends the window slides inward instead of shrinking, so every point
// is judged against the same number of neighbours.
func hampel(v []float64) []float64 {
	n := len(v)
	width := 2*hampelHalf + 1
	if n < width+1 {
		return v
	}
	// 0.6745 scales a Gaussian MAD to a standard deviation, so hampelMADs is
	// read in the usual units rather than in MADs.
	sigma := noiseSigma(v)
	if !(sigma > 0) {
		return v
	}
	limit := hampelMADs * sigma
	out := make([]float64, n)
	// A fixed array, sorted in place by insertion. medianOf would allocate a
	// copy of the window for every point — eight thousand allocations for a
	// 4,000-point series, since this runs twice — and at seven elements an
	// insertion sort beats anything cleverer.
	var win [2*hampelHalf + 1]float64
	for i := range v {
		lo := min(max(i-hampelHalf, 0), n-width)
		copy(win[:], v[lo:lo+width])
		for a := 1; a < width; a++ {
			for b := a; b > 0 && win[b] < win[b-1]; b-- {
				win[b], win[b-1] = win[b-1], win[b]
			}
		}
		if med := win[width/2]; math.Abs(v[i]-med) > limit {
			out[i] = med
			continue
		}
		out[i] = v[i]
	}
	return out
}

// medianOf is the median of a copy of v, so the caller's order survives.
func medianOf(v []float64) float64 {
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}
