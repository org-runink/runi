package season

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"testing/quick"
)

// classicalRefXYZ is the implementation Classical had before it was made to
// stop copying the series it never writes to, to stop dividing once per point
// to find a phase, and to allocate its three components once instead of four
// times. It is kept here verbatim so that the rewrite can be held to the
// standard that matters: not "close enough", but the SAME arithmetic in the
// same order, down to the last bit.
//
// That is the bar because season.Classical's whole claim is that it agrees
// with statsmodels.tsa.seasonal.seasonal_decompose to 2e-13 on a series of
// magnitude 300. Any reordering of a floating-point sum puts that claim back
// in play. Keeping the operation order identical means the claim cannot move,
// and a bitwise test proves it rather than a tolerance that would hide drift.
func classicalRefXYZ(x []float64, period int) (*Decomposition, error) {
	if period < 2 {
		return nil, errBadPeriodXYZ
	}
	if len(x) < 2*period {
		return nil, ErrTooShort
	}
	if allNaN(x) {
		return nil, errNoValuesXYZ
	}
	y := fill(x)
	n := len(y)
	h := period / 2
	even := period%2 == 0

	trend := make([]float64, n)
	w := 0.0
	for _, v := range y[:period] {
		w += v
	}
	for t := h; t <= n-1-h; t++ {
		s := t - h
		if s > 0 {
			if s%period == 0 {
				w = 0
				for _, v := range y[s : s+period] {
					w += v
				}
			} else {
				w += y[s+period-1] - y[s-1]
			}
		}
		v := w
		if even {
			v += 0.5 * (y[t+h] - y[t-h])
		}
		trend[t] = v / float64(period)
	}

	sum := make([]float64, period)
	cnt := make([]int, period)
	for t := h; t <= n-1-h; t++ {
		ph := t % period
		sum[ph] += y[t] - trend[t]
		cnt[ph]++
	}
	mean := 0.0
	for i := range sum {
		sum[i] /= float64(cnt[i])
		mean += sum[i]
	}
	mean /= float64(period)
	for i := range sum {
		sum[i] -= mean
	}

	for t := 0; t < h; t++ {
		trend[t] = trend[h]
	}
	for t := n - h; t < n; t++ {
		trend[t] = trend[n-1-h]
	}

	d := &Decomposition{
		Period:   period,
		Trend:    trend,
		Seasonal: make([]float64, n),
		Residual: make([]float64, n),
		n:        n,
	}
	for t := range y {
		se := sum[t%period]
		d.Seasonal[t] = se
		d.Residual[t] = y[t] - trend[t] - se
	}
	return d, nil
}

// Sentinel errors so the reference can be compared against Classical by the
// message it returns, which is what a caller sees.
var (
	errBadPeriodXYZ = errStrXYZ("season: period must be at least 2")
	errNoValuesXYZ  = errStrXYZ("season: series has no values")
)

type errStrXYZ string

func (e errStrXYZ) Error() string { return string(e) }

// sameBitsXYZ compares two slices by bit pattern, so that a NaN matches a NaN
// and +0 does not match −0. Equality here is the point: a tolerance would let
// exactly the drift this test exists to catch through.
func sameBitsXYZ(a, b []float64) (int, bool) {
	if len(a) != len(b) {
		return -1, false
	}
	for i := range a {
		if math.Float64bits(a[i]) != math.Float64bits(b[i]) {
			return i, false
		}
	}
	return -1, true
}

// checkSameXYZ runs both implementations and insists they agree bit for bit,
// including on which error they return.
func checkSameXYZ(t *testing.T, name string, x []float64, period int) {
	t.Helper()
	got, gotErr := Classical(x, period)
	want, wantErr := classicalRefXYZ(x, period)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("%s: Classical err=%v, reference err=%v", name, gotErr, wantErr)
	}
	if gotErr != nil {
		if gotErr.Error() != wantErr.Error() {
			t.Errorf("%s: Classical err=%q, reference err=%q", name, gotErr, wantErr)
		}
		return
	}
	if got.Period != want.Period || got.n != want.n {
		t.Errorf("%s: period/n = %d/%d, want %d/%d", name, got.Period, got.n, want.Period, want.n)
	}
	for _, c := range []struct {
		what     string
		got, ref []float64
	}{
		{"trend", got.Trend, want.Trend},
		{"seasonal", got.Seasonal, want.Seasonal},
		{"residual", got.Residual, want.Residual},
	} {
		if i, ok := sameBitsXYZ(c.got, c.ref); !ok {
			if i < 0 {
				t.Errorf("%s: %s length %d, want %d", name, c.what, len(c.got), len(c.ref))
				continue
			}
			t.Errorf("%s: %s[%d] = %v (%#x), reference %v (%#x)", name, c.what, i,
				c.got[i], math.Float64bits(c.got[i]), c.ref[i], math.Float64bits(c.ref[i]))
		}
	}
}

