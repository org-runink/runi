// SPDX-License-Identifier: BSD-3-Clause

package stats

import (
	"math"
	"math/big"
	mrand "math/rand"
	"reflect"
	"testing"
	"testing/quick"
)

// trendReferenceXYZ is the implementation Trend had before the closed-form
// rewrite: three passes, with meanX and sxx summed from the index rather than
// written down. It is kept here verbatim so every claim the new code makes can
// be held against the arithmetic it replaced.
func trendReferenceXYZ(y []float64) TrendTest {
	n := len(y)
	out := TrendTest{N: n, P: 1}
	if n < 2 {
		return out
	}
	var sx, sy float64
	for i, v := range y {
		sx += float64(i)
		sy += v
	}
	meanX, meanY := sx/float64(n), sy/float64(n)
	var sxx, sxy float64
	for i, v := range y {
		dx := float64(i) - meanX
		sxx += dx * dx
		sxy += dx * (v - meanY)
	}
	b := sxy / sxx
	out.Slope = b
	df := n - 2
	if df < 1 {
		return out
	}
	var sse float64
	for i, v := range y {
		resid := v - (meanY + b*(float64(i)-meanX))
		sse += resid * resid
	}
	if sse <= 0 {
		if b != 0 {
			out.T = math.Copysign(math.Inf(1), b)
			out.P = 0
		}
		return out
	}
	se := math.Sqrt((sse / float64(df)) / sxx)
	out.StdErr = se
	out.T = b / se
	out.P = TwoSidedP(out.T, df)
	return out
}

// closeEnoughXYZ compares one field. NaN matches only NaN and an infinity only
// the same infinity, because those are answers here, not failures: a NaN slope
// is the documented response to a missing value.
//
// abs is a floor, not a convenience. Slope and StdErr carry the units of y, and
// the old code leaves dust at the scale of eps·max|y| in both — a constant
// series, for instance, comes out of it with a standard error of about
// eps·max|y|/n rather than the zero it has. Holding the rewrite to a purely
// relative bound would be demanding that it reproduce that dust. The floor is
// set nine orders of magnitude below the natural scale of the quantity, far
// below anything a caller could act on and far above the noise.
func closeEnoughXYZ(got, want, rel, abs float64) bool {
	switch {
	case math.IsNaN(got) || math.IsNaN(want):
		return math.IsNaN(got) && math.IsNaN(want)
	case math.IsInf(got, 0) || math.IsInf(want, 0):
		return got == want
	}
	return math.Abs(got-want) <= rel*math.Abs(want)+abs
}

func maxAbsXYZ(y []float64) float64 {
	m := 0.0
	for _, v := range y {
		if a := math.Abs(v); a > m {
			m = a
		}
	}
	return m
}

// agreesWithReferenceXYZ reports whether Trend and the pre-rewrite algorithm
// give the same answer for y.
//
// The tolerances are split because the two quantities are not equally
// sensitive. Slope, StdErr and T are smooth in the sums, and for these inputs
// both algorithms are backward stable, so they agree to a few units in the
// last place times the conditioning of the fit; 1e-9 leaves four orders of
// headroom over the ~1e-13 actually observed. P is not smooth: with df degrees
// of freedom log P falls like -t²/2, so a relative wobble d in t comes out of
// the tail as roughly t²·d. At the t values these series reach that is still
// far inside 1e-6, which is why P is allowed the looser bound rather than a
// bound that would be quietly impossible to meet.
func agreesWithReferenceXYZ(y []float64, slopeTol, pTol float64) (field string, got, want TrendTest, ok bool) {
	want = trendReferenceXYZ(y)
	got = Trend(y)
	if got.N != want.N {
		return "n", got, want, false
	}
	floor := 1e-12 * maxAbsXYZ(y)
	for _, c := range []struct {
		name           string
		a, b, rel, flr float64
	}{
		{"slope", got.Slope, want.Slope, slopeTol, floor},
		{"stderr", got.StdErr, want.StdErr, slopeTol, floor},
	} {
		if !closeEnoughXYZ(c.a, c.b, c.rel, c.flr) {
			return c.name, got, want, false
		}
	}
	// T and P are the standard error divided into the slope and then pushed
	// through a tail that falls like exp(-t²/2). When the two standard errors
	// matched only because of the floor above, the residual variance they came
	// from is rounding dust in both implementations — an exact line whose
	// points are too large to be held exactly, say — and the ratio of two
	// pieces of dust is not a quantity either version can be held to. Where the
	// standard error is a real number, T and P are checked.
	if !closeEnoughXYZ(got.StdErr, want.StdErr, slopeTol, 0) {
		return "", got, want, true
	}
	for _, c := range []struct {
		name           string
		a, b, rel, flr float64
	}{
		{"t", got.T, want.T, slopeTol, 1e-9},
		{"p", got.P, want.P, pTol, 0},
	} {
		if !closeEnoughXYZ(c.a, c.b, c.rel, c.flr) {
			return c.name, got, want, false
		}
	}
	return "", got, want, true
}

