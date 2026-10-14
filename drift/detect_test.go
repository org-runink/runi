package drift

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"
)

// Behaviour every detector promises, on seeded Gaussian noise with σ = 1 and
// target 0, at the documented defaults. Seeds are fixed, so each test is a
// deterministic check; the tolerances are set so that a different seed would
// pass with high probability too, which is what makes the result a property
// of the detector rather than of the seed.

type detector interface {
	Add(x float64) Signal
}

var detectors = []struct {
	name string
	make func() detector
}{
	{"EWMAChart", func() detector { c, _ := DefaultEWMAChart(0, 1); return &c }},
	{"CUSUM", func() detector { c, _ := DefaultCUSUM(0, 1); return &c }},
	{"PageHinkley", func() detector { p, _ := NewPageHinkley(0.5, 5); return &p }},
}

// An in-control stream does not signal over a run well short of the average
// run length. P(no false alarm in 60 samples) is about 0.9 for each detector
// at these defaults; the fixed seed makes it certain here.
func TestInControlNoSignal(t *testing.T) {
	for _, d := range detectors {
		r := rand.New(rand.NewPCG(21, 22))
		det := d.make()
		for i := range 60 {
			if s := det.Add(r.NormFloat64()); s != None {
				t.Errorf("%s: false alarm %v at sample %d", d.name, s, i+1)
				break
			}
		}
	}
}

const warmup = 100 // in-control samples before the step

// delays runs reps streams of warmup in-control samples followed by a step of
// shift, and returns the detection delay of each (1 = caught on the first
// shifted sample). The same noise is fed, mirrored, to a second detector:
// with target 0 the mirrored stream must give Down at exactly the same delay,
// which checks the two sides are the same test. Streams with a false alarm
// during warm-up are left out; they are counted by the ARL tests instead.
func delays(t *testing.T, name string, mk func() detector, shift float64, seed uint64) []int {
	t.Helper()
	const reps, horizon = 400, 400
	r := rand.New(rand.NewPCG(seed, 23))
	noise := make([]float64, warmup+horizon)
	var out []int
	for range reps {
		for i := range noise {
			noise[i] = r.NormFloat64()
		}
		up, down := mk(), mk()
		warned := false
		for _, x := range noise[:warmup] {
			if up.Add(x) != None {
				warned = true
			}
			down.Add(-x)
		}
		if warned {
			continue
		}
		got := 0
		for i, x := range noise[warmup:] {
			su, sd := up.Add(x+shift), down.Add(-(x + shift))
			if su != sd.mirror() {
				t.Fatalf("%s shift %v: mirrored streams disagree at %d: %v vs %v", name, shift, i+1, su, sd)
			}
			if su == Up {
				got = i + 1
				break
			}
			if su == Down {
				t.Fatalf("%s: upward shift %v reported as Down", name, shift)
			}
		}
		if got == 0 {
			t.Fatalf("%s: shift of %vσ not detected within %d samples", name, shift, horizon)
		}
		out = append(out, got)
	}
	if len(out) < reps*3/4 {
		t.Fatalf("%s: %d of %d streams false-alarmed during warm-up", name, reps-len(out), reps)
	}
	return out
}

func (s Signal) mirror() Signal {
	switch s {
	case Up:
		return Down
	case Down:
		return Up
	}
	return s
}

func mean(xs []int) float64 {
	s := 0
	for _, x := range xs {
		s += x
	}
	return float64(s) / float64(len(xs))
}

// A step of 1σ, 2σ and 3σ is detected, in both directions, and the delay
// shrinks as the shift grows.
func TestStepShiftDetected(t *testing.T) {
	for _, d := range detectors {
		prev := math.Inf(1)
		for i, shift := range []float64{1, 2, 3} {
			m := mean(delays(t, d.name, d.make, shift, uint64(31+i)))
			t.Logf("%s: mean delay for %vσ is %.2f samples", d.name, shift, m)
			if !(m < prev) {
				t.Errorf("%s: delay %.2f for %vσ is not shorter than %.2f for the smaller shift", d.name, m, shift, prev)
			}
			prev = m
		}
	}
}

