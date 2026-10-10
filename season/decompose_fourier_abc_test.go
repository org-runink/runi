package season

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"
	"testing/quick"
)

// This file holds the reference implementations of the two things the
// decomposition's speed-up actually changed, and the tests that hold the new
// code to them.
//
// Only ONE of the changes moves a number at all:
//
//   - medianIn replaced a sort with a selection. A selection returns the same
//     ELEMENT a sort would have left in the middle, so this is bit-identical,
//     and medianSortedABC below proves it rather than assuming it.
//   - hampel's insertion carries its element and shifts instead of swapping.
//     Same comparisons, same permutation, bit-identical, covered by the same
//     property tests that cover everything else here.
//   - Fourier.At now reduces t modulo the period before forming the angle.
//     This is the one that moves: mathematically it is the same value, but
//     cos(2πkt/P) computed at t=4,000 and at t=4,000 mod 24 are not the same
//     float64, because the angle itself is only accurate to a few ulps and by
//     t=4,000 the angle is past a thousand radians. atRefABC below is the old
//     formula, and the tests measure the gap rather than waving at it.

// atRefABC is Fourier.At as it was: the angle formed from t itself, however
// far down the series t is.
func atRefABC(f Fourier, t int) float64 {
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

// decomposeRefABC is Decompose as it was, evaluating the season through
// atRefABC at every point instead of through one tabulated cycle. Everything
// else — the period search, the Fourier fit, the outlier filter, the break
// search, the line fits — is the package's own, unchanged, so what this
// isolates is exactly the change under test.
func decomposeRefABC(x []float64, opt Options) (*Decomposition, error) {
	if len(x) < 4 {
		return nil, ErrTooShort
	}
	if allNaN(x) {
		return nil, errNoValuesXYZ
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

	var four Fourier
	if period > 0 {
		one := fitLines(y, nil)
		f, _ := FitFourier(hampel(sub(y, evalLines(one, n))), period, opt.Harmonics)
		four = f
	}
	deseason := make([]float64, n)
	for t := range y {
		deseason[t] = y[t] - atRefABC(four, t)
	}
	if opt.MaxChangepoints > 0 {
		d.Changepoints = Changepoints(deseason, opt.MaxChangepoints)
	}
	d.lines = fitLines(deseason, d.Changepoints)
	trend := evalLines(d.lines, n)
	if period > 0 {
		if f, err := FitFourier(hampel(sub(y, trend)), period, opt.Harmonics); err == nil {
			four = f
		}
	}
	d.Period, d.season = period, four
	for t := range y {
		d.Trend[t] = trend[t] + four.Mean
		d.Seasonal[t] = atRefABC(four, t)
		d.Residual[t] = y[t] - d.Trend[t] - d.Seasonal[t]
	}
	return d, nil
}

// hampelRefABC is the outlier filter without the shortcut: every window is
// copied and ordered, and the median read off, whatever the point looks like.
// It is the standard the counting fast path has to meet — and it has to meet
// it exactly, because the fast path is supposed to be a PROOF that the median
// lies inside the band, not a guess that it probably does.
func hampelRefABC(v []float64) []float64 {
	n := len(v)
	width := 2*hampelHalf + 1
	if n < width+1 {
		return v
	}
	sigma := noiseSigma(v)
	if !(sigma > 0) {
		return v
	}
	limit := hampelMADs * sigma
	out := make([]float64, n)
	var win [2*hampelHalf + 1]float64
	for i := range v {
		lo := min(max(i-hampelHalf, 0), n-width)
		copy(win[:], v[lo:lo+width])
		for a := 1; a < width; a++ {
			x, b := win[a], a
			for ; b > 0 && x < win[b-1]; b-- {
				win[b] = win[b-1]
			}
			win[b] = x
		}
		if med := win[width/2]; math.Abs(v[i]-med) > limit {
			out[i] = med
			continue
		}
		out[i] = v[i]
	}
	return out
}

// The fast path must change nothing. Over clean series, series with one spike,
// series with many, and series carrying NaN and Inf — which is where the
// counting argument stops holding and the window has to be ordered after all —
// the filter must return the same bits it returned when it ordered every
// window.
func TestHampelFastPathMatchesSortABC(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	check := func(name string, v []float64) {
		t.Helper()
		got := hampel(append([]float64(nil), v...))
		want := hampelRefABC(append([]float64(nil), v...))
		if len(got) != len(want) {
			t.Fatalf("%s: length %d, want %d", name, len(got), len(want))
		}
		for i := range got {
			if math.Float64bits(got[i]) != math.Float64bits(want[i]) {
				t.Errorf("%s (n=%d): [%d] = %v, ordering every window gives %v",
					name, len(v), i, got[i], want[i])
				return
			}
		}
	}
	for _, n := range []int{8, 9, 15, 40, 91, 200} {
		r := rand.New(rand.NewPCG(uint64(n), 61))
		base := make([]float64, n)
		for i := range base {
			base[i] = 100 + 0.5*float64(i) + 10*math.Sin(2*math.Pi*float64(i)/7) + r.NormFloat64()
		}
		with := func(name string, f func(x []float64)) {
			x := append([]float64(nil), base...)
			f(x)
			check(name, x)
		}
		with("clean", func(x []float64) {})
		with("one spike", func(x []float64) { x[n/2] += 500 })
		with("spike down", func(x []float64) { x[n/3] -= 500 })
		with("spike at 0", func(x []float64) { x[0] += 500 })
		with("spike at end", func(x []float64) { x[n-1] += 500 })
		with("many spikes", func(x []float64) {
			for i := range x {
				if i%5 == 0 {
					x[i] += 300
				}
			}
		})
		with("every point wild", func(x []float64) {
			for i := range x {
				x[i] = float64(1-2*(i%2)) * 1e6
			}
		})
		// NaN and Inf: the counting argument does not hold, so these must take
		// the long way round and still agree.
		with("one NaN", func(x []float64) { x[n/2] = nan })
		with("NaN at 0", func(x []float64) { x[0] = nan })
		with("NaN at end", func(x []float64) { x[n-1] = nan })
		with("two NaN", func(x []float64) { x[n/3], x[2*n/3] = nan, nan })
		with("NaN run", func(x []float64) {
			for i := n / 4; i < n/4+4 && i < n; i++ {
				x[i] = nan
			}
		})
		with("+Inf", func(x []float64) { x[n/2] = inf })
		with("-Inf", func(x []float64) { x[n/2] = math.Inf(-1) })
		with("Inf and NaN", func(x []float64) { x[n/3], x[2*n/3] = inf, nan })
		with("all NaN", func(x []float64) {
			for i := range x {
				x[i] = nan
			}
		})
		with("constant", func(x []float64) {
			for i := range x {
				x[i] = 5
			}
		})
	}
	// And at random, including NaN at a density that puts one in most windows.
	f := func(seed uint64, raw uint8) bool {
		n := 8 + int(raw)%120
		r := rand.New(rand.NewPCG(seed|1, 71))
		v := make([]float64, n)
		for i := range v {
			switch r.IntN(12) {
			case 0:
				v[i] = nan
			case 1:
				v[i] = math.Inf(1 - 2*r.IntN(2))
			case 2:
				v[i] = r.NormFloat64() * 1e4 // an outlier the filter should catch
			default:
				v[i] = 100 + r.NormFloat64()
			}
		}
		got, want := hampel(append([]float64(nil), v...)), hampelRefABC(append([]float64(nil), v...))
		for i := range got {
			if math.Float64bits(got[i]) != math.Float64bits(want[i]) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Error(err)
	}
}

// medianSortedABC is the median as it was computed: sort a copy, take the
// middle. It is the standard medianIn has to meet exactly.
func medianSortedABC(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// A selection is not an approximation of a sort. Over random lengths and
// contents, and over the shapes that break a careless quickselect — already
// sorted, reversed, all one value, two values, and NaN at every density —
// medianIn must return the very bits medianSortedABC returns.
func TestMedianSelectionMatchesSortABC(t *testing.T) {
	// Bit equality, with one documented exception: a slice holding both −0 and
	// +0 has no defined order between them under either a sort or a selection,
	// since they compare equal, so which of the two lands in the middle is
	// arbitrary in both. They are the same NUMBER, and every use here goes on
	// to subtract the median from something, where −0 and +0 behave
	// identically, so the distinction cannot reach an answer.
	same := func(name string, v []float64) {
		t.Helper()
		want := medianSortedABC(v)
		got := medianIn(append([]float64(nil), v...))
		if math.Float64bits(got) == math.Float64bits(want) {
			return
		}
		if got == 0 && want == 0 {
			return // −0 against +0
		}
		t.Errorf("%s (n=%d): medianIn = %v (%#x), sorted = %v (%#x)",
			name, len(v), got, math.Float64bits(got), want, math.Float64bits(want))
	}
	nan := math.NaN()
	for n := 1; n <= 70; n++ {
		r := rand.New(rand.NewPCG(uint64(n), 99))
		mk := func(f func(i int) float64) []float64 {
			v := make([]float64, n)
			for i := range v {
				v[i] = f(i)
			}
			return v
		}
		same("random", mk(func(int) float64 { return r.NormFloat64() }))
		same("sorted", mk(func(i int) float64 { return float64(i) }))
		same("reversed", mk(func(i int) float64 { return float64(n - i) }))
		same("constant", mk(func(int) float64 { return 7 }))
		same("two values", mk(func(i int) float64 { return float64(i % 2) }))
		same("organ pipe", mk(func(i int) float64 { return float64(min(i, n-1-i)) }))
		same("huge", mk(func(i int) float64 { return 1e300 * float64(i%5-2) }))
		same("tiny", mk(func(i int) float64 { return 1e-300 * float64(i%5-2) }))
		same("signed zeros", mk(func(i int) float64 {
			if i%2 == 0 {
				return math.Copysign(0, -1)
			}
			return 0
		}))
		same("infinities", mk(func(i int) float64 {
			switch i % 3 {
			case 0:
				return math.Inf(1)
			case 1:
				return math.Inf(-1)
			}
			return float64(i)
		}))
		// NaN at every density, which is where sort.Float64s' ordering (NaN
		// below every number) has to be reproduced exactly, including when
		// more than half the slice is NaN and the median IS one.
		for _, every := range []int{1, 2, 3, 5} {
			same("some NaN", mk(func(i int) float64 {
				if i%every == 0 {
					return nan
				}
				return r.NormFloat64()
			}))
		}
		// All NaN but a few, so that the two middles straddle the boundary.
		for _, reals := range []int{0, 1, 2, 3} {
			same("mostly NaN", mk(func(i int) float64 {
				if i < reals {
					return float64(i) - 1
				}
				return nan
			}))
		}
	}
	// And at random, to catch anything the shapes above do not.
	f := func(seed uint64, raw uint8) bool {
		n := 1 + int(raw)%200
		r := rand.New(rand.NewPCG(seed|1, 5))
		v := make([]float64, n)
		for i := range v {
			switch r.IntN(8) {
			case 0:
				v[i] = nan
			case 1:
				v[i] = math.Inf(1 - 2*r.IntN(2))
			default:
				v[i] = math.Round(r.NormFloat64() * 3) // ties, on purpose
			}
		}
		g, w := medianIn(append([]float64(nil), v...)), medianSortedABC(v)
		return math.Float64bits(g) == math.Float64bits(w) || (g == 0 && w == 0)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 3000}); err != nil {
		t.Error(err)
	}
}

// selectKth must leave the array partitioned around k, which is what lets
// medianIn take the upper middle as the smallest of the right-hand part.
func TestSelectKthPartitionsABC(t *testing.T) {
	for n := 1; n <= 60; n++ {
		for _, k := range []int{0, n / 3, n / 2, n - 1} {
			r := rand.New(rand.NewPCG(uint64(n*100+k), 4))
			v := make([]float64, n)
			for i := range v {
				v[i] = math.Round(r.NormFloat64() * 2)
			}
			want := append([]float64(nil), v...)
			sort.Float64s(want)
			selectKth(v, k)
			if v[k] != want[k] {
				t.Fatalf("n=%d k=%d: selectKth left %v, want %v", n, k, v[k], want[k])
			}
			for i := 0; i < k; i++ {
				if v[i] > v[k] {
					t.Fatalf("n=%d k=%d: v[%d]=%v sits left of the pivot %v", n, k, i, v[i], v[k])
				}
			}
			for i := k + 1; i < n; i++ {
				if v[i] < v[k] {
					t.Fatalf("n=%d k=%d: v[%d]=%v sits right of the pivot %v", n, k, i, v[i], v[k])
				}
			}
		}
	}
}

// At is a function of the phase, so reducing t modulo the period is not an
// approximation — it is the definition. It must now hold EXACTLY, where
// before it held only to about 1e-13.
func TestFourierAtIsExactlyPeriodicABC(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 8))
	for _, period := range []int{2, 3, 7, 12, 24, 25, 48, 365} {
		f := Fourier{Period: period, Mean: 5}
		for k := 0; k < 3 && 2*(k+1) <= period; k++ {
			f.A = append(f.A, r.NormFloat64()*10)
			f.B = append(f.B, r.NormFloat64()*10)
		}
		if len(f.A) == 0 {
			continue
		}
		for _, t0 := range []int{0, 1, 5, period - 1} {
			base := f.At(t0)
			for _, cycles := range []int{1, 2, 17, 1000, 1 << 20} {
				if got := f.At(t0 + cycles*period); got != base {
					t.Errorf("period=%d t=%d +%d cycles: At = %v, want exactly %v",
						period, t0, cycles, got, base)
				}
			}
			// And backwards, where Go's % would otherwise hand back a
			// negative phase.
			for _, cycles := range []int{1, 3, 500} {
				if got := f.At(t0 - cycles*period); got != base {
					t.Errorf("period=%d t=%d −%d cycles: At = %v, want exactly %v",
						period, t0, cycles, got, base)
				}
			}
		}
	}
}

// How far the reduction actually moves At, measured rather than assumed, so
// that the tolerance the decomposition tests assert is a number with a reason
// behind it and not a number that happened to pass.
func TestFourierAtReductionGapABC(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 13))
	f := Fourier{Period: 24}
	for k := 0; k < 3; k++ {
		f.A = append(f.A, r.NormFloat64()*10)
		f.B = append(f.B, r.NormFloat64()*10)
	}
	worst, amp := 0.0, 0.0
	for k := range f.A {
		amp += math.Abs(f.A[k]) + math.Abs(f.B[k])
	}
	for t0 := 0; t0 < 4000; t0++ {
		if g := math.Abs(f.At(t0) - atRefABC(f, t0)); g > worst {
			worst = g
		}
	}
	t.Logf("over n=4,000 at period 24 with |A|+|B| = %.3g, reducing t first moves At by at most %.3g", amp, worst)
	// The old formula's angle at t=4,000 is past 3,000 radians, where an ulp
	// is about 4.5e-13; a coefficient of order 10 carries that to about
	// 1e-11. Anything far above that would mean the reduction is not what it
	// claims to be.
	if worst > 1e-9 {
		t.Errorf("reduction moves At by %g, which is far more than the angle's own rounding explains", worst)
	}
}

