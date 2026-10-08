package arimax

import (
	"errors"
	"math"
)

// ErrSingular is returned when the design matrix has no full-rank solution.
var ErrSingular = errors.New("arimax: design matrix is rank deficient")

// olsQR solves min ||y − Xb|| by Householder QR and returns b.
//
// QR rather than the normal equations (X'X)^-1 X'y on purpose: forming X'X
// squares the condition number, and exogenous regressors in this domain are
// routinely near-collinear (ad spend across channels moves together). QR costs
// about twice the flops and keeps the conditioning.
//
// X is row-major, n rows by p columns, and is overwritten.
func olsQR(x []float64, y []float64, n, p int) ([]float64, error) {
	if p == 0 {
		return nil, nil
	}
	if n < p {
		return nil, ErrSingular
	}
	b := make([]float64, len(y))
	copy(b, y)

	for k := 0; k < p; k++ {
		// Householder vector for column k below the diagonal.
		var norm float64
		for i := k; i < n; i++ {
			v := x[i*p+k]
			norm += v * v
		}
		norm = math.Sqrt(norm)
		if norm == 0 || math.IsNaN(norm) {
			return nil, ErrSingular
		}
		alpha := -norm
		if x[k*p+k] < 0 {
			alpha = norm
		}
		v := make([]float64, n)
		for i := k; i < n; i++ {
			v[i] = x[i*p+k]
		}
		v[k] -= alpha
		var vnorm float64
		for i := k; i < n; i++ {
			vnorm += v[i] * v[i]
		}
		if vnorm < 1e-300 {
			continue
		}
		// Apply H = I − 2vv'/v'v to the remaining columns and to b.
		for j := k; j < p; j++ {
			var dot float64
			for i := k; i < n; i++ {
				dot += v[i] * x[i*p+j]
			}
			f := 2 * dot / vnorm
			for i := k; i < n; i++ {
				x[i*p+j] -= f * v[i]
			}
		}
		var dot float64
		for i := k; i < n; i++ {
			dot += v[i] * b[i]
		}
		f := 2 * dot / vnorm
		for i := k; i < n; i++ {
			b[i] -= f * v[i]
		}
	}

	// Back-substitute the upper-triangular R.
	out := make([]float64, p)
	for i := p - 1; i >= 0; i-- {
		s := b[i]
		for j := i + 1; j < p; j++ {
			s -= x[i*p+j] * out[j]
		}
		d := x[i*p+i]
		if math.Abs(d) < 1e-12 {
			return nil, ErrSingular
		}
		out[i] = s / d
	}
	return out, nil
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}
