package throttle

import (
	"errors"
	"testing"
	"time"
)

func TestNewCooldownInvalid(t *testing.T) {
	for _, c := range []struct {
		p   time.Duration
		max int
	}{{0, 0}, {-time.Second, 0}, {time.Second, -1}} {
		if cd, err := NewCooldown[string](c.p, c.max); !errors.Is(err, ErrInvalid) || cd != nil {
			t.Errorf("NewCooldown(%v, %d) = %v, %v; want nil, ErrInvalid", c.p, c.max, cd, err)
		}
	}
}

// Exactly once per period per key, the period measured from the last allowed
// action; a refused action does not restart it.
func TestCooldownScript(t *testing.T) {
	cd, err := NewCooldown[string](10*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cd.Period() != 10*time.Second {
		t.Fatal("Period")
	}
	if !cd.Allow("a", ts(0)) || !cd.Allow("b", ts(1)) || cd.Allow("a", ts(5)) {
		t.Fatal("first actions")
	}
	if got := cd.Next("a", ts(5)); !got.Equal(ts(10)) {
		t.Fatalf("Next(a) = %v, want 10 s", got.Sub(t0))
	}
	if got := cd.Next("z", ts(5)); !got.Equal(ts(5)) {
		t.Fatalf("Next of an unknown key = %v, want now", got.Sub(t0))
	}
	if cd.Allow("a", ts(10).Add(-1)) || !cd.Allow("a", ts(10)) {
		t.Fatal("the period is not exact")
	}
	if cd.Len() != 2 {
		t.Fatalf("Len = %d, want 2", cd.Len())
	}
	// b's period ended at 11 s; it is still tracked until a call drops it,
	// and Next says it is free.
	if got := cd.Next("b", ts(12)); !got.Equal(ts(12)) {
		t.Fatalf("Next(b) after its period = %v, want now", got.Sub(t0))
	}
	if n := cd.Sweep(ts(12)); n != 1 || cd.Len() != 1 {
		t.Fatalf("Sweep removed %d, Len %d; want 1, 1", n, cd.Len())
	}
	if n := cd.Sweep(ts(19)); n != 0 {
		t.Fatalf("Sweep removed %d live keys", n)
	}
	if cd.Allowed() != 3 || cd.Denied() != 2 || cd.Full() != 0 || cd.Late() != 0 {
		t.Fatalf("Allowed %d Denied %d Full %d Late %d", cd.Allowed(), cd.Denied(), cd.Full(), cd.Late())
	}
	cd.Reset()
	if cd.Len() != 0 || cd.Allowed() != 0 || cd.Denied() != 0 || !cd.Allow("a", ts(0)) || cd.Period() != 10*time.Second {
		t.Fatal("Reset")
	}
}

// When the table is full of live keys a new key is refused, counted in
// Full, and no live key is evicted; Next names the time the oldest frees a
// place.
func TestCooldownCap(t *testing.T) {
	cd := mustCooldown(t, 10*time.Second, 2)
	if !cd.Allow(1, ts(0)) || !cd.Allow(2, ts(3)) {
		t.Fatal("first two keys")
	}
	if cd.Allow(3, ts(4)) || cd.Full() != 1 || cd.Denied() != 1 {
		t.Fatal("a third key was admitted over the cap")
	}
	if got := cd.Next(3, ts(4)); !got.Equal(ts(10)) {
		t.Fatalf("Next(3) = %v, want 10 s", got.Sub(t0))
	}
	if cd.Allow(1, ts(5)) || cd.Full() != 1 {
		t.Fatal("a live key was refused for the cap, or allowed")
	}
	// Key 1's period has ended but not been swept: the table is still full,
	// and Next says a place is free now.
	if got := cd.Next(3, ts(11)); !got.Equal(ts(11)) {
		t.Fatalf("Next(3) once key 1 expired = %v, want now", got.Sub(t0))
	}
	if !cd.Allow(3, ts(11)) || cd.Allow(4, ts(11)) || cd.Full() != 2 {
		t.Fatal("the expired key's place")
	}
	if cd.Len() != 2 {
		t.Fatalf("Len = %d over the cap", cd.Len())
	}
}

// The ring that holds the keys grows while it wraps, keeps its order, and
// with a cap never grows past it.
func TestCooldownGrowth(t *testing.T) {
	cd := mustCooldown(t, 10*time.Second, 0)
	// Six keys at 0 s fill the first ring of 8 to slot 5. At 10 s they
	// expire, leaving the head at slot 6, so the next keys wrap round to
	// slot 0; the ninth of them finds the ring full and wrapped, and the
	// copy must unwrap it. Keys 6–13 act at 10 s and 14–24 at 12 s, so the
	// order in which they expire shows the copy kept the order.
	for k := range 6 {
		cd.Allow(k, ts(0))
	}
	for k := 6; k < 25; k++ {
		at := ts(10)
		if k >= 14 {
			at = ts(12)
		}
		if !cd.Allow(k, at) {
			t.Fatalf("key %d", k)
		}
	}
	if cd.Len() != 19 || len(cd.queue) != 32 {
		t.Fatalf("Len %d ring %d, want 19 32", cd.Len(), len(cd.queue))
	}
	for k := 6; k < 25; k++ {
		if cd.Allow(k, ts(14)) {
			t.Fatalf("key %d allowed twice", k)
		}
	}
	if n := cd.Sweep(ts(20)); n != 8 {
		t.Fatalf("Sweep at 20 s removed %d, want the 8 keys from 10 s", n)
	}
	if n := cd.Sweep(ts(22)); n != 11 || cd.Len() != 0 {
		t.Fatalf("Sweep at 22 s removed %d, Len %d", n, cd.Len())
	}

	capped := mustCooldown(t, time.Second, 5)
	for k := range 9 {
		capped.Allow(k, ts(0))
	}
	if len(capped.queue) != 5 || capped.Len() != 5 || capped.Full() != 4 {
		t.Fatalf("ring %d Len %d Full %d, want 5 5 4", len(capped.queue), capped.Len(), capped.Full())
	}
}

// Expired keys are dropped and their slots zeroed, so the table does not
// keep keys (and what they point to) alive.
func TestCooldownReleases(t *testing.T) {
	cd, _ := NewCooldown[*int](time.Second, 0)
	p := new(int)
	cd.Allow(p, ts(0))
	cd.Sweep(ts(1))
	for _, s := range cd.queue {
		if s.key != nil || !s.at.IsZero() {
			t.Fatal("an expired slot still holds its key")
		}
	}
	if len(cd.last) != 0 {
		t.Fatal("an expired key is still in the map")
	}
}

// The zero value is documented as unusable; Allow on it panics rather than
// silently allowing every action.
func TestCooldownZeroValuePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Allow on the zero Cooldown did not panic")
		}
	}()
	var cd Cooldown[string]
	cd.Allow("a", ts(0))
}
