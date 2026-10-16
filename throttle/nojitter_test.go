package throttle

import (
	"testing"
	"time"
)

// NoJitter returns the exponential delay itself: base, 2·base, 4·base …
// capped. A caller that documents its retry schedule gets the schedule it
// documented.
func TestNoJitterIsExactlyExponential(t *testing.T) {
	b, err := NewBackoff(NoJitter, time.Second, time.Minute, nil)
	if err != nil {
		t.Fatalf("NewBackoff: %v", err)
	}
	want := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second,
		time.Minute, time.Minute, // capped, and stays capped
	}
	for i, w := range want {
		if got := b.Next(); got != w {
			t.Fatalf("delay %d = %v, want %v", i, got, w)
		}
	}
}

// A deterministic backoff needs no randomness, so nil is accepted for it —
// and still refused for the modes that do need a source.
func TestNoJitterNeedsNoSource(t *testing.T) {
	if _, err := NewBackoff(NoJitter, time.Second, time.Minute, nil); err != nil {
		t.Errorf("NoJitter with a nil source: %v; it needs no randomness", err)
	}
	if _, err := NewBackoff(FullJitter, time.Second, time.Minute, nil); err == nil {
		t.Error("FullJitter accepted a nil source; it draws from one")
	}
	if _, err := NewBackoff(DecorrelatedJitter, time.Second, time.Minute, nil); err == nil {
		t.Error("DecorrelatedJitter accepted a nil source; it draws from one")
	}
}

// Reset returns to the base delay, which is what a caller does after a
// success.
func TestNoJitterResets(t *testing.T) {
	b, err := NewBackoff(NoJitter, time.Second, time.Minute, nil)
	if err != nil {
		t.Fatalf("NewBackoff: %v", err)
	}
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got != time.Second {
		t.Errorf("after Reset the first delay was %v, want the base %v", got, time.Second)
	}
}

// The validation still refuses a value past the last mode, so a future
// constant cannot be used before it exists.
func TestAJitterPastTheLastModeIsRefused(t *testing.T) {
	if _, err := NewBackoff(NoJitter+1, time.Second, time.Minute, nil); err == nil {
		t.Error("a Jitter past NoJitter was accepted")
	}
}

// The existing constants keep their values: NoJitter was appended, not
// inserted, so a caller compiled against FullJitter or DecorrelatedJitter is
// unaffected.
func TestTheExistingJitterValuesDidNotMove(t *testing.T) {
	if FullJitter != 0 || DecorrelatedJitter != 1 || NoJitter != 2 {
		t.Errorf("Jitter values moved: Full=%d Decorrelated=%d No=%d; appending must not renumber",
			FullJitter, DecorrelatedJitter, NoJitter)
	}
}