// ampABC is the total size of a fit's coefficients, which is what the
// evaluation's rounding error scales with.
func ampABC(f Fourier) float64 {
	a := 0.0
	for k := range f.A {
		a += math.Abs(f.A[k]) + math.Abs(f.B[k])
	}
	return a
}

// decomposeCloseABC compares the two decompositions component by component and
// returns the worst absolute gap, failing on anything structural.
//
// It reports separately when the two FITS diverge rather than the two
// evaluations. Some designs this package accepts are ill conditioned — an
// alternating ±1 series fitted at period 7, a flat series with one spike in it
// — and for those a perturbation the size of a rounding error in the first
// pass moves the second pass's coefficients by order one. That is the
// conditioning of the least-squares problem, not a property of either
// implementation: the old code's answer on such a series was no more
// determined than the new one's, and a tolerance there would be measuring the
// wrong thing. What those cases are held to instead is the set of invariants
// that must hold whatever the fit comes out as, in decomposeInvariantsABC.
func decomposeCloseABC(t *testing.T, name string, x []float64, opt Options, tol float64) float64 {
	t.Helper()
	got, gotErr := Decompose(x, opt)
	want, wantErr := decomposeRefABC(x, opt)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("%s: Decompose err=%v, reference err=%v", name, gotErr, wantErr)
	}
	if gotErr != nil {
		if gotErr.Error() != wantErr.Error() {
			t.Errorf("%s: Decompose err=%q, reference err=%q", name, gotErr, wantErr)
		}
		return 0
	}
	if got.Period != want.Period {
		t.Errorf("%s (n=%d): period %d, reference %d", name, len(x), got.Period, want.Period)
	}
	// A break is a discrete decision taken on the deseasonalised series, so a
	// change far below the noise can still flip one when the series has no
	// noise for the price of a split to be judged against. When that happens
	// the trend is a different trend and comparing it point by point says
	// nothing; the invariants still have to hold, and they are checked.
	breaksDiffer := len(got.Changepoints) != len(want.Changepoints)
	for i := 0; !breaksDiffer && i < len(got.Changepoints); i++ {
		breaksDiffer = got.Changepoints[i] != want.Changepoints[i]
	}
	// Ill conditioned means the two fits are not the same fit.
	ga, wa := ampABC(got.season), ampABC(want.season)
	illCond := math.Abs(ga-wa) > 1e-6*(1+math.Max(ga, wa))
	if breaksDiffer || illCond {
		decomposeInvariantsABC(t, name, x, got, true)
		// The reference is held to the same invariants but one: it cannot be
		// exactly periodic, because evaluating the season from t rather than
		// from the phase is precisely what it does and what this replaced.
		decomposeInvariantsABC(t, name+" (reference)", x, want, false)
		return 0
	}
	scale := 1.0
	for _, v := range x {
		if a := math.Abs(v); a > scale && !math.IsInf(a, 0) {
			scale = a
		}
	}
	// The gap scales with the coefficients, not only with the series: the one
	// thing that moved is the angle At forms, and its error reaches the output
	// multiplied by |A|+|B|.
	bound := tol * math.Max(scale, ga)
	worst := 0.0
	for _, c := range []struct {
		what     string
		got, ref []float64
	}{
		{"trend", got.Trend, want.Trend},
		{"seasonal", got.Seasonal, want.Seasonal},
		{"residual", got.Residual, want.Residual},
	} {
		for i := range c.got {
			g, w := c.got[i], c.ref[i]
			if math.IsNaN(g) || math.IsNaN(w) {
				if math.IsNaN(g) != math.IsNaN(w) {
					t.Errorf("%s (n=%d): %s[%d] = %v, reference %v", name, len(x), c.what, i, g, w)
				}
				continue
			}
			if math.IsInf(g, 0) || math.IsInf(w, 0) {
				if g != w {
					t.Errorf("%s (n=%d): %s[%d] = %v, reference %v", name, len(x), c.what, i, g, w)
				}
				continue
			}
			if e := math.Abs(g - w); e > worst {
				worst = e
			}
			if math.Abs(g-w) > bound {
				t.Errorf("%s (n=%d period=%d): %s[%d] = %v, reference %v, gap %g over %g",
					name, len(x), got.Period, c.what, i, g, w, math.Abs(g-w), bound)
			}
		}
	}
	return worst
}