// The rewrite must be the same function. Over random series of random lengths
// at random periods, every component must match the reference bit for bit.
func TestClassicalRollingAgreesXYZ(t *testing.T) {
	f := func(seed uint64, rawN uint16, rawP uint8) bool {
		period := 2 + int(rawP)%40
		// Lengths from exactly two cycles up to a few hundred points past it,
		// so that the ragged tail of the last cycle lands at every offset.
		n := 2*period + int(rawN)%300
		r := rand.New(rand.NewPCG(seed|1, 0x9e3779b9))
		x := make([]float64, n)
		for i := range x {
			x[i] = 100 + 0.05*float64(i) +
				10*math.Sin(2*math.Pi*float64(i)/float64(period)) + r.NormFloat64()
		}
		got, err := Classical(x, period)
		if err != nil {
			t.Errorf("n=%d period=%d: %v", n, period, err)
			return false
		}
		want, err := classicalRefXYZ(x, period)
		if err != nil {
			t.Errorf("reference n=%d period=%d: %v", n, period, err)
			return false
		}
		for _, c := range [][2][]float64{
			{got.Trend, want.Trend}, {got.Seasonal, want.Seasonal}, {got.Residual, want.Residual},
		} {
			if i, ok := sameBitsXYZ(c[0], c[1]); !ok {
				t.Errorf("n=%d period=%d: index %d: %v != %v", n, period, i, c[0][i], c[1][i])
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Error(err)
	}
}

// The same again on series that are nothing like a season: pure noise, a flat
// line, a hard ramp, and alternating signs. The phase-count arithmetic that
// replaced the counting loop does not care what the values are, but the
// cancellation in the rolling window does, and a ramp is where it bites.
func TestClassicalRollingShapesXYZ(t *testing.T) {
	shapes := map[string]func(i, n int, r *rand.Rand) float64{
		"noise":      func(i, n int, r *rand.Rand) float64 { return r.NormFloat64() },
		"constant":   func(i, n int, r *rand.Rand) float64 { return 300 },
		"ramp":       func(i, n int, r *rand.Rand) float64 { return 1e6 + 1e-3*float64(i) },
		"steep ramp": func(i, n int, r *rand.Rand) float64 { return float64(i) * 1e9 },
		"alternating": func(i, n int, r *rand.Rand) float64 {
			return float64(1-2*(i%2)) * 1e8
		},
		"step": func(i, n int, r *rand.Rand) float64 {
			if i < n/2 {
				return 10
			}
			return 1e7
		},
		"tiny":  func(i, n int, r *rand.Rand) float64 { return 1e-300 * float64(1+i%7) },
		"huge":  func(i, n int, r *rand.Rand) float64 { return 1e300 * float64(1+i%7) },
		"zeros": func(i, n int, r *rand.Rand) float64 { return 0 },
		"signed zeros": func(i, n int, r *rand.Rand) float64 {
			if i%2 == 0 {
				return math.Copysign(0, -1)
			}
			return 0
		},
	}
	for name, mk := range shapes {
		for _, period := range []int{2, 3, 4, 5, 7, 12, 24, 25, 48} {
			for _, extra := range []int{0, 1, 2, period - 1, period, 3*period + 1} {
				n := 2*period + extra
				r := rand.New(rand.NewPCG(uint64(n), uint64(period)))
				x := make([]float64, n)
				for i := range x {
					x[i] = mk(i, n, r)
				}
				checkSameXYZ(t, name, x, period)
			}
		}
	}
}

// The edges the brief calls out, each one a place where an off-by-one in the
// new phase-count arithmetic or the new reseed countdown would show.
func TestClassicalRollingEdgesXYZ(t *testing.T) {
	// Too short, exactly long enough, and one past: n below two cycles is
	// ErrTooShort from both, n at exactly two cycles must work.
	for _, period := range []int{2, 3, 4, 7, 24, 25} {
		for n := 0; n <= 2*period+2; n++ {
			x := make([]float64, n)
			for i := range x {
				x[i] = 100 + float64(i) + 10*math.Sin(float64(i))
			}
			checkSameXYZ(t, "short", x, period)
		}
	}
	// A period below 2 is an error whatever the series.
	for _, period := range []int{-7, -1, 0, 1} {
		checkSameXYZ(t, "bad period", []float64{1, 2, 3, 4, 5, 6, 7, 8}, period)
	}
	// Period 2 and 3, the shortest even and odd cycles, where h is 1 and the
	// defined range is almost the whole series.
	for _, period := range []int{2, 3} {
		for n := 2 * period; n < 2*period+40; n++ {
			r := rand.New(rand.NewPCG(uint64(n), 3))
			x := make([]float64, n)
			for i := range x {
				x[i] = r.NormFloat64() * 1000
			}
			checkSameXYZ(t, "tiny period", x, period)
		}
	}
}

// NaN and Inf. The fast path skips the copy fill() makes, so every arrangement
// of NaN has to land on the same answer as the reference, which always copies:
// leading NaNs, a trailing NaN, a lone NaN in the middle, every value NaN, and
// infinities, which poison a rolling window far more thoroughly than a NaN
// does and must poison it identically in both.
func TestClassicalRollingNonFiniteXYZ(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	const period = 7
	base := func(n int) []float64 {
		r := rand.New(rand.NewPCG(uint64(n), 11))
		x := make([]float64, n)
		for i := range x {
			x[i] = 100 + 0.05*float64(i) + r.NormFloat64()
		}
		return x
	}
	type mut struct {
		name string
		at   []int
		v    float64
	}
	for _, n := range []int{14, 15, 20, 31, 60} {
		muts := []mut{
			{"leading NaN", []int{0}, nan},
			{"leading NaNs", []int{0, 1, 2, 3, 4, 5, 6, 7}, nan},
			{"trailing NaN", []int{n - 1}, nan},
			{"trailing NaNs", []int{n - 3, n - 2, n - 1}, nan},
			{"interior NaN", []int{n / 2}, nan},
			{"scattered NaN", []int{1, n / 3, n / 2, n - 2}, nan},
			{"+Inf", []int{n / 2}, inf},
			{"-Inf", []int{n / 2}, math.Inf(-1)},
			{"leading Inf", []int{0}, inf},
			{"Inf and NaN", []int{0}, inf},
		}
		for _, m := range muts {
			x := base(n)
			for _, i := range m.at {
				x[i] = m.v
			}
			if m.name == "Inf and NaN" {
				x[n/2] = nan
			}
			checkSameXYZ(t, m.name, x, period)
		}
		// Every value NaN: both must refuse, with the same message.
		all := make([]float64, n)
		for i := range all {
			all[i] = nan
		}
		checkSameXYZ(t, "all NaN", all, period)
	}
}

// Nothing in Classical may write to the series it was given. That was always
// true, but it used to be true by accident — the series was copied first. Now
// the copy is skipped when there is no NaN, so the caller's slice IS the one
// the arithmetic reads, and a stray write would corrupt it.
func TestClassicalDoesNotWriteInputXYZ(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 9))
	x := make([]float64, 400)
	for i := range x {
		x[i] = 100 + 0.05*float64(i) + 10*math.Sin(float64(i)/4) + r.NormFloat64()
	}
	keep := append([]float64(nil), x...)
	if _, err := Classical(x, 24); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x, keep) {
		for i := range x {
			if x[i] != keep[i] {
				t.Fatalf("Classical wrote to its input at index %d: %v -> %v", i, keep[i], x[i])
			}
		}
	}
}

