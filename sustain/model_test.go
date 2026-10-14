package sustain

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ts is t0 plus s seconds.
func ts(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }

// reading is one offered input: a value (or, for Debounce, a condition) and
// the time it was taken.
type reading struct {
	x    float64
	cond bool
	at   time.Time
}

// accepted is the package's rule for which readings count, written once,
// naively, over the whole history: the first finite reading, and after it
// every finite reading strictly later than the last one accepted. checkValue
// is false for Debounce, which has no values.
func accepted(rs []reading, checkValue bool) (out []reading, skipped int64) {
	for _, r := range rs {
		if checkValue && (math.IsNaN(r.x) || math.IsInf(r.x, 0)) {
			skipped++
			continue
		}
		if len(out) > 0 && !r.at.After(out[len(out)-1].at) {
			skipped++
			continue
		}
		out = append(out, r)
	}
	return out, skipped
}

// randomReadings draws n readings. Values are small integers, so readings
// land exactly on levels often; one in twenty is NaN or ±Inf; time steps
// range over −2 s to +5 s, so duplicates and out-of-order times are common.
func randomReadings(r *rand.Rand, n int, lo, hi int) []reading {
	rs := make([]reading, n)
	at := t0
	for i := range rs {
		at = at.Add(time.Duration(r.IntN(8)-2) * time.Second)
		x := float64(lo + r.IntN(hi-lo+1))
		switch r.IntN(20) {
		case 0:
			x = math.NaN()
		case 1:
			x = math.Inf(1 - 2*r.IntN(2))
		}
		rs[i] = reading{x: x, cond: r.IntN(3) == 0, at: at}
	}
	return rs
}

// stateRef is what every state-keeping type reports after a reading.
type stateRef struct {
	level       int // 0/1 for the on/off types
	since       time.Time
	transitions int64
}

