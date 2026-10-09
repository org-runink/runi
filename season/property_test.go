package season

import (
	"math"
	"math/rand/v2"
	"testing"
)

// season is where a property test earns its keep: the bug this package shipped
// with was an outlier filter whose scale estimate fired on roughly one clean
// point in 37 instead of one in 15787, and no example test noticed because
// every example happened to miss it. These properties are stated over
// thousands of generated series, so a filter that is slightly too eager has
// nowhere to hide.

// An additive decomposition must ADD UP. trend + season + residual is the
// series it was given, to the last bit, for every series -- otherwise the
// three components describe something other than the data.
func TestPropertyDecompositionIsExact(t *testing.T) {
	r := rand.New(rand.NewPCG(81, 82))
	for i := 0; i < 1000; i++ {
		n := 4 + r.IntN(300)
		x := make([]float64, n)
		for j := range x {
			x[j] = 10*math.Sin(float64(j)*2*math.Pi/float64(3+r.IntN(20))) +
				0.05*float64(j) + r.NormFloat64()
		}
		d, err := Decompose(x, Options{})
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if len(d.Trend) != n || len(d.Seasonal) != n || len(d.Residual) != n {
			t.Fatalf("n=%d: component lengths %d/%d/%d", n, len(d.Trend), len(d.Seasonal), len(d.Residual))
		}
		for j := range x {
			sum := d.Trend[j] + d.Seasonal[j] + d.Residual[j]
			if math.Abs(sum-x[j]) > 1e-9*(1+math.Abs(x[j])) {
				t.Fatalf("n=%d point %d: components sum to %v, series is %v", n, j, sum, x[j])
			}
		}
	}
}

// Every component must be a number. A NaN anywhere in a decomposition
// poisons every forecast taken from it, and a single singular solve would put
// one there.
func TestPropertyNoComponentIsEverNaN(t *testing.T) {
	r := rand.New(rand.NewPCG(83, 84))
	for i := 0; i < 2000; i++ {
		n := 4 + r.IntN(120)
		x := make([]float64, n)
		switch r.IntN(4) {
		case 0: // constant: no trend, no season, nothing to fit
			c := r.NormFloat64()
			for j := range x {
				x[j] = c
			}
		case 1: // a perfect ramp, which is collinear with the intercept
			for j := range x {
				x[j] = float64(j)
			}
		case 2: // a step, so the changepoint search has something exact to find
			for j := range x {
				if j > n/2 {
					x[j] = 100
				}
			}
		default:
			for j := range x {
				x[j] = r.NormFloat64()
			}
		}
		d, err := Decompose(x, Options{})
		if err != nil {
			continue // refusing is allowed; returning nonsense is not
		}
		for j := range x {
			if math.IsNaN(d.Trend[j]) || math.IsNaN(d.Seasonal[j]) || math.IsNaN(d.Residual[j]) {
				t.Fatalf("case %d n=%d: NaN at %d", i%4, n, j)
			}
		}
		for h, v := range d.Forecast(5) {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("case %d n=%d: forecast[%d] = %v", i%4, n, h, v)
			}
		}
	}
}

// The Hampel filter replaces a point with a local MEDIAN, so its output can
// never leave the range of its input. A filter that invented a value outside
// the data would be adding signal, not removing noise.
func TestPropertyHampelStaysInsideTheData(t *testing.T) {
	r := rand.New(rand.NewPCG(85, 86))
	for i := 0; i < 3000; i++ {
		n := 1 + r.IntN(200)
		x := make([]float64, n)
		lo, hi := math.Inf(1), math.Inf(-1)
		for j := range x {
			x[j] = r.NormFloat64() * 100
			lo, hi = math.Min(lo, x[j]), math.Max(hi, x[j])
		}
		for j, v := range hampel(x) {
			if v < lo-1e-9 || v > hi+1e-9 {
				t.Fatalf("n=%d: hampel invented %v at %d, outside [%v,%v]", n, v, j, lo, hi)
			}
		}
	}
}

// THE regression property. On clean Gaussian noise the filter must leave
// nearly everything alone: a 4-MAD threshold against a correct scale estimate
// touches about one point in 15787. Measured over many series rather than one,
// because the defect this replaces looked fine on any single draw.
func TestPropertyHampelBarelyTouchesCleanNoise(t *testing.T) {
	r := rand.New(rand.NewPCG(87, 88))
	var points, changed int
	for i := 0; i < 400; i++ {
		n := 150
		x := make([]float64, n)
		for j := range x {
			x[j] = r.NormFloat64()
		}
		out := hampel(x)
		for j := range x {
			points++
			if out[j] != x[j] {
				changed++
			}
		}
	}
	// Generous against the theoretical 1/15787 so the test is not itself
	// flaky, and still two orders of magnitude tighter than the 1-in-37 the
	// broken scale estimate produced.
	if rate := float64(changed) / float64(points); rate > 0.002 {
		t.Fatalf("hampel changed %d of %d clean points (%.4f%%), want under 0.2%%",
			changed, points, rate*100)
	}
}

