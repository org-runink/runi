package sustain

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

// The two rules every type shares, as the package doc states them: a reading
// not after the last accepted one, and a non-finite one, are skipped, counted,
// and leave no other trace. Each type is fed a random series with such
// readings mixed in, and a twin is fed only the readings the rule accepts;
// apart from the skip count the two must be identical in every field.
func TestSkippedLeaveNoTrace(t *testing.T) {
	r := rand.New(rand.NewPCG(13, 14))
	for range 200 / raceScale {
		rs := randomReadings(r, 200, -5, 5)
		accX, skipX := accepted(rs, true)
		accC, skipC := accepted(rs, false)
		if skipX == 0 || skipC == 0 {
			t.Fatal("the series has nothing to skip")
		}
		dwell := time.Duration(r.IntN(3)) * time.Second

		h := mustHysteresis(t, High, 2, -1, dwell)
		b := mustBand(t, dwell, Edge{Up: -2, Down: -3}, Edge{Up: 3, Down: 0})
		w := mustNofM(t, Low, 0, 3, 5)
		s := mustSlew(t, 0.5, 2)
		d := mustDebounce(t, Delay{For: 2 * time.Second}, Delay{Readings: 2})
		h2, b2, w2, s2, d2 := h, b, w, s, d
		for _, rd := range rs {
			h.Update(rd.x, rd.at)
			b.Update(rd.x, rd.at)
			w.Update(rd.x, rd.at)
			s.Update(rd.x, rd.at)
			d.Update(rd.cond, rd.at)
		}
		for _, rd := range accX {
			h2.Update(rd.x, rd.at)
			b2.Update(rd.x, rd.at)
			w2.Update(rd.x, rd.at)
			s2.Update(rd.x, rd.at)
		}
		for _, rd := range accC {
			d2.Update(rd.cond, rd.at)
		}
		for name, c := range map[string]struct {
			got, want *clock
			skipped   int64
		}{
			"Hysteresis": {&h.c, &h2.c, skipX},
			"Band":       {&b.c, &b2.c, skipX},
			"NofM":       {&w.c, &w2.c, skipX},
			"Slew":       {&s.c, &s2.c, skipX},
			"Debounce":   {&d.c, &d2.c, skipC},
		} {
			if c.got.skipped != c.skipped || c.want.skipped != 0 {
				t.Fatalf("%s skipped %d (twin %d), want %d", name, c.got.skipped, c.want.skipped, c.skipped)
			}
			c.got.skipped = 0
		}
		for name, pair := range map[string][2]any{
			"Hysteresis": {h, h2}, "Band": {b, b2}, "NofM": {w, w2}, "Slew": {s, s2}, "Debounce": {d, d2},
		} {
			if !reflect.DeepEqual(pair[0], pair[1]) {
				t.Fatalf("%s: skipped readings left a trace:\n%+v\n%+v", name, pair[0], pair[1])
			}
		}
	}
}

// Out-of-order and duplicate times are skipped by every type, even with a
// value that would otherwise change the state; the first reading may have any
// time at all.
func TestOutOfOrder(t *testing.T) {
	h := mustHysteresis(t, High, 1, 0, 0)
	w := mustNofM(t, High, 1, 1, 1)
	b := mustBand(t, 0, Edge{Up: 1, Down: 0})
	d := mustDebounce(t, Delay{}, Delay{})
	s := mustSlew(t, 1, 1)
	early := time.Time{} // the zero time is a valid first reading
	h.Update(0, early)
	w.Update(0, early)
	b.Update(0, early)
	d.Update(false, early)
	s.Update(0, early)
	late := early.Add(time.Second)
	if !h.Update(5, late) || !w.Update(5, late) || b.Update(5, late) != 1 || !d.Update(true, late) || s.Update(5, late) != 1 {
		t.Fatal("a reading after the first did not count")
	}
	// Each of these would turn every state off again, were it accepted.
	for _, at := range []time.Time{late, early, late.Add(-1)} {
		if !h.Update(-5, at) || !w.Update(-5, at) || b.Update(-5, at) != 1 || !d.Update(false, at) || s.Update(-5, at) != 1 {
			t.Fatalf("at %v: a reading not after the last changed the state", at)
		}
	}
	for _, n := range []int64{h.Skipped(), w.Skipped(), b.Skipped(), d.Skipped(), s.Skipped()} {
		if n != 3 {
			t.Fatalf("skipped %d, want 3", n)
		}
	}
}
