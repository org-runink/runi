package arimax

import (
	"fmt"
	"math"
	"testing"
)

func synth(seed uint64, n int, phi, beta float64) (y, x []float64) {
	g := lcg(seed)
	y = make([]float64, n)
	x = make([]float64, n)
	var e float64
	for i := 0; i < n; i++ {
		x[i] = math.Sin(float64(i)/11)*10 + float64(i)*0.01
		e = phi*e + g.next()
		y[i] = 5 + beta*x[i] + e
	}
	return y, x
}

func BenchmarkFit(b *testing.B) {
	for _, n := range []int{100, 500, 2000, 10000} {
		y, x := synth(1, n, 0.6, 2.5)
		b.Run(fmt.Sprintf("n=%d/ARIMAX(1,0,1)+1x", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Fit(y, x, 1, Order{P: 1, Q: 1}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkForecast(b *testing.B) {
	y, x := synth(2, 2000, 0.6, 2.5)
	m, err := Fit(y, x, 1, Order{P: 1, Q: 1})
	if err != nil {
		b.Fatal(err)
	}
	xf := make([]float64, 24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := m.Forecast(24, xf, 0.05); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkACF(b *testing.B) {
	y, _ := synth(3, 5000, 0.7, 1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ACF(y, 40)
	}
}

func BenchmarkOLSQR(b *testing.B) {
	const n, k = 5000, 8
	cols := k + 1
	base := make([]float64, n*cols)
	y := make([]float64, n)
	g := lcg(9)
	for i := 0; i < n; i++ {
		base[i*cols] = 1
		for j := 0; j < k; j++ {
			base[i*cols+1+j] = g.next()
		}
		y[i] = g.next()
	}
	design := make([]float64, len(base))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(design, base)
		if _, err := olsQR(design, y, n, cols); err != nil {
			b.Fatal(err)
		}
	}
}
