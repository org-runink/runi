package arimax

import "math"

// nelderMead minimises f over a simplex built around start. It is derivative
// free, which suits the conditional sum of squares: the CSS surface is smooth
// enough to walk but its gradient is awkward to write correctly, and a wrong
// gradient fails silently by converging to the wrong place.
//
// Deterministic: the initial simplex is a fixed perturbation of start, so the
// same input always gives the same result.
func nelderMead(f func([]float64) float64, start []float64, maxIter int) ([]float64, float64) {
	n := len(start)
	if n == 0 {
		return nil, f(nil)
	}
	const (
		alpha = 1.0  // reflection
		gamma = 2.0  // expansion
		rho   = 0.5  // contraction
		sigma = 0.5  // shrink
		step  = 0.10 // initial simplex offset
	)
	pts := make([][]float64, n+1)
	val := make([]float64, n+1)
	for i := range pts {
		p := make([]float64, n)
		copy(p, start)
		if i > 0 {
			if p[i-1] != 0 {
				p[i-1] *= 1 + step
			} else {
				p[i-1] = step
			}
		}
		pts[i] = p
		val[i] = f(p)
	}
	order := func() {
		for i := 1; i < len(val); i++ {
			for j := i; j > 0 && val[j] < val[j-1]; j-- {
				val[j], val[j-1] = val[j-1], val[j]
				pts[j], pts[j-1] = pts[j-1], pts[j]
			}
		}
	}
	for it := 0; it < maxIter; it++ {
		order()
		if math.Abs(val[n]-val[0]) <= 1e-12*(math.Abs(val[0])+1e-12) {
			break
		}
		cent := make([]float64, n)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				cent[j] += pts[i][j]
			}
		}
		for j := range cent {
			cent[j] /= float64(n)
		}
		refl := make([]float64, n)
		for j := range refl {
			refl[j] = cent[j] + alpha*(cent[j]-pts[n][j])
		}
		fr := f(refl)
		switch {
		case fr < val[0]:
			exp := make([]float64, n)
			for j := range exp {
				exp[j] = cent[j] + gamma*(refl[j]-cent[j])
			}
			if fe := f(exp); fe < fr {
				pts[n], val[n] = exp, fe
			} else {
				pts[n], val[n] = refl, fr
			}
		case fr < val[n-1]:
			pts[n], val[n] = refl, fr
		default:
			con := make([]float64, n)
			for j := range con {
				con[j] = cent[j] + rho*(pts[n][j]-cent[j])
			}
			if fc := f(con); fc < val[n] {
				pts[n], val[n] = con, fc
			} else {
				for i := 1; i <= n; i++ {
					for j := 0; j < n; j++ {
						pts[i][j] = pts[0][j] + sigma*(pts[i][j]-pts[0][j])
					}
					val[i] = f(pts[i])
				}
			}
		}
	}
	order()
	return pts[0], val[0]
}
