package drift

import (
	"testing"
	"time"
)

// The package doc promises no allocation per update; this is the test of it.
// Each closure feeds a changing value so the call is not optimised to nothing.
func TestZeroAllocs(t *testing.T) {
	var w Welford
	e, _ := NewEWMA(0.1)
	c, _ := DefaultEWMAChart(0, 1)
	cu, _ := DefaultCUSUM(0, 1)
	p, _ := NewPageHinkley(0.5, 5)
	d, _ := NewDecayed(time.Minute)
	var other Welford
	other.Add(1)
	other.Add(2)
	x, total, at := 0.0, uint64(0), t0
	for name, f := range map[string]func(){
		"Welford.Add":     func() { x++; w.Add(x) },
		"Welford.Merge":   func() { w.Merge(other) },
		"EWMA.Add":        func() { x++; e.Add(x) },
		"EWMAChart.Add":   func() { x = -x + 0.1; c.Add(x) },
		"CUSUM.Add":       func() { x = -x + 0.1; cu.Add(x) },
		"PageHinkley.Add": func() { x = -x + 0.1; p.Add(x) },
		"Decayed.Update":  func() { total += 7; at = at.Add(time.Second); d.Update(total, at) },
	} {
		if n := testing.AllocsPerRun(1000, f); n != 0 {
			t.Errorf("%s: %v allocations per call, want 0", name, n)
		}
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

func BenchmarkWelfordAdd(b *testing.B) {
	b.ReportAllocs()
	var w Welford
	for i := range b.N {
		w.Add(float64(i & 1023))
	}
}

func BenchmarkEWMAAdd(b *testing.B) {
	b.ReportAllocs()
	e, _ := NewEWMA(0.1)
	for i := range b.N {
		e.Add(float64(i & 1023))
	}
}

func BenchmarkEWMAChartAdd(b *testing.B) {
	b.ReportAllocs()
	c, _ := DefaultEWMAChart(0, 1)
	for i := range b.N {
		c.Add(float64(i&7) - 3.5)
	}
}

func BenchmarkCUSUMAdd(b *testing.B) {
	b.ReportAllocs()
	c, _ := DefaultCUSUM(0, 1)
	for i := range b.N {
		c.Add(float64(i&7) - 3.5)
	}
}

func BenchmarkPageHinkleyAdd(b *testing.B) {
	b.ReportAllocs()
	p, _ := NewPageHinkley(0.5, 5)
	for i := range b.N {
		p.Add(float64(i&7) - 3.5)
	}
}

func BenchmarkDecayedUpdate(b *testing.B) {
	b.ReportAllocs()
	d, _ := NewDecayed(time.Minute)
	at := t0
	for i := range b.N {
		at = at.Add(time.Second)
		d.Update(uint64(i)*1000, at)
	}
}