// trendSeriesXYZ is a random series shaped like something a caller would
// actually hand Trend.
type trendSeriesXYZ []float64

// Generate draws a series of one of four shapes — constant, an exact line,
// a line under noise, or pure noise — at a scale drawn across forty orders of
// magnitude. The offset is held within 1e3 of the spread on purpose: beyond
// that the two algorithms are being asked to agree about digits neither of
// them has, and that regime belongs to the exact-arithmetic tests below, which
// can say which one is right rather than only whether they match.
func (trendSeriesXYZ) Generate(r *mrand.Rand, size int) reflect.Value {
	n := r.Intn(size + 3)
	y := make([]float64, n)
	spread := math.Pow(10, float64(r.Intn(41)-20)) // 1e-20 … 1e20
	offset := spread * r.Float64() * 1e3
	if r.Intn(2) == 0 {
		offset = -offset
	}
	slope := spread * r.NormFloat64()
	switch r.Intn(4) {
	case 0:
		for i := range y {
			y[i] = offset
		}
	case 1:
		for i := range y {
			y[i] = offset + slope*float64(i)
		}
	case 2:
		for i := range y {
			y[i] = offset + slope*float64(i) + spread*r.NormFloat64()
		}
	default:
		for i := range y {
			y[i] = offset + spread*r.NormFloat64()
		}
	}
	return reflect.ValueOf(trendSeriesXYZ(y))
}

func TestTrendClosedFormAgreesWithReferenceXYZ(t *testing.T) {
	var firstField string
	var firstGot, firstWant TrendTest
	var firstN int
	f := func(y trendSeriesXYZ) bool {
		field, got, want, ok := agreesWithReferenceXYZ(y, 1e-9, 1e-6)
		if !ok && firstField == "" {
			firstField, firstGot, firstWant, firstN = field, got, want, len(y)
		}
		return ok
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 20000}); err != nil {
		t.Errorf("n=%d: %s disagrees\n new %+v\n old %+v", firstN, firstField, firstGot, firstWant)
	}
}

func TestTrendClosedFormEdgeCasesXYZ(t *testing.T) {
	long := make([]float64, 100003) // not a multiple of four: exercises the tail
	r := mrand.New(mrand.NewSource(20261010))
	for i := range long {
		long[i] = 0.001*float64(i) + r.NormFloat64()
	}
	longFlat := make([]float64, 65536)
	for i := range longFlat {
		longFlat[i] = r.NormFloat64()
	}
	tiny := make([]float64, 1000)
	for i := range tiny {
		tiny[i] = 1e-300 * (1 + 0.5*r.NormFloat64())
	}
	huge := make([]float64, 1000)
	for i := range huge {
		huge[i] = 1e300 * (1 + 0.5*r.NormFloat64())
	}

	for _, tc := range []struct {
		name string
		y    []float64
	}{
		{"nil", nil},
		{"n=0", []float64{}},
		{"n=1", []float64{42}},
		{"n=2", []float64{1, 2}},
		{"n=3", []float64{1, 2, 4}},
		{"n=5 tail", []float64{3, 1, 4, 1, 5}},
		{"constant", []float64{3, 3, 3, 3, 3, 3}},
		{"constant huge", []float64{1e300, 1e300, 1e300, 1e300, 1e300}},
		{"constant tiny", []float64{1e-300, 1e-300, 1e-300, 1e-300, 1e-300}},
		{"constant zero", make([]float64, 64)},
		{"exact line up", []float64{1, 2, 3, 4, 5, 6}},
		{"exact line down", []float64{6, 5, 4, 3}},
		{"exact line offset", []float64{1e6 + 1, 1e6 + 2, 1e6 + 3, 1e6 + 4, 1e6 + 5}},
		{"exact line tiny", []float64{1e-300, 2e-300, 3e-300, 4e-300, 5e-300, 6e-300, 7e-300}},
		{"exact line huge", []float64{1e300, 2e300, 3e300, 4e300, 5e300}},
		{"huge magnitudes", huge},
		{"tiny magnitudes", tiny},
		{"nan first", []float64{math.NaN(), 2, 3, 4, 5}},
		{"nan middle", []float64{1, 2, math.NaN(), 4, 5, 6}},
		{"nan last", []float64{1, 2, 3, 4, math.NaN()}},
		{"all nan", []float64{math.NaN(), math.NaN(), math.NaN(), math.NaN()}},
		{"+inf", []float64{1, 2, math.Inf(1), 4, 5}},
		{"-inf", []float64{1, 2, math.Inf(-1), 4, 5}},
		{"inf first", []float64{math.Inf(1), 2, 3, 4, 5}},
		{"both infs", []float64{math.Inf(-1), 2, 3, math.Inf(1)}},
		{"overflowing products", []float64{0, 0, 1.7e308, 1.7e308}},
		{"long trending", long},
		{"long flat", longFlat},
	} {
		slopeTol, pTol := 1e-9, 1e-6
		if tc.name == "long trending" || tc.name == "long flat" {
			// 10^5 points: the reference's summed sxx has accumulated 10^5
			// roundings by here, where the closed form has one. A looser
			// bound on P follows from the looser bound on T through the same
			// t² amplification described above, at t ≈ 300.
			slopeTol, pTol = 1e-8, 1e-3
		}
		field, got, want, ok := agreesWithReferenceXYZ(tc.y, slopeTol, pTol)
		if !ok {
			t.Errorf("%s: %s disagrees\n new %+v\n old %+v", tc.name, field, got, want)
		}
	}
}

