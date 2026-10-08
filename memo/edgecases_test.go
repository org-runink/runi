package memo

import (
	"testing"
	"time"
)

// Gaps the existing tests leave: the direct Get path (they exercise Do), the
// update-in-place branch of storeLocked, and the writeValue type switch.

func TestGetMissOnEmptyStore(t *testing.T) {
	s := New[string, int](Options{})
	if _, ok := s.Get("absent"); ok {
		t.Fatal("Get on an empty store reported a hit")
	}
}

func TestGetExpiresWithoutGoingThroughDo(t *testing.T) {
	c := &fakeClock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	s := New[string, int](Options{TTL: time.Minute, Now: c.Now})
	s.Set("k", 1)

	if _, ok := s.Get("k"); !ok {
		t.Fatal("entry gone before its TTL elapsed")
	}
	c.Add(time.Minute) // expiry is "not before", so exactly TTL is expired
	if _, ok := s.Get("k"); ok {
		t.Fatal("Get served an expired entry")
	}
	st := s.Stats()
	if st.Expired != 1 {
		t.Fatalf("Stats().Expired = %d; want 1", st.Expired)
	}
	if st.Entries != 0 {
		t.Fatalf("expired entry was not removed: Entries = %d", st.Entries)
	}
}

func TestSetOverwritesInPlaceAndRefreshesExpiry(t *testing.T) {
	c := &fakeClock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	s := New[string, int](Options{TTL: time.Minute, Now: c.Now})
	s.Set("k", 1)
	c.Add(30 * time.Second)
	s.Set("k", 2) // must update the existing entry, not append a second one

	if st := s.Stats(); st.Entries != 1 {
		t.Fatalf("overwriting created a second entry: Entries = %d", st.Entries)
	}
	c.Add(45 * time.Second) // 75s after the first Set, 45s after the second
	v, ok := s.Get("k")
	if !ok {
		t.Fatal("entry expired on the ORIGINAL deadline; Set must refresh it")
	}
	if v != 2 {
		t.Fatalf("value = %d; want the overwritten 2", v)
	}
}

func TestGetPromotesWithinTheLRU(t *testing.T) {
	// Get, not just Do, must count as a use — otherwise a frequently read key
	// is evicted as though it were idle.
	s := New[string, int](Options{Capacity: 2})
	s.Set("a", 1)
	s.Set("b", 2)
	if _, ok := s.Get("a"); !ok {
		t.Fatal("a missing")
	}
	s.Set("c", 3)

	if _, ok := s.Get("b"); ok {
		t.Fatal("evicted the recently read key instead of the idle one")
	}
	if _, ok := s.Get("a"); !ok {
		t.Fatal("a was evicted despite being read most recently")
	}
}

func TestHashCoversEverySupportedType(t *testing.T) {
	// Every branch of writeValue. The assertion is the property that matters:
	// each value hashes deterministically and none of them collide.
	values := []any{
		nil,
		"text", []byte("bytes"), true, false,
		int(1), int8(2), int16(3), int32(4), int64(5),
		uint(6), uint8(7), uint16(8), uint32(9), uint64(10),
		float32(1.5), float64(2.5),
		[]string{"a", "b"},
		[]any{1, "two", 3.0},
		map[string]any{"b": 2, "a": 1},
		map[string]string{"y": "2", "x": "1"},
		struct{ A int }{42}, // the default branch
	}

	seen := make(map[string]int, len(values))
	for i, v := range values {
		h := Hash(v)
		if h == "" {
			t.Fatalf("Hash(%#v) returned an empty key", v)
		}
		if j, dup := seen[h]; dup {
			t.Fatalf("Hash collision between %#v and %#v", values[j], v)
		}
		seen[h] = i
		if again := Hash(v); again != h {
			t.Fatalf("Hash(%#v) is not deterministic: %q then %q", v, h, again)
		}
	}
}

func TestHashSortsMapStringStringKeys(t *testing.T) {
	a := map[string]string{"x": "1", "y": "2", "z": "3"}
	b := map[string]string{"z": "3", "y": "2", "x": "1"}
	if Hash(a) != Hash(b) {
		t.Fatal("map[string]string key order changed the hash; keys must be sorted")
	}
}

func TestHashWalksSliceElements(t *testing.T) {
	if Hash([]string{"a", "b"}) == Hash([]string{"a", "c"}) {
		t.Fatal("changing a []string element did not change the hash")
	}
	if Hash([]any{1, 2}) == Hash([]any{1, 3}) {
		t.Fatal("changing a []any element did not change the hash")
	}
}

func TestRemoveLockedIgnoresAnAbsentKey(t *testing.T) {
	// Defensive guard, exercised directly: every caller checks presence first,
	// so this branch is unreachable through the public API. It exists so that a
	// future caller that forgets to check cannot corrupt the LRU list.
	s := New[string, int](Options{})
	s.Set("present", 1)

	s.mu.Lock()
	s.removeLocked("never-stored")
	s.mu.Unlock()

	if _, ok := s.Get("present"); !ok {
		t.Fatal("removing an absent key disturbed an existing entry")
	}
	if st := s.Stats(); st.Entries != 1 {
		t.Fatalf("Entries = %d; want 1", st.Entries)
	}
}
