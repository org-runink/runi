package throttle

import (
	"errors"
	"testing"
	"time"
)

func TestNewBreakerInvalid(t *testing.T) {
	for _, c := range []struct {
		th, probes int
		open       time.Duration
	}{{0, 1, 0}, {-1, 1, 0}, {1, 1, -1}, {1, 0, 0}, {1, -1, time.Second}} {
		if _, err := NewBreaker(c.th, c.open, c.probes); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewBreaker(%d, %v, %d) error = %v, want ErrInvalid", c.th, c.open, c.probes, err)
		}
	}
}

func TestStateString(t *testing.T) {
	for s, want := range map[State]string{Closed: "closed", Open: "open", HalfOpen: "half-open", 7: "State(7)"} {
		if s.String() != want {
			t.Errorf("%d.String() = %q, want %q", s, s.String(), want)
		}
	}
}

// Every transition of the state machine, in order, with its time and count.
func TestBreakerScript(t *testing.T) {
	b := mustBreaker(t, 3, 10*time.Second, 2)
	type step struct {
		at    float64
		do    string // "allow", "deny", "ok", "fail", "state"
		state State
		trans int64
		since float64
	}
	for i, s := range []step{
		{0, "allow", Closed, 0, -1},
		{1, "fail", Closed, 0, -1},
		{2, "fail", Closed, 0, -1},
		{3, "ok", Closed, 0, -1}, // a success resets the run of failures
		{4, "fail", Closed, 0, -1},
		{5, "fail", Closed, 0, -1},
		{6, "fail", Open, 1, 6}, // the third in a row opens it
		{7, "deny", Open, 1, 6},
		{8, "ok", Open, 1, 6},        // results while open are ignored
		{15.999, "deny", Open, 1, 6}, // open for exactly 10 s
		{16, "state", HalfOpen, 2, 16},
		{16, "ok", HalfOpen, 2, 16}, // no probe out yet: a late result, ignored
		{17, "allow", HalfOpen, 2, 16},
		{17, "allow", HalfOpen, 2, 16},
		{17, "deny", HalfOpen, 2, 16}, // only two probes
		{18, "ok", HalfOpen, 2, 16},
		{19, "fail", Open, 3, 19}, // one probe failed: open again
		{29, "allow", HalfOpen, 4, 29},
		{29, "allow", HalfOpen, 4, 29},
		{30, "ok", HalfOpen, 4, 29},
		{31, "ok", Closed, 5, 31}, // both probes succeeded
		{32, "fail", Closed, 5, 31},
	} {
		at := ts(s.at)
		switch s.do {
		case "allow", "deny":
			if got := b.Allow(at); got != (s.do == "allow") {
				t.Fatalf("step %d: Allow = %v", i, got)
			}
		case "ok", "fail":
			b.Record(s.do == "ok", at)
		}
		if got := b.State(at); got != s.state || b.Transitions() != s.trans {
			t.Fatalf("step %d: state %v after %d transitions, want %v after %d", i, got, b.Transitions(), s.state, s.trans)
		}
		if s.since >= 0 && !b.Since().Equal(ts(s.since)) {
			t.Fatalf("step %d: Since %v, want %v", i, b.Since().Sub(t0), s.since)
		}
	}
	if b.Failures() != 1 || !b.OpenUntil().IsZero() {
		t.Fatalf("Failures %d OpenUntil %v", b.Failures(), b.OpenUntil())
	}
	b.Reset()
	if b.State(ts(0)) != Closed || b.Transitions() != 0 || b.Failures() != 0 || !b.Since().IsZero() {
		t.Fatal("Reset")
	}
}

func TestBreakerOpenUntil(t *testing.T) {
	b := mustBreaker(t, 1, 5*time.Second, 1)
	b.Record(false, ts(2))
	if !b.OpenUntil().Equal(ts(7)) {
		t.Fatalf("OpenUntil = %v, want 7 s", b.OpenUntil().Sub(t0))
	}
	// A clock stepped back cannot end the open period early.
	if b.Allow(ts(100)); b.State(ts(100)) != HalfOpen {
		t.Fatal("not half-open after the period")
	}
	b.Record(false, ts(100))
	if b.Allow(ts(50)) || b.State(ts(50)) != Open || b.Late() != 2 {
		t.Fatal("a late time ended the open period")
	}
}

// An open duration of zero lets the first call after opening probe at once.
func TestBreakerZeroOpen(t *testing.T) {
	b := mustBreaker(t, 1, 0, 1)
	b.Record(false, ts(1))
	if b.state != Open {
		t.Fatal("did not open")
	}
	if !b.Allow(ts(1)) || b.Allow(ts(1)) {
		t.Fatal("not exactly one probe at once")
	}
	b.Record(true, ts(1))
	if b.State(ts(1)) != Closed || b.Transitions() != 3 {
		t.Fatalf("state %v after %d transitions", b.State(ts(1)), b.Transitions())
	}
}