// Documented behaviour that the rewrite must not quietly change.
func TestTrendClosedFormKeepsContractXYZ(t *testing.T) {
	if got := Trend(nil); got.N != 0 || got.P != 1 || got.Slope != 0 {
		t.Errorf("empty: %+v", got)
	}
	if got := Trend([]float64{7}); got.N != 1 || got.P != 1 {
		t.Errorf("single point: %+v", got)
	}
	if got := Trend([]float64{1, 2}); got.P != 1 || got.StdErr != 0 || got.Slope != 1 {
		t.Errorf("two points: %+v", got)
	}
	if got := Trend([]float64{3, 3, 3, 3, 3, 3}); got.Slope != 0 || got.P != 1 || got.StdErr != 0 {
		t.Errorf("constant: %+v", got)
	}
	// An exact line must still land on exactly zero residual variance, which
	// is the branch that reports P = 0 and an infinite t. A sum of squares
	// computed as Syy - b²·sxx would have left rounding dust here and turned
	// certainty into a very small p-value, which is why the second pass over
	// the residuals was kept.
	for _, y := range [][]float64{
		{1, 2, 3, 4, 5, 6},
		{6, 5, 4, 3},
		{0, 2, 4, 6, 8, 10, 12, 14, 16},
		{1e6 + 1, 1e6 + 2, 1e6 + 3, 1e6 + 4},
	} {
		got := Trend(y)
		if got.StdErr != 0 || got.P != 0 || !math.IsInf(got.T, 0) {
			t.Errorf("exact line %v: %+v", y, got)
		}
	}
	// A missing value still propagates rather than being silently dropped.
	got := Trend([]float64{1, 2, math.NaN(), 4, 5, 6})
	if !math.IsNaN(got.Slope) || !math.IsNaN(got.StdErr) || !math.IsNaN(got.T) || got.P != 1 {
		t.Errorf("NaN must propagate: %+v", got)
	}
	if got.Rising(DefaultAlpha) || got.Falling(DefaultAlpha) {
		t.Error("an unmeasurable series is neither rising nor falling")
	}
}

// trendSlopeExactXYZ computes the OLS slope of y against its index in 300-bit
// arithmetic: sxy and sxx are formed from the exact integer index, so the only
// error left is in the final rounding, some 60 digits below float64.
func trendSlopeExactXYZ(y []float64) *big.Float {
	const prec = 300
	n := len(y)
	nf := new(big.Float).SetPrec(prec).SetInt64(int64(n))
	sy := new(big.Float).SetPrec(prec)
	for _, v := range y {
		sy.Add(sy, new(big.Float).SetPrec(prec).SetFloat64(v))
	}
	meanY := new(big.Float).SetPrec(prec).Quo(sy, nf)
	meanX := new(big.Float).SetPrec(prec).SetFloat64(float64(n-1) / 2)
	sxy := new(big.Float).SetPrec(prec)
	dx := new(big.Float).SetPrec(prec)
	dy := new(big.Float).SetPrec(prec)
	for i, v := range y {
		dx.SetInt64(int64(i))
		dx.Sub(dx, meanX)
		dy.SetFloat64(v)
		dy.Sub(dy, meanY)
		sxy.Add(sxy, dy.Mul(dy, dx))
	}
	nn := big.NewInt(int64(n))
	num := new(big.Int).Mul(nn, nn)
	num.Sub(num, big.NewInt(1))
	num.Mul(num, nn)
	sxx := new(big.Float).SetPrec(prec).SetInt(num)
	sxx.Quo(sxx, new(big.Float).SetPrec(prec).SetInt64(12))
	return new(big.Float).SetPrec(prec).Quo(sxy, sxx)
}

