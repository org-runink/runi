// SPDX-License-Identifier: BSD-3-Clause

package stats

import (
	"math"
	"math/rand"
	"testing"
)

// Published two-sided critical values from any t-table: P(|T| >= crit) = alpha.
func TestTwoSidedPAgainstPublishedCriticalValues(t *testing.T) {
	for _, tc := range []struct {
		df    int
		crit  float64
		alpha float64
	}{
		{1, 12.706, 0.05}, {10, 2.228, 0.05}, {18, 2.101, 0.05}, {20, 2.086, 0.05},
		{30, 2.042, 0.05}, {60, 2.000, 0.05}, {120, 1.980, 0.05},
		{1, 63.657, 0.01}, {10, 3.169, 0.01}, {18, 2.878, 0.01}, {20, 2.845, 0.01},
		{30, 2.750, 0.01}, {60, 2.660, 0.01}, {120, 2.617, 0.01},
	} {
		got := TwoSidedP(tc.crit, tc.df)
		if rel := math.Abs(got-tc.alpha) / tc.alpha; rel > 0.01 {
			t.Errorf("TwoSidedP(%v, df=%d) = %.10g, want ≈ %v", tc.crit, tc.df, got, tc.alpha)
		}
	}
}

// Reference values computed independently (scipy.stats.t.sf * 2).
func TestTwoSidedPAgainstReferenceValues(t *testing.T) {
	for _, tc := range []struct {
		t    float64
		df   int
		want float64
	}{
		{1.0, 5, 0.363217467649},
		{0.5, 18, 0.623132457297},
		{3.0, 18, 0.00768541214031},
		{2.228, 10, 0.0500117718171},
		{2.086, 20, 0.0499963544574},
		{2.750, 30, 0.00999989452693},
		{25.9408190555, 18, 1.0375193824e-15},
	} {
		got := TwoSidedP(tc.t, tc.df)
		if rel := math.Abs(got-tc.want) / tc.want; rel > 1e-9 {
			t.Errorf("TwoSidedP(%v, df=%d) = %.12g, want %.12g", tc.t, tc.df, got, tc.want)
		}
	}
}

func TestTwoSidedPProperties(t *testing.T) {
	if got := TwoSidedP(0, 18); math.Abs(got-1) > 1e-12 {
		t.Errorf("P(|T| >= 0) = %v, want 1", got)
	}
	for _, tv := range []float64{0.25, 1, 2.5, 7} {
		for _, df := range []int{1, 5, 18, 200} {
			if a, b := TwoSidedP(tv, df), TwoSidedP(-tv, df); math.Abs(a-b) > 1e-15 {
				t.Errorf("not symmetric at t=%v df=%d: %v vs %v", tv, df, a, b)
			}
		}
	}
	prev := 1.0
	for tv := 0.0; tv < 8; tv += 0.1 {
		p := TwoSidedP(tv, 18)
		if p > prev+1e-15 {
			t.Fatalf("tail increased with |t| at t=%v", tv)
		}
		prev = p
	}
	if got := TwoSidedP(1.959964, 1000000); math.Abs(got-0.05) > 1e-5 {
		t.Errorf("at large df the t tail must approach the normal tail; got %v", got)
	}
	for _, c := range []struct {
		t    float64
		df   int
		want float64
	}{{5, 0, 1}, {math.NaN(), 18, 1}, {math.Inf(1), 18, 0}, {math.Inf(-1), 3, 0}} {
		if got := TwoSidedP(c.t, c.df); got != c.want {
			t.Errorf("TwoSidedP(%v, %d) = %v, want %v", c.t, c.df, got, c.want)
		}
	}
}

