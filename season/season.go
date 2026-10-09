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
// survives the filter.
//
// Limits, stated so they are not discovered later:
//   - One seasonality. A series with both a weekly and a yearly cycle gets the
//     strongest one only.
//   - The trend is least-squares lines between changepoints, fitted
//     independently: it may jump at a changepoint (a level shift is a break),
//     and a forecast extends the last line.
//   - Short series are refused rather than guessed at: Period needs
//     MinPeriodLength (28) points and two full cycles, Decompose needs 4
//     points. 28 is not arbitrary — it is the shortest series in which a cycle
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
// Decompose is SLOWER than statsmodels.tsa.seasonal.seasonal_decompose at the
// decomposition itself, and that is the honest headline. Given the same period
// and with the trend-break search off — the operation seasonal_decompose
// performs — n=4,000 takes 1.74 ms here against 0.182 ms there, about 9.5x
// slower: it fits a line and a Fourier series by least squares where
// seasonal_decompose takes a centred moving average in C loops. If you already
// know your period and want the classical decomposition, use statsmodels.
//
// What this package offers is the work seasonal_decompose does not do at all,
// and the cost of each piece is reported rather than folded into the comparison
// above:
//
//	period given, no break search      1.74 ms
//	plus the BIC changepoint search    18.5 ms   (the default)
//	plus detecting the period too      55.4 ms
//	Period detection on its own        4.32 ms
//
// So: a moving average cannot tell you the period, cannot tell you where the
// trend broke, and cannot extrapolate. Those three are what the extra time buys.
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