// decomposeInvariantsABC holds a decomposition to what must be true of it
// whatever the fit came out as: the three components add back up to the
// series, the season really is periodic, and nothing has become non-finite
// that was not already.
func decomposeInvariantsABC(t *testing.T, name string, x []float64, d *Decomposition, exactSeason bool) {
	t.Helper()
	scale := 1.0
	finite := true
	for _, v := range x {
		if a := math.Abs(v); a > scale && !math.IsInf(a, 0) {
			scale = a
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			finite = false
		}
	}
	for i := range x {
		s := d.Trend[i] + d.Seasonal[i] + d.Residual[i]
		if finite && math.Abs(s-x[i]) > 1e-9*scale {
			t.Errorf("%s: components at %d sum to %v, want %v", name, i, s, x[i])
			return
		}
		if finite && (math.IsNaN(d.Seasonal[i]) || math.IsInf(d.Seasonal[i], 0)) {
			t.Errorf("%s: seasonal[%d] = %v from a finite series", name, i, d.Seasonal[i])
			return
		}
	}
	if exactSeason && d.Period >= 2 {
		for i := 0; i+d.Period < len(x); i++ {
			if d.Seasonal[i] != d.Seasonal[i+d.Period] {
				t.Errorf("%s: seasonal[%d] = %v but seasonal[%d] = %v — a season that is not periodic",
					name, i, d.Seasonal[i], i+d.Period, d.Seasonal[i+d.Period])
				return
			}
		}
	}
}