// trace turns a level per accepted reading into the full reference: since
// and transitions follow from the levels alone.
func trace(acc []reading, levels []int) []stateRef {
	out := make([]stateRef, len(levels))
	prev, since, n := 0, time.Time{}, int64(0)
	for i, l := range levels {
		if i == 0 {
			since = acc[0].at
		}
		if l != prev {
			since, n = acc[i].at, n+1
		}
		prev = l
		out[i] = stateRef{l, since, n}
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// refHysteresisDecisive is the no-dwell Hysteresis written a different way:
// the state after a reading is set by the latest reading at or past either
// level, on if that reading was at or past set, off if none has been.
func refHysteresisDecisive(dir Direction, set, clear float64, acc []reading) []int {
	s := dir.sign()
	out := make([]int, len(acc))
	for i := range acc {
		for j := i; j >= 0; j-- {
			if x := s * acc[j].x; x >= s*set {
				out[i] = 1
				break
			} else if x <= s*clear {
				break
			}
		}
	}
	return out
}

// refBand recomputes a Band reading by reading: every edge is a trigger, on
// for a reading at or above Up, off at or below Down, and otherwise as the
// current level says (the edges below it on). The new level is the number of
// triggers on, taken only if the dwell since the last change has passed.
// With dwell 0 this is a set of independent triggers. A Hysteresis is a Band
// of one edge in sign-adjusted units, so it uses this too.
func refBand(edges []Edge, dwell time.Duration, acc []reading) []int {
	out := make([]int, len(acc))
	level := 0
	var changes []time.Time
	for i, r := range acc {
		k := 0
		for e, ed := range edges {
			on := e < level
			if r.x >= ed.Up {
				on = true
			} else if r.x <= ed.Down {
				on = false
			}
			k += b2i(on)
		}
		if k != level && (len(changes) == 0 || r.at.Sub(changes[len(changes)-1]) >= dwell) {
			level = k
			changes = append(changes, r.at)
		}
		out[i] = level
	}
	return out
}

func refHysteresis(dir Direction, set, clear float64, dwell time.Duration, acc []reading) []int {
	s := dir.sign()
	flipped := make([]reading, len(acc))
	for i, r := range acc {
		flipped[i] = reading{x: s * r.x, at: r.at}
	}
	return refBand([]Edge{{Up: s * set, Down: s * clear}}, dwell, flipped)
}

// refNofM counts hits among the last m accepted readings directly.
func refNofM(dir Direction, level float64, n, m int, acc []reading) []int {
	s := dir.sign()
	out := make([]int, len(acc))
	for i := range acc {
		c := 0
		for j := max(0, i-m+1); j <= i; j++ {
			c += b2i(s*acc[j].x >= s*level)
		}
		out[i] = b2i(c >= n)
	}
	return out
}

// refDebounce scans back from each reading over the run of readings that
// disagree with the current state, and changes the state when the run meets
// the delay for that direction.
func refDebounce(on, off Delay, acc []reading) []int {
	out := make([]int, len(acc))
	state := false
	for i, r := range acc {
		need := on
		if state {
			need = off
		}
		j := i
		for j >= 0 && acc[j].cond != state {
			j--
		}
		run := i - j
		if run > 0 && run >= need.Readings && r.at.Sub(acc[j+1].at) >= need.For {
			state = !state
		}
		out[i] = b2i(state)
	}
	return out
}

// refSlew is the limiter as a clamp, with the same one-addition rounding.
func refSlew(rise, fall float64, acc []reading) []float64 {
	out := make([]float64, len(acc))
	for i, r := range acc {
		if i == 0 {
			out[i] = r.x
			continue
		}
		dt := r.at.Sub(acc[i-1].at).Seconds()
		y := out[i-1]
		out[i] = math.Max(math.Min(r.x, y+rise*dt), y-fall*dt)
	}
	return out
}

func checkState(t *testing.T, name string, i int, level int, since time.Time, n int64, want stateRef) {
	t.Helper()
	if level != want.level || !since.Equal(want.since) || n != want.transitions {
		t.Fatalf("%s reading %d: level %d since %v transitions %d, want %+v", name, i, level, since.Sub(t0), n, want)
	}
}

// Every type against its reference, over random parameters and random
// readings with duplicates, out-of-order times and non-finite values mixed
// in. The references see only the accepted readings, filtered by the naive
// rule above, so this also tests the skipping rules.
func TestModels(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for round := range 400 / raceScale {
		rs := randomReadings(r, 300, -6, 6)
		accX, skipX := accepted(rs, true)
		accC, skipC := accepted(rs, false)
		dwell := time.Duration(r.IntN(4)) * 2 * time.Second
		dir := Direction(1 + r.IntN(2))

		// Hysteresis.
		a, b := float64(r.IntN(9)-4), float64(r.IntN(9)-4)
		if a == b {
			b++
		}
		set, clear := max(a, b), min(a, b)
		if dir == Low {
			set, clear = clear, set
		}
		h, err := NewHysteresis(dir, set, clear, dwell)
		if err != nil {
			t.Fatal(err)
		}
		want := trace(accX, refHysteresis(dir, set, clear, dwell, accX))
		k := 0
		for _, rd := range rs {
			before := h.Skipped()
			on := h.Update(rd.x, rd.at)
			if h.Skipped() > before {
				continue
			}
			checkState(t, "Hysteresis", k, b2i(on), h.Since(), h.Transitions(), want[k])
			k++
		}
		if h.Skipped() != skipX {
			t.Fatalf("round %d: Hysteresis skipped %d, want %d", round, h.Skipped(), skipX)
		}
		if dwell == 0 {
			dec := refHysteresisDecisive(dir, set, clear, accX)
			for i := range dec {
				if dec[i] != want[i].level {
					t.Fatalf("round %d: the two Hysteresis references disagree at %d", round, i)
				}
			}
		}

		// Band, two or three edges.
		edges := []Edge{{Up: -2, Down: -4}, {Up: 2, Down: 0}}
		if r.IntN(2) == 0 {
			edges = []Edge{{Up: -3, Down: -4}, {Up: 1, Down: -2}, {Up: 5, Down: 1.5}}
		}
		bd, err := NewBand(dwell, edges...)
		if err != nil {
			t.Fatal(err)
		}
		want = trace(accX, refBand(edges, dwell, accX))
		k = 0
		for _, rd := range rs {
			before := bd.Skipped()
			l := bd.Update(rd.x, rd.at)
			if bd.Skipped() > before {
				continue
			}
			checkState(t, "Band", k, l, bd.Since(), bd.Transitions(), want[k])
			k++
		}
		if bd.Skipped() != skipX {
			t.Fatalf("round %d: Band skipped %d, want %d", round, bd.Skipped(), skipX)
		}

		// NofM.
		m := 1 + r.IntN(MaxM)
		if r.IntN(4) == 0 {
			m = 1 + r.IntN(6)
		}
		n := 1 + r.IntN(m)
		level := float64(r.IntN(9) - 4)
		w, err := NewNofM(dir, level, n, m)
		if err != nil {
			t.Fatal(err)
		}
		want = trace(accX, refNofM(dir, level, n, m, accX))
		k = 0
		for _, rd := range rs {
			before := w.Skipped()
			on := w.Update(rd.x, rd.at)
			if w.Skipped() > before {
				continue
			}
			checkState(t, "NofM", k, b2i(on), w.Since(), w.Transitions(), want[k])
			if w.Len() != min(k+1, m) {
				t.Fatalf("NofM Len %d after %d readings, m %d", w.Len(), k+1, m)
			}
			k++
		}
		if w.Skipped() != skipX {
			t.Fatalf("round %d: NofM skipped %d, want %d", round, w.Skipped(), skipX)
		}

		// Debounce.
		on := Delay{For: time.Duration(r.IntN(4)) * 3 * time.Second, Readings: r.IntN(4)}
		off := Delay{For: time.Duration(r.IntN(4)) * 3 * time.Second, Readings: r.IntN(4)}
		d, err := NewDebounce(on, off)
		if err != nil {
			t.Fatal(err)
		}
		want = trace(accC, refDebounce(on, off, accC))
		k = 0
		for _, rd := range rs {
			before := d.Skipped()
			st := d.Update(rd.cond, rd.at)
			if d.Skipped() > before {
				continue
			}
			checkState(t, "Debounce", k, b2i(st), d.Since(), d.Transitions(), want[k])
			k++
		}
		if d.Skipped() != skipC {
			t.Fatalf("round %d: Debounce skipped %d, want %d", round, d.Skipped(), skipC)
		}

		// Slew.
		rise, fall := 0.25*float64(1+r.IntN(8)), 0.25*float64(1+r.IntN(8))
		s, err := NewSlew(rise, fall)
		if err != nil {
			t.Fatal(err)
		}
		ws := refSlew(rise, fall, accX)
		k = 0
		for _, rd := range rs {
			before := s.Skipped()
			y := s.Update(rd.x, rd.at)
			if s.Skipped() > before {
				continue
			}
			if y != ws[k] {
				t.Fatalf("round %d: Slew reading %d = %v, want %v", round, k, y, ws[k])
			}
			k++
		}
		if s.Skipped() != skipX {
			t.Fatalf("round %d: Slew skipped %d, want %d", round, s.Skipped(), skipX)
		}
	}
}

// NofM against the reference for every bit pattern of up to 12 readings and
// every n and m that fit: the count is exact, not merely close.
func TestNofMExhaustive(t *testing.T) {
	const length = 12
	for m := 1; m <= length; m++ {
		for n := 1; n <= m; n++ {
			for pat := range 1 << length {
				w, err := NewNofM(High, 1, n, m)
				if err != nil {
					t.Fatal(err)
				}
				var hist uint64
				for i := range length {
					bit := uint64(pat>>i) & 1
					got := w.Update(float64(bit), ts(float64(i)))
					hist = hist<<1 | bit
					c := bits.OnesCount64(hist & (1<<m - 1))
					if got != (c >= n) || w.Count() != c {
						t.Fatalf("n %d m %d pattern %b after %d: %v count %d, want count %d", n, m, pat, i+1, got, w.Count(), c)
					}
				}
			}
		}
	}
}