// Since points back at the sample the shift began.
func TestCUSUMSinceLocatesChange(t *testing.T) {
	for i, shift := range []float64{1, 2, 3} {
		r := rand.New(rand.NewPCG(uint64(41+i), 42))
		var errs []int
		for range 400 {
			c, _ := DefaultCUSUM(0, 1)
			start := int64(-1)
			for j := 1; j <= warmup+400; j++ {
				x := r.NormFloat64()
				if j > warmup {
					x += shift
				}
				if c.Add(x) != None {
					start = c.N() - c.Since() + 1
					if j <= warmup {
						start = -1 // a false alarm, not a test of Since
					}
					break
				}
			}
			if start < 0 {
				continue
			}
			e := int(start - (warmup + 1))
			errs = append(errs, max(e, -e))
		}
		sort.Ints(errs)
		med, p90 := errs[len(errs)/2], errs[len(errs)*9/10]
		t.Logf("%vσ: |estimated − true start|: median %d, 90th percentile %d", shift, med, p90)
		if med > 2 || p90 > 8 {
			t.Errorf("%vσ: Since is off by a median %d and 90th percentile %d samples", shift, med, p90)
		}
	}
}

// arl is the mean number of samples to the first signal over reps streams of
// N(shift, 1).
func arl(mk func() detector, shift float64, reps int, seed uint64) float64 {
	r := rand.New(rand.NewPCG(seed, 51))
	total := 0
	for range reps {
		d := mk()
		n := 1
		for d.Add(r.NormFloat64()+shift) == None {
			n++
		}
		total += n
	}
	return float64(total) / float64(reps)
}

// The defaults mean what the docs say. Montgomery's table gives an in-control
// average run length of 168 at k = σ/2, h = 4σ and 465 at h = 5σ, and 8.38 and
// 10.4 samples for a one-sigma shift. The standard error of a 4000-run
// estimate is about 1.6% of the ARL, so the bands below are several standard
// errors wide, and an h off by 10% falls outside them: measured with this
// seed, h = 3.6 and 4.4 give 114 and 254, h = 4.5 and 5.5 give 280 and 751.
func TestCUSUMARL(t *testing.T) {
	h4 := func() detector { c, _ := NewCUSUM(0, 0.5, 4); return &c }
	h5 := func() detector { c, _ := DefaultCUSUM(0, 1); return &c }
	for _, c := range []struct {
		name   string
		mk     func() detector
		shift  float64
		lo, hi float64
	}{
		{"h=4 in control", h4, 0, 120, 230},
		{"h=5 in control", h5, 0, 380, 560},
		{"h=4 1σ", h4, 1, 7.5, 9.5},
		{"h=5 1σ", h5, 1, 9.3, 11.5},
	} {
		got := arl(c.mk, c.shift, 4000, 61)
		t.Logf("CUSUM %s: ARL %.1f", c.name, got)
		if got < c.lo || got > c.hi {
			t.Errorf("CUSUM %s: ARL %.1f outside %v..%v", c.name, got, c.lo, c.hi)
		}
	}
}

// DefaultEWMAChart's documented run lengths. L = 2.85 and 3.15, 5% either
// side of the default, measure 353 and 886 with this seed: outside the band.
func TestEWMAChartARL(t *testing.T) {
	mk := detectors[0].make
	if got := arl(mk, 0, 4000, 62); got < 450 || got > 680 {
		t.Errorf("in-control ARL %.1f, documented about 560", got)
	} else {
		t.Logf("in-control ARL %.1f", got)
	}
	if got := arl(mk, 1, 4000, 63); got < 9 || got > 11.5 {
		t.Errorf("1σ ARL %.2f, documented about 10", got)
	} else {
		t.Logf("1σ ARL %.2f", got)
	}
}

func TestPageHinkleyARL(t *testing.T) {
	mk := detectors[2].make
	if got := arl(mk, 0, 4000, 64); got < 390 || got > 580 {
		t.Errorf("in-control ARL %.1f, documented about 480", got)
	} else {
		t.Logf("in-control ARL %.1f", got)
	}
	if got := mean(delays(t, "PageHinkley", mk, 1, 65)); got < 9 || got > 13.5 {
		t.Errorf("1σ delay after %d samples %.2f, documented about 11 to 12", warmup, got)
	} else {
		t.Logf("1σ delay after %d samples %.2f", warmup, got)
	}
}