func TestIncompleteBeta(t *testing.T) {
	for _, x := range []float64{0.1, 0.25, 0.5, 0.75, 0.9} {
		if got := incompleteBeta(1, 1, x); math.Abs(got-x) > 1e-14 {
			t.Errorf("I_%v(1,1) = %v, want %v", x, got, x)
		}
	}
	for _, x := range []float64{0.2, 0.6, 0.95} {
		if got := incompleteBeta(2, 1, x); math.Abs(got-x*x) > 1e-13 {
			t.Errorf("I_%v(2,1) = %v, want %v", x, got, x*x)
		}
		if got, want := incompleteBeta(1, 2, x), 2*x-x*x; math.Abs(got-want) > 1e-13 {
			t.Errorf("I_%v(1,2) = %v, want %v", x, got, want)
		}
	}
	for _, a := range []float64{0.5, 1, 3, 9, 50} {
		if got := incompleteBeta(a, a, 0.5); math.Abs(got-0.5) > 1e-13 {
			t.Errorf("I_0.5(%v,%v) = %v, want 0.5", a, a, got)
		}
	}
	for _, x := range []float64{0.05, 0.3, 0.7, 0.99} {
		if l, r := incompleteBeta(3, 7, x), 1-incompleteBeta(7, 3, 1-x); math.Abs(l-r) > 1e-13 {
			t.Errorf("reflection disagrees at x=%v: %v vs %v", x, l, r)
		}
	}
	if incompleteBeta(2, 3, 0) != 0 || incompleteBeta(2, 3, 1) != 1 {
		t.Error("I_0 must be 0 and I_1 must be 1")
	}
}

// scipy.stats.false_discovery_control on the same families.
func TestBenjaminiHochbergAgainstReference(t *testing.T) {
	for _, tc := range []struct {
		name   string
		p, q   []float64
		reject []bool
	}{
		{
			"ten-hypothesis family",
			[]float64{0.001, 0.008, 0.039, 0.041, 0.042, 0.06, 0.074, 0.205, 0.212, 0.216},
			[]float64{0.01, 0.04, 0.084, 0.084, 0.084, 0.1, 0.105714285714, 0.216, 0.216, 0.216},
			[]bool{true, true, false, false, false, false, false, false, false, false},
		},
		{"monotone downward", []float64{0.01, 0.02, 0.03, 0.9}, []float64{0.04, 0.04, 0.04, 0.9}, []bool{true, true, true, false}},
		{"ties", []float64{0.04, 0.04, 0.04, 0.04, 0.04}, []float64{0.04, 0.04, 0.04, 0.04, 0.04}, []bool{true, true, true, true, true}},
		{"clamped to one", []float64{0.9, 0.95}, []float64{0.95, 0.95}, []bool{false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, rej := BenjaminiHochberg(tc.p, 0.05)
			for i := range tc.p {
				if math.Abs(q[i]-tc.q[i]) > 1e-10 || rej[i] != tc.reject[i] {
					t.Errorf("[%d] q=%.12g reject=%v, want q=%.12g reject=%v", i, q[i], rej[i], tc.q[i], tc.reject[i])
				}
			}
		})
	}
}

func TestBenjaminiHochbergIsMonotoneAndBounded(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	p := make([]float64, 200)
	for i := range p {
		p[i] = rng.Float64()
	}
	q, _ := BenjaminiHochberg(p, 0.05)
	for i := range p {
		if q[i] < p[i]-1e-12 || q[i] > 1 {
			t.Fatalf("q[%d]=%v out of [p, 1] for p=%v", i, q[i], p[i])
		}
		for j := range p {
			if p[i] < p[j] && q[i] > q[j]+1e-12 {
				t.Fatalf("not monotone: p=%v→%v but p=%v→%v", p[i], q[i], p[j], q[j])
			}
		}
	}
	if q, rej := BenjaminiHochberg(nil, 0.05); len(q) != 0 || len(rej) != 0 {
		t.Error("an empty family must give empty output")
	}
	// An out-of-range alpha falls back to DefaultAlpha.
	if _, rej := BenjaminiHochberg([]float64{0.01}, 0); !rej[0] {
		t.Error("alpha 0 must mean DefaultAlpha")
	}
	if _, rej := BenjaminiHochberg([]float64{0.06}, 2); rej[0] {
		t.Error("alpha 2 must mean DefaultAlpha")
	}
}

