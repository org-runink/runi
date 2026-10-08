package arimax

import "math"

// arma holds a fitted ARMA(p,q) for a zero-mean series.
type arma struct {
	phi    []float64 // autoregressive
	theta  []float64 // moving average
	sigma2 float64   // residual variance
	resid  []float64
}

// cssResiduals runs the ARMA recursion and returns the residuals, conditioning
// on the first p observations and treating pre-sample errors as zero. That is
// the "conditional" in conditional sum of squares.
func cssResiduals(y, phi, theta []float64) []float64 {
	p, q := len(phi), len(theta)
	n := len(y)
	e := make([]float64, n)
	for t := p; t < n; t++ {
		pred := 0.0
		for i := 0; i < p; i++ {
			pred += phi[i] * y[t-1-i]
		}
		for j := 0; j < q; j++ {
			if t-1-j >= 0 {
				pred += theta[j] * e[t-1-j]
			}
		}
		e[t] = y[t] - pred
	}
	return e[p:]
}

// stationary reports whether the AR polynomial's companion matrix is likely
// stable, using the cheap sufficient check Σ|φ| < 1. It is conservative: it can
// reject a stationary model, which costs a little search space, but it cannot
// accept an explosive one, which would produce a forecast that runs away.
func stationary(phi []float64) bool {
	var s float64
	for _, v := range phi {
		s += math.Abs(v)
	}
	return s < 0.999
}

// fitARMA estimates φ and θ by minimising the conditional sum of squares.
func fitARMA(y []float64, p, q int) (*arma, error) {
	if p < 0 || q < 0 {
		return nil, errOrder
	}
	if len(y) <= p+q+1 {
		return nil, errShort
	}
	if p == 0 && q == 0 {
		e := make([]float64, len(y))
		copy(e, y)
		return &arma{sigma2: variance(e), resid: e}, nil
	}
	obj := func(par []float64) float64 {
		phi, theta := par[:p], par[p:]
		if !stationary(phi) {
			return math.Inf(1)
		}
		e := cssResiduals(y, phi, theta)
		var s float64
		for _, v := range e {
			s += v * v
		}
		if math.IsNaN(s) {
			return math.Inf(1)
		}
		return s
	}
	start := make([]float64, p+q)
	// A small positive AR start is a better basin than zero for the persistent
	// series this package targets; MA terms start at zero.
	for i := 0; i < p; i++ {
		start[i] = 0.1
	}
	best, _ := nelderMead(obj, start, 2000)
	phi := append([]float64(nil), best[:p]...)
	theta := append([]float64(nil), best[p:]...)
	e := cssResiduals(y, phi, theta)
	return &arma{phi: phi, theta: theta, sigma2: variance(e), resid: e}, nil
}

func variance(e []float64) float64 {
	if len(e) < 2 {
		return 0
	}
	m := mean(e)
	var s float64
	for _, v := range e {
		d := v - m
		s += d * d
	}
	return s / float64(len(e)-1)
}

// psiWeights returns the first h MA(∞) coefficients of the ARMA model. They are
// what makes a prediction interval widen with horizon in the correct shape:
// Var(forecast error at h) = sigma2 · Σ_{j<h} psi_j².
func psiWeights(a *arma, h int) []float64 {
	psi := make([]float64, h)
	if h == 0 {
		return psi
	}
	psi[0] = 1
	for j := 1; j < h; j++ {
		var v float64
		if j-1 < len(a.theta) {
			v = a.theta[j-1]
		}
		for i := 0; i < len(a.phi) && i < j; i++ {
			v += a.phi[i] * psi[j-1-i]
		}
		psi[j] = v
	}
	return psi
}
