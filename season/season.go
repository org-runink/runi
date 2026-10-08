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
// Limits, stated so they are not discovered later:
//   - One seasonality. A series with both a weekly and a yearly cycle gets the
//     strongest one only.
//   - The trend is least-squares lines between changepoints, fitted
//     independently: it may jump at a changepoint (a level shift is a break),
//     and a forecast extends the last line.
//   - Short series are refused rather than guessed at: Period needs 8 points
//     and two full cycles, Decompose needs 4 points.
//   - No uncertainty intervals.
package season

import (
	"errors"
	"math"
	"sort"
)

// MinPeriodLength is the shortest series Period will look for a cycle in.
const MinPeriodLength = 8

// ErrTooShort is returned when a series has too few points to model.
var ErrTooShort = errors.New("season: series too short")

// Period returns the dominant seasonal period of x, or 0 when there is none.
//
// It looks for the lag with the strongest autocorrelation of the
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
	best, bestR := 0, periodThreshold(m, maxLag-1)
	for lag := 2; lag <= maxLag; lag++ {
		c := 0.0
		for i := 0; i+lag < m; i++ {
			c += (d[i] - mean) * (d[i+lag] - mean)
		}
		if r := c / var0; r > bestR {
			best, bestR = lag, r
		}
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
	best, bestSSR := lag, math.Inf(1)
	for p := lag - 1; p <= lag+1; p++ {
		if p < 2 || p > maxLag {
			continue
		}
		f, err := FitFourier(detrended, p, 3)
		if err != nil {
			continue
		}
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
	row := make([]float64, p)
	for t, y := range x {
		for j, c := range cols {
			row[j] = basis(c, t)
		}
		for i := 0; i < p; i++ {
			xty[i] += row[i] * y
			for j := 0; j < p; j++ {
				xtx[i][j] += row[i] * row[j]
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
	sigma := noiseSigma(x)
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
		f, err := FitFourier(sub(y, evalLines(one, n)), period, opt.Harmonics)
		if err == nil {
			four = f
		} else {
			period = 0
		}
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
		if f, err := FitFourier(sub(y, trend), period, opt.Harmonics); err == nil {
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