// The decomposition over random series. The tolerance is relative to the
// series' own magnitude: the only thing that moved is the angle At forms, and
// that moves the season by about an ulp of a thousand radians times the
// harmonic amplitudes — order 1e-11 on a series of order 300. 1e-12 relative
// to the scale leaves two orders of headroom over that and is still eight
// orders tighter than any difference a real defect would make.
func TestDecomposeFourierAgreesABC(t *testing.T) {
	worst := 0.0
	f := func(seed uint64, rawN uint16, rawP uint8, rawH uint8) bool {
		period := 2 + int(rawP)%30
		n := 2*period + int(rawN)%400
		harmonics := 1 + int(rawH)%4
		r := rand.New(rand.NewPCG(seed|1, 0x5eed))
		x := make([]float64, n)
		for i := range x {
			x[i] = 100 + 0.05*float64(i) +
				10*math.Sin(2*math.Pi*float64(i)/float64(period)) + r.NormFloat64()
		}
		opt := Options{Period: period, Harmonics: harmonics, MaxChangepoints: -1}
		if w := decomposeCloseABC(t, "random", x, opt, 1e-12); w > worst {
			worst = w
		}
		return !t.Failed()
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 300}); err != nil {
		t.Error(err)
	}
	t.Logf("worst component gap against the old evaluation: %.3g", worst)
}

