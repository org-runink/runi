package season

import (
	"math"
	"math/rand/v2"
	"testing"
)

// weekly is a clean series: a straight trend plus one fixed weekly pattern.
func weekly(pattern []float64, n int) []float64 {
	x := make([]float64, n)
	for i := range x {
		x[i] = 100 + 0.5*float64(i) + pattern[i%len(pattern)]
	}
	return x
}

// A season with a sharp feature that recurs every cycle -- two weekend days far
// below the rest -- is the season, not two outliers a week. Decompose used to
// clean the series against each point's neighbours in TIME before fitting the
// season, which replaced both weekend days every week, and returned a season of
// [-1.7 -7.1 -1.4 1.3 4.7 7.8 -3.6] for this pattern with residuals of about -12
// on the dip days and three invented changepoints. Three harmonics fit a
// seven-day season exactly, so the right answer is the pattern itself.
func TestSharpRecurringSeasonIsKept(t *testing.T) {
	pattern := []float64{-12, -4, 2, 5, 8, 14, -13}
	x := weekly(pattern, 56)
	for _, p := range []int{7, 0} {
		d, err := Decompose(x, Options{Period: p, Harmonics: 3})
		if err != nil {
			t.Fatal(err)
		}
		if d.Period != 7 || len(d.Changepoints) != 0 {
			t.Fatalf("period opt %d: Period=%d Changepoints=%v, want 7 and none", p, d.Period, d.Changepoints)
		}
		mean := 0.0
		for _, v := range pattern {
			mean += v / 7
		}
		for i, want := range pattern {
			if got := d.Seasonal[i]; math.Abs(got-(want-mean)) > 1e-9 {
				t.Fatalf("period opt %d: season[%d] = %.6f, want %.6f", p, i, got, want-mean)
			}
		}
		for i, r := range d.Residual {
			if math.Abs(r) > 1e-9 {
				t.Fatalf("period opt %d: residual[%d] = %g, want 0 on a clean series", p, i, r)
			}
		}
	}
}

// The same, with noise and one bad reading: the spike must still be cleaned out
// of the season fit and survive in the residual, while the dip days stay.
func TestSharpSeasonWithNoiseAndASpike(t *testing.T) {
	pattern := []float64{-12, -4, 2, 5, 8, 14, -13}
	x := weekly(pattern, 91)
	r := rand.New(rand.NewPCG(3, 9))
	for i := range x {
		x[i] += r.NormFloat64() * 0.5
	}
	x[40] += 80
	d, err := Decompose(x, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Period != 7 || len(d.Changepoints) != 0 {
		t.Fatalf("Period=%d Changepoints=%v, want 7 and none", d.Period, d.Changepoints)
	}
	mean := 0.0
	for _, v := range pattern {
		mean += v / 7
	}
	for i, want := range pattern {
		if got := d.Seasonal[i]; math.Abs(got-(want-mean)) > 0.5 {
			t.Errorf("season[%d] = %.2f, want %.2f within the noise", i, got, want-mean)
		}
	}
	if d.Residual[40] < 70 {
		t.Errorf("the spike left the residual: residual[40] = %.1f", d.Residual[40])
	}
}

// A clean series has no breaks, and must not be given any. A least-squares
// first line through a seasonal series takes part of the season (slope 0.508
// here, for a true 0.5), the season fitted after it carries the error as a
// small weekly ripple, and on noise-free data the break search's price is zero,
// so it bought three breaks to explain the ripple: [7 14 21].
func TestCleanSeasonGetsNoPhantomBreaks(t *testing.T) {
	d, err := Decompose(weekly([]float64{-8, -2, 3, 5, 6, 4, -8}, 56), Options{Period: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Changepoints) != 0 {
		t.Fatalf("Changepoints = %v on a straight trend, want none", d.Changepoints)
	}
}

// phaseLine's slope is the trend's, whatever the season and despite a spike.
func TestPhaseLineIgnoresSeasonAndSpike(t *testing.T) {
	x := weekly([]float64{-12, -4, 2, 5, 8, 14, -13}, 91)
	if l, _ := phaseLine(x, 7); math.Abs(l[0].b-0.5) > 1e-12 {
		t.Fatalf("clean: slope %.12f, want 0.5", l[0].b)
	}
	x[40] += 1000
	if l, _ := phaseLine(x, 7); math.Abs(l[0].b-0.5) > 1e-12 {
		t.Fatalf("one spike moved the slope to %.12f", l[0].b)
	}
}

func TestPhaseHampel(t *testing.T) {
	t.Run("fewer than three cycles are left alone", func(t *testing.T) {
		v := []float64{0, 0, 0, 50, 0, 0}
		if got := phaseHampel(v, 3, lagSigma(v, 3)); &got[0] != &v[0] {
			t.Fatal("a two-cycle series was filtered")
		}
	})
	t.Run("a zero scale disables the filter", func(t *testing.T) {
		v := weekly([]float64{-12, -4, 2, 5, 8, 14, -13}, 28)
		for i := range v {
			v[i] -= 0.5 * float64(i) // exactly periodic: every lag-7 difference is 0
		}
		if got := phaseHampel(v, 7, lagSigma(v, 7)); &got[0] != &v[0] {
			t.Fatal("an exactly periodic series was filtered")
		}
	})
	t.Run("an even number of same-phase points takes the middle two", func(t *testing.T) {
		// Period 2, eight points: each phase has four, so the window is four
		// wide and its median is the mean of the middle two. Noise on the
		// other phase keeps the scale from being zero.
		v := []float64{10, 1, 11, 4, 12, 2, 90, 6}
		got := phaseHampel(v, 2, lagSigma(v, 2))
		if got[6] != 11.5 {
			t.Fatalf("spike replaced by %v, want the mean of the middle two, 11.5", got[6])
		}
		for i, w := range []float64{10, 1, 11, 4, 12, 2, 0, 6} {
			if i != 6 && got[i] != w {
				t.Fatalf("point %d changed from %v to %v", i, w, got[i])
			}
		}
	})
}