func relativeToExactXYZ(got float64, want *big.Float) float64 {
	d := new(big.Float).SetPrec(300).SetFloat64(got)
	d.Sub(d, want)
	d.Quo(d, want)
	f, _ := d.Abs(d).Float64()
	return f
}

// Where the two disagree, this says which one is wrong: both are held against
// exact arithmetic.
//
// It compares an ensemble rather than a single series on purpose. A single
// draw differs in the last place or two, where the winner is luck; what
// matters is whether the error of the rewrite is systematically larger, so
// each regime is sampled repeatedly and the root-mean-square relative error
// compared. The rewrite is allowed to be up to twice the old error before it
// counts as a regression — a factor of two is inside the run-to-run spread of
// this measurement, while the regression this guards against (the y[0]-shifted
// form that this one replaced) was a factor of ten on the flat regime.
func TestTrendClosedFormIsNoLessAccurateXYZ(t *testing.T) {
	const reps = 60
	for _, regime := range []struct {
		name string
		n    int
		make func(r *mrand.Rand, i int) float64
	}{
		{"trending under noise", 2048, func(r *mrand.Rand, i int) float64 {
			return 0.001*float64(i) + r.NormFloat64()
		}},
		{"a 1e9 offset", 2048, func(r *mrand.Rand, i int) float64 {
			return 1e9 + 0.5*float64(i) + r.NormFloat64()
		}},
		{"nearly flat", 2048, func(r *mrand.Rand, i int) float64 {
			return 1e-9*float64(i) + r.NormFloat64()
		}},
		{"a steep line under light noise", 2048, func(r *mrand.Rand, i int) float64 {
			return 1e4*float64(i) + 1e-6*r.NormFloat64()
		}},
		{"long and trending", 20000, func(r *mrand.Rand, i int) float64 {
			return 0.001*float64(i) + r.NormFloat64()
		}},
	} {
		r := mrand.New(mrand.NewSource(99991))
		n := reps
		if regime.n > 10000 {
			n = 4 // the exact reference is the slow part here
		}
		var newSq, oldSq float64
		y := make([]float64, regime.n)
		for rep := 0; rep < n; rep++ {
			for i := range y {
				y[i] = regime.make(r, i)
			}
			exact := trendSlopeExactXYZ(y)
			ne := relativeToExactXYZ(Trend(y).Slope, exact)
			oe := relativeToExactXYZ(trendReferenceXYZ(y).Slope, exact)
			newSq += ne * ne
			oldSq += oe * oe
		}
		newRMS, oldRMS := math.Sqrt(newSq/float64(n)), math.Sqrt(oldSq/float64(n))
		t.Logf("n=%-6d %-30s closed form %.3g, three-pass %.3g (RMS relative error vs 300-bit)",
			regime.n, regime.name, newRMS, oldRMS)
		if newRMS > 2*oldRMS {
			t.Errorf("%s: the rewrite is systematically less accurate: %.3g vs %.3g", regime.name, newRMS, oldRMS)
		}
		if newRMS > 1e-12 {
			t.Errorf("%s: closed form off by %.3g, want <= 1e-12", regime.name, newRMS)
		}
	}
}

// A long constant series at 1e300 overflows the sum the old code took first,
// so it reported NaN for a series whose slope is plainly zero. The new code
// never forms that sum: it shifts by y[0], which is exactly what makes a
// constant series collapse to zero rather than to infinity. This is a
// deliberate divergence from the old behaviour, in the direction of the right
// answer, and it is recorded here so it cannot regress unnoticed.
func TestTrendClosedFormBeatsOverflowXYZ(t *testing.T) {
	y := []float64{1.7e308, 1.7e308, 1.7e308, 1.7e308, 1.7e308}
	old := trendReferenceXYZ(y)
	if !math.IsNaN(old.Slope) {
		t.Fatalf("fixture no longer exercises the overflow: old slope %v", old.Slope)
	}
	got := Trend(y)
	if got.Slope != 0 || got.P != 1 || got.StdErr != 0 || got.T != 0 {
		t.Errorf("a constant series at the top of the range is flat, not unknowable: %+v", got)
	}
}