// The break search ON, which is the default path and shares all of this
// machinery. A changepoint is a discrete choice, so this is where a 1e-11
// change would show up as something other than a 1e-11 change.
func TestDecomposeWithBreaksAgreesABC(t *testing.T) {
	worst := 0.0
	for _, seed := range []uint64{1, 2, 3, 4, 5, 6, 7, 8} {
		for _, period := range []int{7, 12, 24} {
			n := 300
			r := rand.New(rand.NewPCG(seed, uint64(period)))
			x := make([]float64, n)
			for i := range x {
				x[i] = 100 + 0.05*float64(i) +
					10*math.Sin(2*math.Pi*float64(i)/float64(period)) + r.NormFloat64()
				if i > n/2 {
					x[i] += 30 // a break for the search to find
				}
			}
			if w := decomposeCloseABC(t, "breaks", x, Options{Period: period}, 1e-12); w > worst {
				worst = w
			}
			// And the fully automatic path, which also detects the period.
			if w := decomposeCloseABC(t, "auto", x, Options{}, 1e-12); w > worst {
				worst = w
			}
		}
	}
	t.Logf("worst component gap with the break search on: %.3g", worst)
}

// The shapes the brief calls out, each one a place where the tabulated cycle
// or the selection-based median could behave differently from what it
// replaced.
func TestDecomposeFourierShapesABC(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	shapes := map[string]func(i, n int, r *rand.Rand) float64{
		"constant":      func(i, n int, r *rand.Rand) float64 { return 42 },
		"pure trend":    func(i, n int, r *rand.Rand) float64 { return 10 + 0.3*float64(i) },
		"pure season":   func(i, n int, r *rand.Rand) float64 { return 10 * math.Sin(2*math.Pi*float64(i)/12) },
		"noise only":    func(i, n int, r *rand.Rand) float64 { return r.NormFloat64() },
		"steep trend":   func(i, n int, r *rand.Rand) float64 { return float64(i) * 1e9 },
		"huge":          func(i, n int, r *rand.Rand) float64 { return 1e300 * (1 + 0.1*math.Sin(float64(i))) },
		"tiny":          func(i, n int, r *rand.Rand) float64 { return 1e-300 * (1 + 0.1*math.Sin(float64(i))) },
		"zeros":         func(i, n int, r *rand.Rand) float64 { return 0 },
		"alternating":   func(i, n int, r *rand.Rand) float64 { return float64(1 - 2*(i%2)) },
		"single spike":  func(i, n int, r *rand.Rand) float64 { return map[bool]float64{true: 1e6, false: 1}[i == n/2] },
		"ill cond ramp": func(i, n int, r *rand.Rand) float64 { return 1e12 + 1e-6*float64(i) },
	}
	for name, mk := range shapes {
		for _, period := range []int{2, 3, 7, 12, 24} {
			// n an exact multiple of the period, and emphatically not one.
			for _, n := range []int{2 * period, 4 * period, 4*period + 1, 4*period + period/2, 97} {
				if n < 2*period || n < 4 {
					continue
				}
				r := rand.New(rand.NewPCG(uint64(n), uint64(period)))
				x := make([]float64, n)
				for i := range x {
					x[i] = mk(i, n, r)
				}
				decomposeCloseABC(t, name, x, Options{Period: period, MaxChangepoints: -1}, 1e-12)
				decomposeCloseABC(t, name+" breaks", x, Options{Period: period}, 1e-12)
			}
		}
	}
	// Non-finite values. The API takes an int period, so there is no
	// fractional period to cover; what there is instead is a series the
	// arithmetic cannot stay finite through, and both must not stay finite
	// differently.
	for _, n := range []int{24, 48, 49, 100} {
		for _, m := range []struct {
			name string
			v    float64
			at   []int
		}{
			{"leading NaN", nan, []int{0}},
			{"interior NaN", nan, []int{n / 2}},
			{"trailing NaN", nan, []int{n - 1}},
			{"many NaN", nan, []int{0, 1, 2, n / 3, n / 2, n - 2, n - 1}},
			{"+Inf", inf, []int{n / 2}},
			{"-Inf", math.Inf(-1), []int{n / 3}},
			{"both Inf", inf, []int{0, n - 1}},
		} {
			r := rand.New(rand.NewPCG(uint64(n), 21))
			x := make([]float64, n)
			for i := range x {
				x[i] = 100 + 0.05*float64(i) + 10*math.Sin(2*math.Pi*float64(i)/12) + r.NormFloat64()
			}
			for _, i := range m.at {
				x[i] = m.v
			}
			decomposeCloseABC(t, m.name, x, Options{Period: 12, MaxChangepoints: -1}, 1e-12)
		}
		// Every value NaN: both must refuse, with the same message.
		all := make([]float64, n)
		for i := range all {
			all[i] = nan
		}
		decomposeCloseABC(t, "all NaN", all, Options{Period: 12}, 1e-12)
	}
	// Series shorter than the period, shorter than two cycles, and shorter
	// than Decompose will look at at all. A period it cannot identify is
	// dropped rather than fitted, in both.
	for n := 0; n < 30; n++ {
		r := rand.New(rand.NewPCG(uint64(n), 77))
		x := make([]float64, n)
		for i := range x {
			x[i] = 10 + float64(i) + r.NormFloat64()
		}
		for _, period := range []int{1, 2, 3, 12, 24, 100} {
			decomposeCloseABC(t, "short", x, Options{Period: period, MaxChangepoints: -1}, 1e-12)
		}
	}
}

