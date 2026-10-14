package throttle

import (
	"errors"
	"testing"
	"time"
)

func TestNewBudgetInvalid(t *testing.T) {
	for _, c := range []struct {
		limit int
		w     time.Duration
	}{{0, time.Second}, {-1, time.Second}, {1, 0}, {1, -time.Second}} {
		if _, err := NewBudget(c.limit, c.w, t0); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewBudget(%d, %v) error = %v, want ErrInvalid", c.limit, c.w, err)
		}
	}
}

// Two per minute, windows starting on the half minute. t0 lies in the window
// [−30 s, 30 s), before the origin.
func TestBudgetScript(t *testing.T) {
	b := mustBudget(t, 2, time.Minute, ts(30))
	if b.Limit() != 2 || b.Window() != time.Minute {
		t.Fatal("parameters")
	}
	if b.Remaining(ts(0)) != 2 || !b.WindowEnd(ts(0)).Equal(ts(30)) {
		t.Fatalf("Remaining %d WindowEnd %v", b.Remaining(ts(0)), b.WindowEnd(ts(0)).Sub(t0))
	}
	if !b.Allow(ts(0)) || !b.Allow(ts(10)) || b.Allow(ts(29.999)) {
		t.Fatal("not exactly two in the first window")
	}
	if b.Remaining(ts(29)) != 0 || b.Remaining(ts(30)) != 2 {
		t.Fatal("Remaining across the boundary")
	}
	if !b.WindowEnd(ts(30)).Equal(ts(90)) {
		t.Fatal("a boundary time belongs to the window it starts")
	}
	if b.AllowN(ts(30), 3) || !b.AllowN(ts(30), 2) || b.Allow(ts(89)) {
		t.Fatal("AllowN in the second window")
	}
	if !b.AllowN(ts(89), 0) || !b.AllowN(ts(89), -1) {
		t.Fatal("AllowN of nothing refused")
	}
	if !b.Allow(ts(90)) || b.Remaining(ts(90)) != 1 {
		t.Fatal("third window")
	}
	// Far before the origin: windows exist there too, aligned the same way.
	c := mustBudget(t, 1, time.Minute, ts(30))
	if !c.WindowEnd(ts(-3600)).Equal(ts(-3570)) || !c.Allow(ts(-3600)) || c.Allow(ts(-3571)) || !c.Allow(ts(-3570)) {
		t.Fatal("windows before the origin")
	}
	if b.Allowed() != 4 || b.Denied() != 3 || b.Late() != 0 {
		t.Fatalf("Allowed %d Denied %d Late %d, want 4 3 0", b.Allowed(), b.Denied(), b.Late())
	}
	b.Reset()
	if b.Allowed() != 0 || b.Denied() != 0 || b.Remaining(ts(90)) != 2 || b.Limit() != 2 || b.Window() != time.Minute {
		t.Fatal("Reset")
	}
	if !b.Allow(ts(0)) {
		t.Fatal("after Reset an earlier time is not late")
	}
}

// A clock stepped back cannot reopen an earlier window: the late call is
// taken at the latest time, in the latest window.
func TestBudgetLate(t *testing.T) {
	b := mustBudget(t, 1, time.Minute, t0)
	if !b.Allow(ts(59)) || !b.Allow(ts(60)) {
		t.Fatal("one per window")
	}
	if b.Allow(ts(59)) || b.Late() != 1 {
		t.Fatal("a late call reopened the earlier window")
	}
	if b.Remaining(ts(0)) != 0 || !b.WindowEnd(ts(0)).Equal(ts(120)) {
		t.Fatal("queries with a late time do not use the latest")
	}
}
