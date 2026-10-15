package sustain

import (
	"errors"
	"math"
	"testing"
)

func mustNofM(t testing.TB, dir Direction, level float64, need, size int) NofM {
	t.Helper()
	w, err := NewNofM(dir, level, need, size)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestNofMKnownAnswer(t *testing.T) {
	// Two of the last three at or above 10.
	w := mustNofM(t, High, 10, 2, 3)
	var got []bool
	for i, x := range []float64{12, 5, 10, 3, 4, 11, 2, 15} {
		got = append(got, w.Update(x, ts(float64(i))))
	}
	want := []bool{false, false, true, false, false, false, false, true}
	if !eqBools(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
	if w.Count() != 2 || w.Len() != 3 || w.Need() != 2 || w.Size() != 3 || w.Transitions() != 3 {
		t.Fatalf("count %d len %d need %d size %d transitions %d", w.Count(), w.Len(), w.Need(), w.Size(), w.Transitions())
	}
	// Low: three of four at or below 1.
	l := mustNofM(t, Low, 1, 3, 4)
	got = got[:0]
	for i, x := range []float64{1, 2, 0, 1.5, -1, 1, 7} {
		got = append(got, l.Update(x, ts(float64(i))))
	}
	want = []bool{false, false, false, false, false, true, false}
	if !eqBools(got, want) {
		t.Fatalf("Low: %v, want %v", got, want)
	}
}

// The widest window: 64 readings, all bits in use, and the oldest falls out
// exactly when the 65th arrives.
func TestNofMSixtyFour(t *testing.T) {
	w := mustNofM(t, High, 1, 64, 64)
	for i := range 64 {
		if w.Update(1, ts(float64(i))) != (i == 63) {
			t.Fatalf("reading %d: wrong state", i)
		}
	}
	if w.Count() != 64 || w.Len() != 64 {
		t.Fatalf("count %d len %d", w.Count(), w.Len())
	}
	if w.Update(0, ts(64)) || w.Count() != 63 {
		t.Fatalf("after one miss: count %d", w.Count())
	}
	w.Reset()
	if w != mustNofM(t, High, 1, 64, 64) {
		t.Fatalf("after Reset %+v", w)
	}
}

func TestNofMSkips(t *testing.T) {
	w := mustNofM(t, High, 0, 1, 2)
	w.Update(1, ts(1))
	w.Update(math.NaN(), ts(2))
	w.Update(math.Inf(1), ts(3))
	w.Update(1, ts(1)) // duplicate time
	if w.Len() != 1 || w.Skipped() != 3 || !w.Active() || !w.Since().Equal(ts(1)) || w.InState(ts(3)) != 2e9 {
		t.Fatalf("len %d skipped %d", w.Len(), w.Skipped())
	}
}

func TestNofMInvalid(t *testing.T) {
	for _, c := range []struct {
		dir        Direction
		level      float64
		need, size int
	}{
		{0, 0, 1, 1},
		{High, math.NaN(), 1, 1},
		{Low, math.Inf(-1), 1, 1},
		{High, 0, 1, 0},
		{High, 0, 1, 65},
		{High, 0, 0, 3},
		{High, 0, 4, 3},
	} {
		if _, err := NewNofM(c.dir, c.level, c.need, c.size); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewNofM(%+v): %v, want ErrInvalid", c, err)
		}
	}
}