// Whatever else changes, the three components must still add back up to the
// series. This is the property a caller actually relies on, and it is checked
// at the ends as well as the interior.
func TestDecomposeReconstructsABC(t *testing.T) {
	for _, period := range []int{2, 7, 12, 24, 25} {
		for _, n := range []int{2 * period, 4*period + 1, 300, 4000} {
			if n < 2*period || n < 4 {
				continue
			}
			r := rand.New(rand.NewPCG(uint64(n), uint64(period)))
			x := make([]float64, n)
			for i := range x {
				x[i] = 100 + 0.05*float64(i) +
					10*math.Sin(2*math.Pi*float64(i)/float64(period)) + r.NormFloat64()
			}
			for _, opt := range []Options{
				{Period: period, MaxChangepoints: -1},
				{Period: period},
				{},
			} {
				d, err := Decompose(x, opt)
				if err != nil {
					t.Fatalf("n=%d period=%d: %v", n, period, err)
				}
				worst := 0.0
				for i := range x {
					if e := math.Abs(d.Trend[i] + d.Seasonal[i] + d.Residual[i] - x[i]); e > worst {
						worst = e
					}
				}
				// Residual is formed as x − trend − season, so the identity is
				// exact up to the rounding of that one subtraction at a
				// magnitude of a few hundred: a few ulps.
				if worst > 1e-10 {
					t.Errorf("n=%d period=%d opt=%+v: components miss the series by %g",
						n, period, opt, worst)
				}
			}
		}
	}
}

