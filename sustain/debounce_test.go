package sustain

import (
	"errors"
	"testing"
	"time"
)

func mustDebounce(t testing.TB, on, off Delay) Debounce {
	t.Helper()
	d, err := NewDebounce(on, off)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// step is one reading for a Debounce: the condition and its time in seconds.
type step struct {
	cond bool
	at   float64
}

func feedD(d *Debounce, steps ...step) []bool {
	out := make([]bool, len(steps))
	for i, s := range steps {
		out[i] = d.Update(s.cond, ts(s.at))
	}
	return out
}

func TestDebounceOnDelayByTime(t *testing.T) {
	d := mustDebounce(t, Delay{For: 10 * time.Second}, Delay{})
	got := feedD(&d,
		step{true, 0}, step{true, 5}, step{false, 8}, // run broken at 8
		step{true, 9}, step{true, 18}, step{true, 19}, // 9..18 is 9 s: not yet; 19 is 10 s
		step{false, 20}, // off at once: no off delay
	)
	want := []bool{false, false, false, false, false, true, false}
	if !eqBools(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
	if d.Transitions() != 2 || !d.Since().Equal(ts(20)) {
		t.Fatalf("transitions %d since %v", d.Transitions(), d.Since().Sub(t0))
	}
}

func TestDebounceByReadingsAndBoth(t *testing.T) {
	d := mustDebounce(t, Delay{Readings: 3}, Delay{Readings: 2})
	got := feedD(&d,
		step{true, 0}, step{true, 1}, step{false, 2}, // a flicker of two: ignored
		step{true, 3}, step{true, 4}, step{true, 5}, // three in a row: on
		step{false, 6}, step{true, 7}, step{false, 8}, step{false, 9}, // off after two
	)
	want := []bool{false, false, false, false, false, true, true, true, true, false}
	if !eqBools(got, want) {
		t.Fatalf("readings: %v, want %v", got, want)
	}
	// Both set: the later of the two decides.
	d = mustDebounce(t, Delay{For: 2 * time.Second, Readings: 5}, Delay{})
	got = feedD(&d, step{true, 0}, step{true, 10}, step{true, 11}, step{true, 12}, step{true, 13})
	if !eqBools(got, []bool{false, false, false, false, true}) {
		t.Fatalf("both, readings decide: %v", got)
	}
	d = mustDebounce(t, Delay{For: 20 * time.Second, Readings: 2}, Delay{})
	got = feedD(&d, step{true, 0}, step{true, 1}, step{true, 19}, step{true, 20})
	if !eqBools(got, []bool{false, false, false, true}) {
		t.Fatalf("both, time decides: %v", got)
	}
}

// A flicker shorter than the delay never changes the state, however often it
// comes: the claim the type exists for.
func TestDebounceIgnoresFlicker(t *testing.T) {
	d := mustDebounce(t, Delay{For: 3 * time.Second}, Delay{For: 3 * time.Second})
	for i := range 1000 {
		d.Update(i%3 == 1, ts(float64(i))) // true one second in three
	}
	if d.Transitions() != 0 || d.Active() {
		t.Fatalf("%d transitions", d.Transitions())
	}
	for i := 1000; i < 1004; i++ {
		d.Update(true, ts(float64(i)))
	}
	if !d.Active() || !d.Since().Equal(ts(1003)) || d.InState(ts(1010)) != 7*time.Second {
		t.Fatalf("active %v since %v", d.Active(), d.Since().Sub(t0))
	}
}

func TestDebounceZeroValueAndPending(t *testing.T) {
	var d Debounce
	if !d.Update(true, t0) || d.Update(false, ts(1)) || d.Transitions() != 2 {
		t.Fatal("the zero value does not follow its condition")
	}
	d = mustDebounce(t, Delay{Readings: 2}, Delay{Readings: 2})
	if d.Pending() {
		t.Fatal("pending before any reading")
	}
	d.Update(true, t0)
	if !d.Pending() {
		t.Fatal("not pending in a run")
	}
	d.Update(false, ts(1))
	if d.Pending() {
		t.Fatal("pending after the run broke")
	}
	d.Update(true, ts(2))
	d.Update(true, ts(3))
	if d.Pending() || !d.Active() {
		t.Fatal("pending after the change")
	}
	// Out of order and duplicate: skipped, and they do not break the run.
	d.Update(false, ts(4))
	d.Update(true, ts(4))
	d.Update(true, ts(1))
	if d.Skipped() != 2 || !d.Pending() {
		t.Fatalf("skipped %d pending %v", d.Skipped(), d.Pending())
	}
	d.Reset()
	if d != mustDebounce(t, Delay{Readings: 2}, Delay{Readings: 2}) {
		t.Fatalf("after Reset %+v", d)
	}
}

func TestDebounceInvalid(t *testing.T) {
	for _, c := range [][2]Delay{
		{{For: -1}, {}},
		{{Readings: -1}, {}},
		{{}, {For: -time.Second}},
		{{}, {Readings: -2}},
	} {
		if _, err := NewDebounce(c[0], c[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewDebounce(%+v, %+v): %v, want ErrInvalid", c[0], c[1], err)
		}
	}
}
