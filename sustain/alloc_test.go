package sustain

import (
	"testing"
	"time"
)

// The package doc promises no allocation per update; this is the test of it.
// Each closure changes its reading and moves time forward, so the call is not
// optimised to nothing and the state keeps changing: every closure crosses
// its levels back and forth, so the change path runs as well as the steady
// one.
func TestZeroAllocs(t *testing.T) {
	h := mustHysteresis(t, High, 1, -1, 0)
	hd := mustHysteresis(t, Low, -1, 1, 3*time.Second)
	b := mustBand(t, 0, severities...)
	w := mustNofM(t, High, 0, 2, 64)
	d := mustDebounce(t, Delay{For: time.Second, Readings: 2}, Delay{Readings: 2})
	s := mustSlew(t, 1, 2)
	x, i, at := 0.0, 0, t0
	var sink, limited bool
	var level int
	var dur time.Duration
	next := func() { i++; x = float64(i%5 - 2); at = at.Add(time.Second) }
	for name, f := range map[string]func(){
		"Hysteresis.Update":      func() { next(); sink = h.Update(x, at) },
		"Hysteresis.Update/held": func() { next(); sink = hd.Update(x, at) },
		"Band.Update":            func() { next(); level = b.Update(60+15*x, at) },
		"NofM.Update":            func() { next(); sink = w.Update(x, at) },
		"Debounce.Update":        func() { next(); sink = d.Update(i%6 < 3, at) },
		"Slew.Update":            func() { next(); s.Update(10*x, at); limited = limited || s.Limited() },
		"InState":                func() { next(); dur = h.InState(at) + b.InState(at) + w.InState(at) + d.InState(at) },
	} {
		if n := testing.AllocsPerRun(1000, f); n != 0 {
			t.Errorf("%s: %v allocations per call, want 0", name, n)
		}
	}
	_, _, _ = sink, level, dur
	if h.Transitions() == 0 || hd.Held() == 0 || b.Transitions() == 0 || w.Transitions() == 0 || d.Transitions() == 0 || !limited {
		t.Fatal("a closure never changed its state; the test misses the change path")
	}
	if n := testing.AllocsPerRun(1000, func() { h.Reset(); b.Reset(); w.Reset(); d.Reset(); s.Reset() }); n != 0 {
		t.Errorf("Reset: %v allocations per call, want 0", n)
	}
}

// A check that cannot fail is worth nothing: the same measurement does see
// an allocation when there is one. NewBand copies its edges, which is the one
// allocation in this package, and it is caught.
func TestZeroAllocsCanFail(t *testing.T) {
	var sink Band
	if n := testing.AllocsPerRun(100, func() { sink, _ = NewBand(0, severities...) }); n == 0 {
		t.Fatal("AllocsPerRun reported 0 for NewBand, which allocates")
	}
	_ = sink
}

func BenchmarkHysteresisUpdate(b *testing.B) {
	b.ReportAllocs()
	h := mustHysteresis(b, High, 1, -1, time.Second)
	at := t0
	for i := range b.N {
		at = at.Add(time.Second)
		h.Update(float64(i%5-2), at)
	}
}

func BenchmarkBandUpdate(b *testing.B) {
	b.ReportAllocs()
	bd := mustBand(b, 0, severities...)
	at := t0
	for i := range b.N {
		at = at.Add(time.Second)
		bd.Update(float64(i%50+50), at)
	}
}

func BenchmarkNofMUpdate(b *testing.B) {
	b.ReportAllocs()
	w := mustNofM(b, High, 0.5, 3, 5)
	at := t0
	for i := range b.N {
		at = at.Add(time.Second)
		w.Update(float64(i&1), at)
	}
}

func BenchmarkDebounceUpdate(b *testing.B) {
	b.ReportAllocs()
	d := mustDebounce(b, Delay{For: 2 * time.Second}, Delay{Readings: 2})
	at := t0
	for i := range b.N {
		at = at.Add(time.Second)
		d.Update(i%7 < 4, at)
	}
}

func BenchmarkSlewUpdate(b *testing.B) {
	b.ReportAllocs()
	s := mustSlew(b, 1, 2)
	at := t0
	for i := range b.N {
		at = at.Add(time.Second)
		s.Update(float64(i%10), at)
	}
}