// The three components now share one backing array, which they must not be
// able to reach each other through. Appending to any of them has to reallocate
// rather than overwrite the next one along.
func TestClassicalComponentsDoNotAliasXYZ(t *testing.T) {
	r := rand.New(rand.NewPCG(13, 17))
	x := make([]float64, 200)
	for i := range x {
		x[i] = 50 + r.NormFloat64()
	}
	d, err := Classical(x, 12)
	if err != nil {
		t.Fatal(err)
	}
	seas := append([]float64(nil), d.Seasonal...)
	resid := append([]float64(nil), d.Residual...)
	_ = append(d.Trend, 12345)
	_ = append(d.Seasonal, 12345)
	if i, ok := sameBitsXYZ(d.Seasonal, seas); !ok {
		t.Errorf("appending to Trend reached Seasonal at index %d", i)
	}
	if i, ok := sameBitsXYZ(d.Residual, resid); !ok {
		t.Errorf("appending to Trend or Seasonal reached Residual at index %d", i)
	}
	for _, c := range [][]float64{d.Trend, d.Seasonal, d.Residual} {
		if cap(c) != len(c) {
			t.Errorf("component has cap %d over len %d, so an append writes into its neighbour",
				cap(c), len(c))
		}
	}
}

// The components must still add back up to the series at every index, ends
// included. This is the identity verify_classical.py checks on the Python
// side, asserted here so a change that breaks it fails before it is published.
func TestClassicalSumsToSeriesXYZ(t *testing.T) {
	for _, period := range []int{2, 7, 12, 24, 25, 48} {
		for _, n := range []int{2 * period, 2*period + 1, 300, 4000} {
			if n < 2*period {
				continue
			}
			r := rand.New(rand.NewPCG(uint64(n), uint64(period)))
			x := make([]float64, n)
			for i := range x {
				x[i] = 100 + 0.05*float64(i) +
					10*math.Sin(2*math.Pi*float64(i)/float64(period)) + r.NormFloat64()
			}
			d, err := Classical(x, period)
			if err != nil {
				t.Fatalf("n=%d period=%d: %v", n, period, err)
			}
			worst := 0.0
			for i := range x {
				if e := math.Abs(d.Trend[i] + d.Seasonal[i] + d.Residual[i] - x[i]); e > worst {
					worst = e
				}
			}
			// The three are formed as trend, season, and x−trend−season, so
			// the identity is exact up to the rounding of that one subtraction
			// at a magnitude of a few hundred: a few ulps, far under 1e-10.
			if worst > 1e-10 {
				t.Errorf("n=%d period=%d: components miss the series by %g", n, period, worst)
			}
		}
	}
}
