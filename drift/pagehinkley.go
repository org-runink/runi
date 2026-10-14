// SPDX-License-Identifier: BSD-3-Clause

package drift

import "math"

// PageHinkley is the Page–Hinkley test for a change in the mean of a series
// whose level is not known in advance.
//
// It is CUSUM with the target replaced by the running mean of everything seen
// since the last Reset. With mean_t the mean of x_1..x_t, it keeps
//
//	m⁺_t = Σ (x_i − mean_i − δ),   M⁺_t = min(0, m⁺_1, …, m⁺_t)
//	m⁻_t = Σ (x_i − mean_i + δ),   M⁻_t = max(0, m⁻_1, …, m⁻_t)
//
// and signals Up when m⁺_t − M⁺_t > λ and Down when M⁻_t − m⁻_t > λ (the
// larger when both, Up on a tie). δ plays the part of CUSUM's k, the size of
// deviation tolerated without accumulating evidence, and λ the part of h.
//
// Use it when there is no target to supply: a series per process, per disk,
// per tenant, each settling at its own level. The price is that the reference
// level is learned from the series itself. Shortly after Reset it rests on a
// handful of samples, so the test is noisier than CUSUM with a true target.
// After a change the running mean slowly absorbs the new level, the evidence
// stops growing and then drains, and the next change would be measured
// against a blend of both regimes. Reset after acting on a signal. When the
// target is known, CUSUM is the sharper test.
//
// Method: E. S. Page, "Continuous inspection schemes", Biometrika 41(1/2),
// 1954; D. V. Hinkley, "Inference about the change-point from cumulative sum
// tests", Biometrika 58(3), 1971. The running-mean formulation is the one
// given by J. Gama, I. Žliobaitė, A. Bifet, M. Pechenizkiy and A. Bouchachia,
// "A survey on concept drift adaptation", ACM Computing Surveys 46(4), 2014.
//
// δ and λ are in the data's units and the caller's to choose. There is no
// literature default worth the name, since both scale with the noise. δ =
// σ/2 and λ = 5σ, the CUSUM defaults, are a reasonable start: at those values
// the in-control average run length measured here is about 480 samples, close
// to CUSUM's 465, and a one-sigma shift after 100 in-control samples is
// detected in about 11 to 12 (TestPageHinkleyARL). The delay grows when the
// shift comes early, while the mean still rests on few samples.
//
// The zero value is not usable; build one with NewPageHinkley. Safe to copy,
// not safe for concurrent use. NaN and ±Inf are skipped and counted in
// Skipped. Finite inputs within a factor of a few of the float64 limit can
// overflow the sums; rescale such a series first.
type PageHinkley struct {
	delta, lambda float64

	n         int64
	mean      float64
	up, upMin float64 // m⁺ and its running minimum M⁺
	dn, dnMax float64 // m⁻ and its running maximum M⁻
	skipped   int64
}

// NewPageHinkley returns a two-sided Page–Hinkley test with tolerance delta
// >= 0 and threshold lambda > 0, both finite.
func NewPageHinkley(delta, lambda float64) (PageHinkley, error) {
	switch {
	case !(delta >= 0) || math.IsInf(delta, 1):
		return PageHinkley{}, invalid("delta", delta, "finite and >= 0")
	case !(lambda > 0) || math.IsInf(lambda, 1):
		return PageHinkley{}, invalid("lambda", lambda, "finite and > 0")
	}
	return PageHinkley{delta: delta, lambda: lambda}, nil
}

// Add folds x in and reports whether either side is past lambda. Like CUSUM,
// a signal changes nothing: the statistics keep accumulating until Reset. It
// does not allocate.
func (p *PageHinkley) Add(x float64) Signal {
	if !finite(x) {
		p.skipped++
		return None
	}
	p.n++
	p.mean += (x - p.mean) / float64(p.n)
	d := x - p.mean
	p.up += d - p.delta
	p.upMin = min(p.upMin, p.up)
	p.dn += d + p.delta
	p.dnMax = max(p.dnMax, p.dn)
	return twoSided(p.up-p.upMin, p.dnMax-p.dn, p.lambda)
}

// Statistic is the evidence for each direction, m⁺ − M⁺ and M⁻ − m⁻. Both
// are never negative, and a side signals when it exceeds lambda.
func (p *PageHinkley) Statistic() (up, down float64) {
	return p.up - p.upMin, p.dnMax - p.dn
}

// N is the number of samples folded in.
func (p *PageHinkley) N() int64 { return p.n }

// Skipped is the number of non-finite values Add refused.
func (p *PageHinkley) Skipped() int64 { return p.skipped }

// Reset forgets the mean and both sums, keeping delta and lambda, and clears
// N and Skipped.
func (p *PageHinkley) Reset() { *p = PageHinkley{delta: p.delta, lambda: p.lambda} }