// Under a true null every p is Uniform(0,1). Raw p <= 0.05 flags almost every
// 45-test family; BH must keep that near alpha.
func TestBenjaminiHochbergHoldsTheFDRUnderTheNull(t *testing.T) {
	rng := rand.New(rand.NewSource(20260912))
	const families, m = 2000, 45
	raw, corrected := 0, 0
	for range families {
		p := make([]float64, m)
		anyRaw := false
		for i := range p {
			p[i] = rng.Float64()
			anyRaw = anyRaw || p[i] <= 0.05
		}
		if anyRaw {
			raw++
		}
		_, rej := BenjaminiHochberg(p, 0.05)
		for _, r := range rej {
			if r {
				corrected++
				break
			}
		}
	}
	if float64(raw)/families < 0.8 {
		t.Fatalf("fixture not exercising the problem: raw flagged %d/%d", raw, families)
	}
	if rate := float64(corrected) / families; rate > 0.10 {
		t.Errorf("BH flagged %.1f%% of all-null families; want near 5%%", rate*100)
	}
}

// scipy.stats.linregress on the same series.
func TestTrendAgainstReferenceValues(t *testing.T) {
	rising := []float64{2, 4, 5, 4, 5, 7, 8, 9, 8, 10, 12, 11, 13, 14, 13, 16, 17, 16, 19, 20}
	got := Trend(rising)
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"slope", got.Slope, 0.886466165414},
		{"stderr", got.StdErr, 0.034172635934},
		{"t", got.T, 25.9408190555},
		{"p", got.P, 1.0375193824e-15},
	} {
		if rel := math.Abs(c.got-c.want) / math.Abs(c.want); rel > 1e-9 {
			t.Errorf("%s = %.12g, want %.12g", c.name, c.got, c.want)
		}
	}
	if !got.Rising(DefaultAlpha) || got.Falling(DefaultAlpha) {
		t.Error("a significant rise must be Rising and not Falling")
	}

	flat := Trend([]float64{5, 4, 6, 5, 6, 4, 5, 6, 4, 5, 6, 5, 4, 6, 5, 4, 6, 5, 4, 5})
	if math.Abs(flat.Slope-(-0.0105263157895)) > 1e-12 || math.Abs(flat.P-0.742624967433) > 1e-9 {
		t.Errorf("flat series: slope %.12g p %.12g", flat.Slope, flat.P)
	}
	if flat.Rising(DefaultAlpha) || flat.Falling(DefaultAlpha) {
		t.Error("a non-zero slope with no evidence must be neither rising nor falling")
	}
	falling := Trend([]float64{9, 8, 8, 7, 6, 6, 5, 4, 4, 3})
	if !falling.Falling(DefaultAlpha) {
		t.Errorf("a clear fall must be Falling: %+v", falling)
	}
}

func TestTrendDegenerateInputs(t *testing.T) {
	if got := Trend(nil); got.P != 1 || got.Slope != 0 {
		t.Errorf("empty: %+v", got)
	}
	if got := Trend([]float64{1, 2}); got.P != 1 {
		t.Errorf("two points have no residual to test against: %+v", got)
	}
	if got := Trend([]float64{3, 3, 3, 3, 3, 3}); got.Slope != 0 || got.P != 1 {
		t.Errorf("constant: %+v", got)
	}
	if got := Trend([]float64{1, 2, 3, 4, 5, 6}); got.P != 0 || got.Slope != 1 || !math.IsInf(got.T, 1) {
		t.Errorf("exact rising line: %+v", got)
	}
	if got := Trend([]float64{6, 5, 4, 3}); got.P != 0 || !math.IsInf(got.T, -1) {
		t.Errorf("exact falling line: %+v", got)
	}
}

func TestCorrelateRefusesWhatItCannotAnswer(t *testing.T) {
	short := []float64{1, 2, 3, 4, 5}
	if _, ok := Correlate(short, short); ok {
		t.Error("below MinCorrelationSamples must be ok=false")
	}
	constant := make([]float64, 30)
	ramp := make([]float64, 30)
	for i := range ramp {
		ramp[i] = float64(i)
	}
	if _, ok := Correlate(constant, ramp); ok {
		t.Error("a constant series has no correlation to report")
	}
	if !math.IsNaN(pearsonOrNaN([]float64{1, 2}, []float64{1})) || !math.IsNaN(pearsonOrNaN(nil, nil)) {
		t.Error("mismatched or too-short input must be NaN")
	}
}