// A planted cycle must be found, at every period, not just the one an example
// test picked.
func TestPropertyPeriodRecoversEveryPlantedCycle(t *testing.T) {
	r := rand.New(rand.NewPCG(89, 90))
	// From 3: a period-2 cycle sits at the Nyquist limit, where a sinusoid
	// sampled at integers is either a constant or identically zero, and there
	// is nothing left in the data to recover. Twelve cycles and at least 64
	// points, because the bar a lag must clear falls as the series grows and a
	// long period needs the room -- see MinPeriodLength.
	for p := 3; p <= 40; p++ {
		n := 12 * p
		if n < 64 {
			n = 64
		}
		x := make([]float64, n)
		amp := 1 + r.Float64()*50
		phase := r.Float64() * 2 * math.Pi
		for j := range x {
			x[j] = amp * math.Sin(2*math.Pi*float64(j)/float64(p)+phase)
		}
		if got := Period(x, 0); got != p {
			t.Fatalf("planted period %d (n=%d, amp=%.1f), Period said %d", p, n, amp, got)
		}
	}
}

// Changepoints must return indices that are strictly increasing, inside the
// series, and never the endpoints -- a break at 0 or n is not a break. A
// caller slices the series on these, so a duplicate or an out-of-range index
// is an empty or panicking segment.
func TestPropertyChangepointsAreUsableIndices(t *testing.T) {
	r := rand.New(rand.NewPCG(91, 92))
	for i := 0; i < 2000; i++ {
		n := 4 + r.IntN(300)
		x := make([]float64, n)
		for j := range x {
			x[j] = r.NormFloat64() + float64(j)*r.Float64()
		}
		maxK := r.IntN(6)
		got := Changepoints(x, maxK)
		if maxK > 0 && len(got) > maxK {
			t.Fatalf("maxK=%d, got %d breaks", maxK, len(got))
		}
		for j, cp := range got {
			if cp <= 0 || cp >= n {
				t.Fatalf("n=%d: break at %d is not inside the series", n, cp)
			}
			if j > 0 && cp <= got[j-1] {
				t.Fatalf("breaks not increasing: %v", got)
			}
		}
	}
}

// A Fourier fit of a series that IS a sum of harmonics of its period must
// reproduce it. This is what pins the phase-bucketed normal equations: they
// are an exact rewrite of the full ones, not an approximation, and a wrong
// bucket count would show as a fit that no longer passes through the data.
func TestPropertyFourierReproducesItsOwnHarmonics(t *testing.T) {
	r := rand.New(rand.NewPCG(93, 94))
	for i := 0; i < 500; i++ {
		p := 6 + r.IntN(30)
		h := 1 + r.IntN(3)
		if 2*h+1 > p {
			continue // more unknowns than distinct phases
		}
		n := p * (6 + r.IntN(6))
		a := make([]float64, h+1)
		b := make([]float64, h+1)
		for k := 1; k <= h; k++ {
			a[k] = r.NormFloat64() * 10
			b[k] = r.NormFloat64() * 10
		}
		mean := r.NormFloat64() * 5
		x := make([]float64, n)
		for j := range x {
			v := mean
			for k := 1; k <= h; k++ {
				ang := 2 * math.Pi * float64(k) * float64(j) / float64(p)
				v += a[k]*math.Cos(ang) + b[k]*math.Sin(ang)
			}
			x[j] = v
		}
		f, err := FitFourier(x, p, h)
		if err != nil {
			t.Fatalf("p=%d h=%d: %v", p, h, err)
		}
		for j := range x {
			// At is documented to exclude Mean, so the fitted value is the
			// sum of the two.
			if got := f.Mean + f.At(j); math.Abs(got-x[j]) > 1e-6*(1+math.Abs(x[j])) {
				t.Fatalf("p=%d h=%d: fit gives %v at %d, series is %v", p, h, got, j, x[j])
			}
		}
	}
}

// A Fourier fit is periodic by construction: At(t) and At(t+period) are the
// same value, for every t, including negative and far-future ones a forecast
// will reach.
func TestPropertyFourierIsPeriodic(t *testing.T) {
	r := rand.New(rand.NewPCG(95, 96))
	for i := 0; i < 1000; i++ {
		p := 4 + r.IntN(30)
		n := p * (5 + r.IntN(5))
		x := make([]float64, n)
		for j := range x {
			x[j] = math.Sin(2*math.Pi*float64(j)/float64(p)) + 0.1*r.NormFloat64()
		}
		f, err := FitFourier(x, p, 2)
		if err != nil {
			continue
		}
		for k := 0; k < 20; k++ {
			t0 := r.IntN(10000) - 5000
			if got, want := f.At(t0), f.At(t0+p); math.Abs(got-want) > 1e-9 {
				t.Fatalf("p=%d: At(%d)=%v but At(%d)=%v", p, t0, got, t0+p, want)
			}
		}
	}
}
