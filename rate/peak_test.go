package rate

import (
	"math"
	"testing"
	"time"
)

func TestPeak(t *testing.T) {
	var p Peak
	if !math.IsNaN(p.Max()) || !p.At().IsZero() || p.N() != 0 {
		t.Fatalf("zero value: %v %v %d", p.Max(), p.At(), p.N())
	}
	// A first value below zero is still the peak: no implicit 0 to beat.
	p.Add(-5, t0)
	if p.Max() != -5 || !p.At().Equal(t0) {
		t.Fatalf("%v at %v", p.Max(), p.At())
	}
	p.Add(3, t0.Add(time.Second))
	p.Add(1, t0.Add(2*time.Second))
	p.Add(3, t0.Add(3*time.Second)) // a tie keeps the first
	p.Add(math.NaN(), t0.Add(4*time.Second))
	p.Add(math.Inf(1), t0.Add(5*time.Second))
	if p.Max() != 3 || !p.At().Equal(t0.Add(time.Second)) || p.N() != 4 || p.Skipped() != 2 {
		t.Fatalf("%v at %v n %d skipped %d", p.Max(), p.At().Sub(t0), p.N(), p.Skipped())
	}
	// Times need not increase: a larger value with an earlier time wins.
	p.Add(10, t0.Add(-time.Hour))
	if p.Max() != 10 || !p.At().Equal(t0.Add(-time.Hour)) {
		t.Fatalf("%v at %v", p.Max(), p.At())
	}
	p.Reset()
	if !math.IsNaN(p.Max()) || !p.At().IsZero() || p.N() != 0 || p.Skipped() != 0 {
		t.Fatalf("after Reset: %+v", p)
	}
	p.Add(-1, t0)
	if p.Max() != -1 {
		t.Fatalf("after Reset the first value is the peak, got %v", p.Max())
	}
}