func TestCorrelateAlignsOnTheLeadingWindow(t *testing.T) {
	a := make([]float64, 25)
	b := make([]float64, 40)
	for i := range b {
		b[i] = float64(i % 7)
		if i < len(a) {
			a[i] = 2*b[i] + 1
		}
	}
	c, ok := Correlate(a, b)
	if !ok || c.N != 25 || math.Abs(c.R-1) > 1e-12 || c.P != 0 || !c.Significant {
		t.Errorf("perfect linear relation over 25 aligned points: %+v ok=%v", c, ok)
	}
}

func TestCorrelationPValue(t *testing.T) {
	for _, c := range []struct {
		r    float64
		n    int
		want float64
	}{{0.5, 2, 1}, {math.NaN(), 30, 1}, {1, 30, 0}, {-1.2, 30, 0}, {0, 30, 1}} {
		if got := CorrelationPValue(c.r, c.n); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("CorrelationPValue(%v, %d) = %v, want %v", c.r, c.n, got, c.want)
		}
	}
	// r = 0.5 at n = 20 is t = 0.5·sqrt(18)/sqrt(0.75) on 18 df; the t tail
	// itself is pinned against published values above.
	want := TwoSidedP(0.5*math.Sqrt(18)/math.Sqrt(0.75), 18)
	if got := CorrelationPValue(0.5, 20); math.Abs(got-want) > 1e-15 || math.Abs(got-0.0247696) > 1e-6 {
		t.Errorf("r=0.5 n=20: p = %v, want %v", got, want)
	}
}

// Independent noise must rarely survive the correction, and a planted
// relationship must.
func TestAdjustKeepsRealPairsAndDropsNoise(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	const n = 60
	series := make([][]float64, 10)
	for i := range series {
		series[i] = make([]float64, n)
		for j := range series[i] {
			series[i][j] = rng.NormFloat64()
		}
	}
	for j := range series[1] {
		series[1][j] = series[0][j] + 0.3*rng.NormFloat64() // planted
	}
	var cs []Correlation
	var planted int
	for i := range series {
		for k := i + 1; k < len(series); k++ {
			c, ok := Correlate(series[i], series[k])
			if !ok {
				t.Fatal("unexpected refusal")
			}
			if i == 0 && k == 1 {
				planted = len(cs)
			}
			cs = append(cs, c)
		}
	}
	Adjust(cs, DefaultAlpha)
	kept := 0
	for i, c := range cs {
		if c.Q < c.P-1e-12 {
			t.Fatalf("q below p at %d", i)
		}
		if c.Significant {
			kept++
		}
	}
	if !cs[planted].Significant {
		t.Errorf("the planted relationship did not survive: %+v", cs[planted])
	}
	if kept > 2 {
		t.Errorf("%d of %d pairs kept; only the planted one is real", kept, len(cs))
	}
}

// x = (a+1)/(a+b) zeroes the fraction's first denominator; the guard must
// keep it finite rather than divide by zero.
func TestBetaFractionGuardsAZeroDenominator(t *testing.T) {
	if got := betaFraction(1, 1, 1); math.IsNaN(got) || math.IsInf(got, 0) {
		t.Errorf("betaFraction(1,1,1) = %v, want finite", got)
	}
}

// The documented behaviour for a missing value: everything the standard error
// feeds propagates as NaN, and P stays at 1 so neither Rising nor Falling can
// fire on a series that was never measurable. FACE reported this as a
// divergence from its own copy, which returned a defined StdErr of 0 — a
// number that reads as an infinitely precise estimate of a slope that does not
// exist. NaN is the honest answer, so it is the one that is now documented.
func TestTrendNaNPropagates(t *testing.T) {
	y := []float64{1, 2, math.NaN(), 4, 5, 6}
	got := Trend(y)

	if !math.IsNaN(got.Slope) {
		t.Errorf("Slope = %v, want NaN", got.Slope)
	}
	if !math.IsNaN(got.StdErr) {
		t.Errorf("StdErr = %v, want NaN (not a defined 0)", got.StdErr)
	}
	if !math.IsNaN(got.T) {
		t.Errorf("T = %v, want NaN", got.T)
	}
	if got.P != 1 {
		t.Errorf("P = %v, want 1", got.P)
	}
	if got.N != len(y) {
		t.Errorf("N = %d, want %d", got.N, len(y))
	}
	if got.Rising(0.05) || got.Falling(0.05) {
		t.Error("an unmeasurable series reported a direction")
	}
}
