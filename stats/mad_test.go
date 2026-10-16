package stats

import (
	"math"
	"testing"
)

// MAD is the median of the absolute deviations from the median. These are
// worked by hand so the test does not restate the implementation.
func TestMAD(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []float64
		want float64
	}{
		// median 3; deviations 2,1,0,1,2 -> median 1
		{"odd", []float64{1, 2, 3, 4, 5}, 1},
		// median 2.5; deviations 1.5,0.5,0.5,1.5 -> median 1
		{"even", []float64{1, 2, 3, 4}, 1},
		// every value identical: no spread
		{"no spread", []float64{7, 7, 7}, 0},
		// one wild value barely moves it, which is the point of MAD
		{"outlier resistant", []float64{1, 2, 3, 4, 1000}, 1},
		{"single", []float64{42}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := MAD(c.in); got != c.want {
				t.Errorf("MAD(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Empty input has no median, so it has no deviations from it.
func TestMADOfNothingIsNaN(t *testing.T) {
	if got := MAD(nil); !math.IsNaN(got) {
		t.Errorf("MAD(nil) = %v, want NaN", got)
	}
	if got := MAD([]float64{}); !math.IsNaN(got) {
		t.Errorf("MAD(empty) = %v, want NaN", got)
	}
}

// NaN PROPAGATES, matching Median rather than silently dropping data. A
// column with an unknown value has an unknown median and therefore unknown
// deviations from it; deciding to drop or impute is the caller's.
func TestMADPropagatesNaN(t *testing.T) {
	if got := MAD([]float64{1, 2, 3, 4, 5, math.NaN()}); !math.IsNaN(got) {
		t.Errorf("MAD with a NaN = %v, want NaN — it must not silently drop the value", got)
	}
	if got := MAD([]float64{math.NaN(), math.NaN()}); !math.IsNaN(got) {
		t.Errorf("MAD(all NaN) = %v, want NaN", got)
	}
}