// The seasonal component a caller gets back is now EXACTLY periodic — the
// same bits one cycle later, at every index, on every series. That is what a
// season means, and it is the guarantee the reduction in At buys: before it,
// the component drifted from its own period down the series, by about 1e-13
// at n=4,000 on a well-conditioned fit and by far more on a badly conditioned
// one, because the angle handed to Cos grew with t and so did the rounding in
// forming it. Asserted here with ==, since anything else would let the drift
// back in.
func TestDecomposeSeasonIsExactlyPeriodicABC(t *testing.T) {
	drift := 0.0
	for _, period := range []int{2, 3, 7, 12, 24, 25} {
		for _, n := range []int{2 * period, 4*period + 1, 500, 4000} {
			if n < 2*period || n < 4 {
				continue
			}
			r := rand.New(rand.NewPCG(uint64(n), uint64(period)))
			x := make([]float64, n)
			for i := range x {
				x[i] = 100 + 0.05*float64(i) +
					10*math.Sin(2*math.Pi*float64(i)/float64(period)) + r.NormFloat64()
			}
			for _, opt := range []Options{{Period: period, MaxChangepoints: -1}, {Period: period}, {}} {
				d, err := Decompose(x, opt)
				if err != nil {
					t.Fatalf("n=%d period=%d: %v", n, period, err)
				}
				if d.Period < 2 {
					continue
				}
				for i := 0; i+d.Period < n; i++ {
					if d.Seasonal[i] != d.Seasonal[i+d.Period] {
						t.Fatalf("n=%d period=%d: seasonal[%d] = %v but seasonal[%d] = %v",
							n, period, i, d.Seasonal[i], i+d.Period, d.Seasonal[i+d.Period])
					}
					// How far the old evaluation would have drifted, for the
					// record.
					g := math.Abs(atRefABC(d.season, i) - atRefABC(d.season, i+d.Period))
					if g > drift {
						drift = g
					}
				}
			}
		}
	}
	t.Logf("the evaluation this replaces drifted from its own period by up to %.3g over the same series", drift)
}

