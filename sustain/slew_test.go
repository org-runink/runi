package sustain

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

func mustSlew(t testing.TB, rise, fall float64) Slew {
	t.Helper()
	s, err := NewSlew(rise, fall)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSlewKnownAnswer(t *testing.T) {
	s := mustSlew(t, 2, 5) // up 2/s, down 5/s
	if !math.IsNaN(s.Value()) {
		t.Fatal("Value before the first reading is not NaN")
	}
	for _, c := range []struct {
		x, at, want float64
		limited     bool
	}{
		{10, 0, 10, false},  // first reading: taken as is
		{20, 1, 12, true},   // +2 in 1 s
		{20, 3.5, 17, true}, // +5 in 2.5 s
		{18, 4, 18, false},  // within reach (up 1, allowed 1)
		{0, 5, 13, true},    // −5 in 1 s
		{13, 6, 13, false},  // no move needed
		{-10, 10, -7, true}, // −20 in 4 s
	} {
		if got := s.Update(c.x, ts(c.at)); got != c.want || s.Limited() != c.limited {
			t.Fatalf("Update(%v, %vs) = %v limited %v, want %v %v", c.x, c.at, got, s.Limited(), c.want, c.limited)
		}
	}
}

// Rates of +Inf remove the limit in their direction.
func TestSlewInfiniteRates(t *testing.T) {
	s := mustSlew(t, math.Inf(1), 1) // fast attack, slow decay
	s.Update(0, t0)
	if s.Update(1e300, ts(1e-9)) != 1e300 || s.Limited() {
		t.Fatal("an infinite rise rate limited the rise")
	}
	// A fall of about 1 is far below the spacing of float64 at 1e300, so it
	// rounds to no movement, as the doc says; the output is still limited.
	if s.Update(0, ts(1)) != 1e300 || !s.Limited() {
		t.Fatalf("fall: %v limited %v", s.Value(), s.Limited())
	}
	s = mustSlew(t, math.MaxFloat64, math.MaxFloat64) // rate·Δt overflows to +Inf
	s.Update(-1e308, t0)
	if s.Update(1e308, ts(10)) != 1e308 || s.Update(-1e308, ts(20)) != -1e308 {
		t.Fatal("an overflowing step did not reach the input")
	}
}

// The claims in Slew's doc, over random rates, inputs and irregular times:
// every move is within rate·Δt as one addition rounds it, the output never
// passes the input, it moves the whole way when it can, and Limited says
// which.
func TestSlewBound(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	limited := 0
	for range 500 / raceScale {
		rise, fall := math.Exp(6*r.Float64()-3), math.Exp(6*r.Float64()-3)
		s := mustSlew(t, rise, fall)
		at := t0
		y := s.Update(100*r.NormFloat64(), at)
		for range 300 {
			dt := time.Duration(r.ExpFloat64() * float64(time.Second))
			if dt == 0 {
				dt = 1
			}
			at = at.Add(dt)
			x := y + 10*r.NormFloat64()
			got := s.Update(x, at)
			up, down := y+rise*dt.Seconds(), y-fall*dt.Seconds()
			switch {
			case got > up || got < down:
				t.Fatalf("y %v → %v over %v: outside [%v, %v]", y, got, dt, down, up)
			case (got-y)*(x-got) < 0:
				t.Fatalf("y %v → %v passed the input %v", y, got, x)
			case s.Limited() != (got != x):
				t.Fatalf("limited %v with output %v and input %v", s.Limited(), got, x)
			case s.Limited() && got != up && got != down:
				t.Fatalf("limited to %v, not the bound %v or %v", got, down, up)
			}
			limited += b2i(s.Limited())
			y = got
		}
	}
	if limited < 1000 {
		t.Fatalf("only %d limited moves; the test proves little", limited)
	}
}

// A skipped reading's interval is not lost: the next accepted reading's Δt
// covers it.
func TestSlewSkips(t *testing.T) {
	s := mustSlew(t, 1, 1)
	s.Update(0, t0)
	if s.Update(math.NaN(), ts(1)) != 0 || s.Update(5, t0) != 0 || s.Update(5, ts(-1)) != 0 {
		t.Fatal("a skipped reading moved the output")
	}
	if s.Update(5, ts(3)) != 3 || s.Skipped() != 3 {
		t.Fatalf("value %v skipped %d", s.Value(), s.Skipped())
	}
	s.Reset()
	if s != mustSlew(t, 1, 1) {
		t.Fatalf("after Reset %+v", s)
	}
}

func TestSlewInvalid(t *testing.T) {
	for _, c := range [][2]float64{{0, 1}, {-1, 1}, {math.NaN(), 1}, {1, 0}, {1, math.NaN()}, {1, math.Inf(-1)}} {
		if _, err := NewSlew(c[0], c[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewSlew(%v, %v): %v, want ErrInvalid", c[0], c[1], err)
		}
	}
}
