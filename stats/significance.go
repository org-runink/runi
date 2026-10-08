// SPDX-License-Identifier: BSD-3-Clause

package stats

import (
	"math"
	"sort"
)

// MinCorrelationSamples is the number of aligned observations Correlate needs
// before it reports anything.
//
// Twenty is where a bare |r| >= 0.5 rule's false-positive rate on independent
// noise falls to about the nominal two-sided 5% (it is roughly 50% at n = 4,
// 31% at n = 6 and 20% at n = 8). With df = 18 the t-test still has useful power:
// a true |ρ| near 0.6 is detected at α = 0.05 about 80% of the time. Lowering
// it does not find more relationships; it invents them.
const MinCorrelationSamples = 20

// DefaultAlpha is the conventional significance level and false discovery
// rate: 0.05.
const DefaultAlpha = 0.05

// Correlation is a Pearson coefficient together with the evidence that makes
// it a finding rather than a coincidence.
type Correlation struct {
	R float64 `json:"r"` // Pearson r over the aligned window
	N int     `json:"n"` // aligned observations r was computed on
	P float64 `json:"p"` // two-sided p-value of r against ρ = 0, n-2 df
	// Q is the Benjamini–Hochberg adjusted p-value, set by Adjust. Zero until
	// a family correction has been applied.
	Q float64 `json:"q,omitempty"`
	// Significant is q <= alpha once Adjust has run, otherwise p <= DefaultAlpha.
	Significant bool `json:"significant"`
}

// Correlate returns the Pearson correlation of a and b over their overlapping
// leading window (the first min(len(a), len(b)) values), with its sample size
// and two-sided p-value.
//
// ok is false when the overlap is below MinCorrelationSamples, or when r is
// undefined because either series is constant over the window. A caller that
// ignores ok gets a zero Correlation, which is not significant.
func Correlate(a, b []float64) (c Correlation, ok bool) {
	n := min(len(a), len(b))
	if n < MinCorrelationSamples {
		return Correlation{}, false
	}
	r := pearsonOrNaN(a[:n], b[:n])
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return Correlation{}, false
	}
	p := CorrelationPValue(r, n)
	return Correlation{R: r, N: n, P: p, Significant: p <= DefaultAlpha}, true
}

// Adjust applies Benjamini–Hochberg at alpha across every correlation in cs,
// setting each Q and Significant in place. Use it when one analysis tests many
// pairs: k series give k(k-1)/2 tests, and a per-test 5% error rate compounds.
func Adjust(cs []Correlation, alpha float64) {
	p := make([]float64, len(cs))
	for i := range cs {
		p[i] = cs[i].P
	}
	q, keep := BenjaminiHochberg(p, alpha)
	for i := range cs {
		cs[i].Q, cs[i].Significant = q[i], keep[i]
	}
}

// CorrelationPValue is the two-sided p-value for a Pearson r on n paired
// observations, under the null that the true correlation is zero:
//
//	t = r·sqrt(n-2)/sqrt(1-r²),  df = n-2
//
// It returns 1 (no evidence) when n < 3 or r is NaN, and 0 for |r| = 1.
func CorrelationPValue(r float64, n int) float64 {
	df := n - 2
	if df < 1 || math.IsNaN(r) {
		return 1
	}
	r = max(-1, min(1, r))
	denom := 1 - r*r
	if denom <= 0 {
		return 0
	}
	return TwoSidedP(r*math.Sqrt(float64(df))/math.Sqrt(denom), df)
}

// TwoSidedP returns P(|T| >= |t|) for T distributed Student-t with df degrees
// of freedom, by the identity P(|T| >= |t|) = I_{df/(df+t²)}(df/2, 1/2), where
// I is the regularised incomplete beta function. It returns 1 for df < 1 or a
// NaN t, and 0 for an infinite t.
func TwoSidedP(t float64, df int) float64 {
	if df < 1 || math.IsNaN(t) {
		return 1
	}
	if math.IsInf(t, 0) {
		return 0
	}
	d := float64(df)
	return incompleteBeta(d/2, 0.5, d/(d+t*t))
}

// BenjaminiHochberg controls the false discovery rate across a family of m
// tests at level alpha. It returns, aligned with p, the adjusted q-values and
// whether each hypothesis is rejected (q <= alpha).
//
// The q-values use the step-up form q(i) = min over j >= i of (m/j)·p(j),
// clamped to 1, which keeps them monotone in p. An alpha outside (0, 1] is
// replaced by DefaultAlpha.
func BenjaminiHochberg(p []float64, alpha float64) (q []float64, reject []bool) {
	m := len(p)
	q = make([]float64, m)
	reject = make([]bool, m)
	if m == 0 {
		return q, reject
	}
	if alpha <= 0 || alpha > 1 {
		alpha = DefaultAlpha
	}
	order := make([]int, m)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return p[order[i]] < p[order[j]] })
	running := math.Inf(1)
	for rank := m; rank >= 1; rank-- {
		idx := order[rank-1]
		running = min(running, float64(m)/float64(rank)*p[idx])
		q[idx] = min(running, 1)
	}
	for i := range p {
		reject[i] = q[i] <= alpha
	}
	return q, reject
}