// seasonCycle stands in for At over a whole series, so it must BE At: not
// close to it, the same bits, at every phase and for every shape of Fourier
// the fit can produce — including the ones with no season at all, where it
// must say so by returning nothing.
func TestSeasonCycleIsAtABC(t *testing.T) {
	r := rand.New(rand.NewPCG(31, 37))
	for _, period := range []int{2, 3, 7, 12, 24, 25, 48} {
		for harmonics := 1; harmonics <= 4; harmonics++ {
			f := Fourier{Period: period, Mean: r.NormFloat64()}
			for k := 0; k < harmonics && 2*(k+1) <= period; k++ {
				f.A = append(f.A, r.NormFloat64()*10)
				f.B = append(f.B, r.NormFloat64()*10)
			}
			c := seasonCycle(f)
			if len(f.A) == 0 {
				if c != nil {
					t.Errorf("period=%d: a Fourier with no harmonics produced a cycle", period)
				}
				continue
			}
			if len(c) != period {
				t.Fatalf("period=%d: cycle is %d long", period, len(c))
			}
			// Every phase, and then well past the end of one cycle, which is
			// where the tiling in Decompose reads from.
			for tt := 0; tt < 5*period+3; tt++ {
				if got, want := c[tt%period], f.At(tt); math.Float64bits(got) != math.Float64bits(want) {
					t.Errorf("period=%d t=%d: cycle has %v, At has %v", period, tt, got, want)
				}
			}
		}
	}
	// No season at all: Period below 2 has no cycle to lay out.
	for _, f := range []Fourier{{}, {Period: 1, A: []float64{1}, B: []float64{1}}} {
		if c := seasonCycle(f); c != nil {
			t.Errorf("Fourier%+v produced a cycle of %d", f, len(c))
		}
	}
}
