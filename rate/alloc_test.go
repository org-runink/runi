package rate

import (
	"strconv"
	"testing"
	"time"
)

// The package doc promises no allocation per update; this is the test of it.
// Each closure feeds a changing value so the call is not optimised to nothing.
func TestZeroAllocs(t *testing.T) {
	c, _ := NewCounter(32)
	w, _ := NewWindow(64)
	var p Peak
	var iv Intervals
	x, total, at := 0.0, uint64(0), t0
	for name, f := range map[string]func(){
		"Counter.Update": func() { total = (total + 1<<30) & (1<<32 - 1); at = at.Add(time.Second); c.Update(total, at) },
		"Window.Add":     func() { x = -x + 0.5; w.Add(x) },
		"Window.Reset":   func() { w.Reset() },
		"Peak.Add":       func() { x++; at = at.Add(time.Second); p.Add(x, at) },
		"Intervals.Add":  func() { at = at.Add(time.Second); iv.Add(at) },
	} {
		if n := testing.AllocsPerRun(1000, f); n != 0 {
			t.Errorf("%s: %v allocations per call, want 0", name, n)
		}
	}
	if c.Wraps() == 0 {
		t.Fatal("the Counter closure never exercised the wrap path")
	}
}

// A check that cannot fail is worth nothing: the same measurement does see
// an allocation when there is one.
func TestZeroAllocsCanFail(t *testing.T) {
	var sink []float64
	if n := testing.AllocsPerRun(100, func() { sink = make([]float64, 8) }); n == 0 {
		t.Fatal("AllocsPerRun reported 0 for a call that allocates")
	}
	_ = sink
}

func BenchmarkCounterUpdate(b *testing.B) {
	b.ReportAllocs()
	c, _ := NewCounter(32)
	at := t0
	for i := range b.N {
		at = at.Add(time.Second)
		c.Update(uint64(i)*1_000_003&(1<<32-1), at)
	}
}

func BenchmarkWindowAdd(b *testing.B) {
	for _, size := range []int{8, 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()
			w, _ := NewWindow(size)
			for i := range b.N {
				w.Add(float64(i*7919%1000) - 500)
			}
		})
	}
}

func BenchmarkPeakAdd(b *testing.B) {
	b.ReportAllocs()
	var p Peak
	at := t0
	for i := range b.N {
		at = at.Add(time.Millisecond)
		p.Add(float64(i&1023), at)
	}
}

func BenchmarkIntervalsAdd(b *testing.B) {
	b.ReportAllocs()
	var iv Intervals
	at := t0
	for i := range b.N {
		at = at.Add(time.Second + time.Duration(i&1023))
		iv.Add(at)
	}
}
