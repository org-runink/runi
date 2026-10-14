package drift

import (
	"errors"
	"math"
	"testing"
)

func TestSignalString(t *testing.T) {
	for s, want := range map[Signal]string{None: "none", Up: "up", Down: "down", Signal(7): "Signal(7)"} {
		if s.String() != want {
			t.Errorf("%d: %q want %q", int(s), s.String(), want)
		}
	}
}

func TestTwoSided(t *testing.T) {
	for _, c := range []struct {
		up, down, h float64
		want        Signal
	}{
		{0, 0, 1, None},
		{1, 1, 1, None}, // at h is not over h
		{2, 0, 1, Up},
		{0, 2, 1, Down},
		{3, 2, 1, Up},   // both over: the larger
		{2, 3, 1, Down}, // both over: the larger
		{2, 2, 1, Up},   // tie goes to Up
	} {
		if got := twoSided(c.up, c.down, c.h); got != c.want {
			t.Errorf("twoSided(%v, %v, %v) = %v want %v", c.up, c.down, c.h, got, c.want)
		}
	}
}

func TestEWMAChartKnownAnswer(t *testing.T) {
	// target 10, σ 2, λ 0.5, L 3.
	c, err := NewEWMAChart(10, 2, 0.5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if lo, hi := c.Limits(); lo != 10 || hi != 10 || c.Statistic() != 10 {
		t.Fatalf("before any sample: limits %v %v statistic %v", lo, hi, c.Statistic())
	}
	// t=1, x=14: z = 0.5·14 + 0.5·10 = 12.
	// width = 3·2·sqrt(0.5/1.5 · (1 − 0.5²)) = 6·sqrt(0.25) = 3, band 7..13.
	if s := c.Add(14); s != None || c.Statistic() != 12 {
		t.Fatalf("t=1: %v z=%v", s, c.Statistic())
	}
	if lo, hi := c.Limits(); relErr(lo, 7) > 1e-15 || relErr(hi, 13) > 1e-15 {
		t.Fatalf("t=1 limits %v %v want 7 13", lo, hi)
	}
	// t=2, x=16: z = 8 + 6 = 14.
	// width = 6·sqrt(1/3 · (1 − 0.5⁴)) = 6·sqrt(0.3125) = 3.3541, hi 13.354.
	if s := c.Add(16); s != Up || c.Statistic() != 14 {
		t.Fatalf("t=2: %v z=%v", s, c.Statistic())
	}
	if _, hi := c.Limits(); relErr(hi, 10+6*math.Sqrt(0.3125)) > 1e-15 {
		t.Fatalf("t=2 hi %v", hi)
	}

	c.Reset()
	// Downward: x=4 gives z = 7, exactly ON the lower limit 7, which is not
	// outside it. Then x=4 again: z = 5.5 < 10 − 3.354 = 6.646.
	if s := c.Add(4); s != None || c.Statistic() != 7 {
		t.Fatalf("on the limit: %v z=%v", s, c.Statistic())
	}
	if s := c.Add(4); s != Down || c.Statistic() != 5.5 {
		t.Fatalf("below: %v z=%v", s, c.Statistic())
	}
	if c.N() != 2 {
		t.Fatalf("N %d", c.N())
	}
}

// The band widens to the asymptotic σ·L·sqrt(λ/(2−λ)) and the running product
// (1−λ)^(2t) agrees with the closed form.
func TestEWMAChartLimitsConverge(t *testing.T) {
	c, _ := DefaultEWMAChart(0, 1)
	for i := 1; i <= 400; i++ {
		c.Add(0)
		_, hi := c.Limits()
		want := 3 * math.Sqrt(0.2/1.8*(1-math.Pow(0.8, float64(2*i))))
		if relErr(hi, want) > 1e-12 {
			t.Fatalf("t=%d hi %v want %v", i, hi, want)
		}
	}
	if _, hi := c.Limits(); relErr(hi, 1) > 1e-12 { // 3·sqrt(0.2/1.8) = 3·(1/3) = 1
		t.Fatalf("asymptotic hi %v want 1", hi)
	}
	// The running product must reach exactly zero rather than sit on the
	// smallest subnormal, where 0.64·x rounds back to x and every multiply
	// is slow. The first check shows that would happen without the cutoff.
	if tiny := math.SmallestNonzeroFloat64; tiny*0.64 != tiny {
		t.Fatal("the subnormal no longer sticks: the cutoff test is moot")
	}
	for range 2000 {
		c.Add(0)
	}
	if c.decay != 0 {
		t.Fatalf("decay %v after 2400 samples, want exactly 0", c.decay)
	}
}

// λ = 1 is a Shewhart chart: z is the sample, limits target ± Lσ from t = 1.
func TestEWMAChartShewhart(t *testing.T) {
	c, _ := NewEWMAChart(10, 2, 1, 3)
	if s := c.Add(15.9); s != None || c.Statistic() != 15.9 {
		t.Fatalf("%v z %v", s, c.Statistic())
	}
	if lo, hi := c.Limits(); lo != 4 || hi != 16 {
		t.Fatalf("limits %v %v want 4 16", lo, hi)
	}
	if s := c.Add(16.1); s != Up {
		t.Fatalf("%v", s)
	}
}

func TestEWMAChartRejects(t *testing.T) {
	bad := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, v := range bad {
		if _, err := NewEWMAChart(v, 1, 0.2, 3); !errors.Is(err, ErrInvalid) {
			t.Errorf("target %v: %v", v, err)
		}
	}
	for _, v := range append(bad, 0, -1) {
		if _, err := NewEWMAChart(0, v, 0.2, 3); !errors.Is(err, ErrInvalid) {
			t.Errorf("sigma %v: %v", v, err)
		}
		if _, err := NewEWMAChart(0, 1, 0.2, v); !errors.Is(err, ErrInvalid) {
			t.Errorf("L %v: %v", v, err)
		}
		if _, err := DefaultEWMAChart(0, v); !errors.Is(err, ErrInvalid) {
			t.Errorf("default sigma %v: %v", v, err)
		}
	}
	for _, v := range append(bad, 0, -0.5, 1.5) {
		if _, err := NewEWMAChart(0, 1, v, 3); !errors.Is(err, ErrInvalid) {
			t.Errorf("lambda %v: %v", v, err)
		}
	}
	if _, err := NewEWMAChart(0, 1, 1, 3); err != nil {
		t.Errorf("lambda 1 is valid: %v", err)
	}
}

func TestEWMAChartSkipsNonFinite(t *testing.T) {
	c, _ := DefaultEWMAChart(5, 1)
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if s := c.Add(x); s != None {
			t.Fatalf("%v signalled %v", x, s)
		}
	}
	if c.N() != 0 || c.Skipped() != 3 || c.Statistic() != 5 {
		t.Fatalf("N %d skipped %d z %v", c.N(), c.Skipped(), c.Statistic())
	}
	c.Add(5)
	c.Reset()
	if c.N() != 0 || c.Skipped() != 0 || c.Statistic() != 5 {
		t.Fatalf("Reset left %+v", c)
	}
	if lo, hi := c.Limits(); lo != 5 || hi != 5 {
		t.Fatalf("Reset left limits %v %v", lo, hi)
	}
}