// TrendTest is an ordinary-least-squares slope of a series against its index,
// with the test of that slope against zero.
type TrendTest struct {
	Slope float64 `json:"slope"`
	// StdErr is the standard error of the slope. It is 0 when there is no
	// residual variance to estimate it from — too few points, or an exact
	// line — and NaN when the series contains one, because a missing value
	// makes the spread genuinely unknown rather than zero. See Trend.
	StdErr float64 `json:"std_err"`
	T      float64 `json:"t"`
	P      float64 `json:"p"` // two-sided p-value against slope = 0, n-2 df
	N      int     `json:"n"`
}

// Rising reports whether the series rises significantly at alpha.
func (t TrendTest) Rising(alpha float64) bool { return t.Slope > 0 && t.P <= alpha }

// Falling reports whether the series falls significantly at alpha.
func (t TrendTest) Falling(alpha float64) bool { return t.Slope < 0 && t.P <= alpha }

// Trend fits y against its index (0, 1, 2, …) by ordinary least squares and
// tests the slope against zero. P is 1 when the series is too short for a
// residual variance (n < 3) or is constant; an exact non-flat line has P = 0.
//
// A slope is never "rising" just because it is not exactly zero: on noise the
// fitted slope is essentially never zero. Use Rising and Falling, which ask
// the test.
//
// A NaN anywhere in y propagates: Slope, StdErr and T come back NaN and P
// comes back 1, so Rising and Falling are both false. Trend does not drop or
// interpolate missing values, because which of those is right depends on what
// the gap means, and it is not a decision this function can make for the
// caller. Clean the series first, or check the result with
// math.IsNaN. The one thing Trend will not do is report a defined standard
// error for a series it could not measure.
func Trend(y []float64) TrendTest {
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

// incompleteBeta is the regularised incomplete beta function I_x(a, b),
// evaluated with the modified Lentz continued fraction. Callers pass a, b > 0
// and 0 <= x <= 1.
func incompleteBeta(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	lab, _ := math.Lgamma(a + b)
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	front := math.Exp(lab - la - lb + a*math.Log(x) + b*math.Log1p(-x))
	// The fraction converges quickly only on one side of the symmetry point;
	// reflect with I_x(a,b) = 1 - I_{1-x}(b,a) on the other.
	if x < (a+1)/(a+b+2) {
		return front * betaFraction(a, b, x) / a
	}
	return 1 - front*betaFraction(b, a, 1-x)/b
}

// betaFraction evaluates the incomplete beta continued fraction.
func betaFraction(a, b, x float64) float64 {
	const (
		maxIter = 500
		epsilon = 3e-16
		tiny    = 1e-300
	)
	guard := func(v float64) float64 {
		if math.Abs(v) < tiny {
			return tiny
		}
		return v
	}
	qab, qap, qam := a+b, a+1, a-1
	c := 1.0
	d := 1 / guard(1-qab*x/qap)
	h := d
	for m := 1; m <= maxIter; m++ {
		fm := float64(m)
		m2 := 2 * fm
		aa := fm * (b - fm) * x / ((qam + m2) * (a + m2))
		d = 1 / guard(1+aa*d)
		c = guard(1 + aa/c)
		h *= d * c
		aa = -(a + fm) * (qab + fm) * x / ((a + m2) * (qap + m2))
		d = 1 / guard(1+aa*d)
		c = guard(1 + aa/c)
		del := d * c
		h *= del
		if math.Abs(del-1) < epsilon {
			break
		}
	}
	return h
}

// pearsonOrNaN returns NaN, not 0, when r is undefined (mismatched or too-short
// input, or a constant series): "no correlation" and "cannot say" differ.
func pearsonOrNaN(x, y []float64) float64 {
	n := len(x)
	if n != len(y) || n < 2 {
		return math.NaN()
	}
	var sx, sy float64
	for i := range n {
		sx += x[i]
		sy += y[i]
	}
	mx, my := sx/float64(n), sy/float64(n)
	var sxy, sxx, syy float64
	for i := range n {
		dx, dy := x[i]-mx, y[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	den := math.Sqrt(sxx * syy)
	if den < 1e-12 {
		return math.NaN()
	}
	return sxy / den
}
