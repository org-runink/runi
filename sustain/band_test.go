package sustain

import (
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

// normal / warn / critical: warn at ≥ 70 until ≤ 65, critical at ≥ 90 until
// ≤ 85.
var severities = []Edge{{Up: 70, Down: 65}, {Up: 90, Down: 85}}

func mustBand(t testing.TB, dwell time.Duration, edges ...Edge) Band {
	t.Helper()
	b, err := NewBand(dwell, edges...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBandKnownAnswer(t *testing.T) {
	b := mustBand(t, 0, severities...)
	var got []int
	for i, x := range []float64{50, 69, 70, 66, 65, 95, 86, 85, 64, 100, 20} {
		got = append(got, b.Update(x, ts(float64(i))))
	}
	want := []int{0, 0, 1, 1, 0, 2, 2, 1, 0, 2, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
	if b.Levels() != 3 || b.Level() != 0 || b.Transitions() != 7 || !b.Since().Equal(ts(10)) || b.InState(ts(12)) != 2*time.Second {
		t.Fatalf("levels %d level %d transitions %d since %v", b.Levels(), b.Level(), b.Transitions(), b.Since().Sub(t0))
	}
}

// Below / inside / above a band whose middle is the normal level.
func TestBandInside(t *testing.T) {
	b := mustBand(t, 0, Edge{Up: 10, Down: 9}, Edge{Up: 21, Down: 20})
	for i, c := range []struct {
		x    float64
		want int
	}{{5, 0}, {9.5, 0}, {10, 1}, {9.5, 1}, {9, 0}, {15, 1}, {20.5, 1}, {21, 2}, {20.5, 2}, {20, 1}} {
		if got := b.Update(c.x, ts(float64(i))); got != c.want {
			t.Fatalf("%v: level %d, want %d", c.x, got, c.want)
		}
	}
}

// Noise below every edge's deadband, centred anywhere, never reverses the
// level: from level 0 it may climb (a mean near the critical edge passes
// warning on the way), but it never comes back down past a level it left.
// One threshold per edge, at the midpoint of each deadband, reverses
// constantly on the same series.
func TestBandNoFlapUnderNoise(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	flaps := 0
	for range 2000 / raceScale {
		b := mustBand(t, 0, severities...)
		mean := 55 + 45*r.Float64()
		amp := 0.4999 * 5 * r.Float64()
		prev, naivePrev, naiveDir, dir := 0, -1, 0, 0
		for i := range 500 {
			x := mean + amp*(2*r.Float64()-1)
			l := b.Update(x, ts(float64(i)))
			if l != prev {
				d := 1
				if l < prev {
					d = -1
				}
				if dir != 0 && d != dir {
					t.Fatalf("mean %v amp %v: level went back to %d", mean, amp, l)
				}
				dir = d
			}
			prev = l
			naive := b2i(x >= 67.5) + b2i(x >= 87.5)
			if naivePrev >= 0 && naive != naivePrev {
				d := 1
				if naive < naivePrev {
					d = -1
				}
				if naiveDir != 0 && d != naiveDir {
					flaps++
				}
				naiveDir = d
			}
			naivePrev = naive
		}
	}
	if flaps < 1000 {
		t.Fatalf("plain thresholds reversed only %d times; the noise does not test the claim", flaps)
	}
}

func TestBandDwell(t *testing.T) {
	b := mustBand(t, 5*time.Second, severities...)
	got := []int{
		b.Update(95, ts(0)), // 0 → 2 at once: the first change is not delayed
		b.Update(50, ts(1)), // held
		b.Update(80, ts(4)), // held
		b.Update(80, ts(5)), // 2 → 1: the dwell has passed
		b.Update(50, ts(6)), // held
		b.Update(68, ts(7)), // inside warn's deadband: nothing to hold
		b.Update(50, ts(10)),
	}
	if want := []int{2, 2, 2, 1, 1, 1, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
	if b.Held() != 3 || b.Transitions() != 3 {
		t.Fatalf("held %d transitions %d", b.Held(), b.Transitions())
	}
	b.Update(math.NaN(), ts(11))
	b.Update(100, ts(9))
	if b.Skipped() != 2 || b.Level() != 0 {
		t.Fatalf("skipped %d level %d", b.Skipped(), b.Level())
	}
	b.Reset()
	if !reflect.DeepEqual(b, mustBand(t, 5*time.Second, severities...)) {
		t.Fatalf("after Reset %+v", b)
	}
}

// NewBand copies its edges, and a copy of a Band is independent of the
// original.
func TestBandCopies(t *testing.T) {
	edges := []Edge{{Up: 1, Down: 0}}
	b := mustBand(t, 0, edges...)
	edges[0].Up = 100
	if b.Update(1, t0) != 1 {
		t.Fatal("changing the caller's slice changed the Band")
	}
	c := b
	c.Update(0, ts(1))
	if b.Level() != 1 || c.Level() != 0 {
		t.Fatalf("copy not independent: %d %d", b.Level(), c.Level())
	}
	var z Band
	if z.Update(1e9, t0) != 0 || z.Levels() != 1 {
		t.Fatal("the zero Band left level 0")
	}
}

func TestBandInvalid(t *testing.T) {
	nan := math.NaN()
	for _, c := range []struct {
		dwell time.Duration
		edges []Edge
	}{
		{-1, []Edge{{1, 0}}},
		{0, nil},
		{0, []Edge{{nan, 0}}},
		{0, []Edge{{1, math.Inf(-1)}}},
		{0, []Edge{{1, 1}}},
		{0, []Edge{{0, 1}}},
		{0, []Edge{{2, 0}, {2, 1}}}, // Up not increasing
		{0, []Edge{{2, 1}, {3, 1}}}, // Down not increasing
		{0, []Edge{{2, 1}, {3, 2}, {2.5, 2.2}}},
	} {
		if _, err := NewBand(c.dwell, c.edges...); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewBand(%v, %v): %v, want ErrInvalid", c.dwell, c.edges, err)
		}
	}
	// Overlapping deadbands are allowed: only the order is required.
	mustBand(t, 0, Edge{Up: 5, Down: 0}, Edge{Up: 6, Down: 1})
}
