package tablelog

import (
	"context"
	"errors"
	"testing"
)

// The change feed's ordering must be a TOTAL order. sort.Slice is not stable,
// so a comparator that reports neither a<b nor b<a for two distinct changes
// lets them come back in either order on different runs — and a consumer
// replaying one version twice would then apply it two different ways.
//
// The key comparison never decides anything on data this package produces,
// because Ord is unique within a commit. It is tested here as the predicate it
// is, rather than through a table that cannot produce the tie.
func TestChangeOrderingIsTotal(t *testing.T) {
	cases := []struct {
		name string
		a, b Change
		want bool
	}{
		{"older version first", mk(1, 0, "k"), mk(2, 0, "k"), true},
		{"newer version not first", mk(2, 0, "k"), mk(1, 0, "k"), false},
		{"same version, lower Ord first", mk(1, 1, "k"), mk(1, 2, "k"), true},
		{"same version, higher Ord not first", mk(1, 2, "k"), mk(1, 1, "k"), false},
		{"same version and Ord, key decides", mk(1, 0, "a"), mk(1, 0, "b"), true},
		{"same version and Ord, key decides the other way", mk(1, 0, "b"), mk(1, 0, "a"), false},
		{"identical is not less", mk(1, 0, "a"), mk(1, 0, "a"), false},
	}
	for _, c := range cases {
		if got := changeLess(c.a, c.b); got != c.want {
			t.Errorf("%s: changeLess = %v, want %v", c.name, got, c.want)
		}
	}
	// Totality: for any two distinct changes exactly one direction holds.
	x, y := mk(1, 0, "a"), mk(1, 0, "b")
	if changeLess(x, y) == changeLess(y, x) {
		t.Error("comparator is not a total order: both directions agree for distinct changes")
	}
	if changeLess(x, x) {
		t.Error("a change is less than itself")
	}
}

func mk(v, ord int64, key string) Change {
	var c Change
	c.Version = v
	c.Ord = ord
	c.Key = key
	return c
}

// A commit that cannot be serialised must not be written. Today nothing makes
// this fail — a log entry is strings, ints and slices — but the entry is
// written to the store before anything else happens, so a silent empty object
// would be a log entry nobody can read.
func TestCommitRefusesAnUnserialisableEntry(t *testing.T) {
	orig := marshalLogEntry
	marshalLogEntry = func(*logEntry) ([]byte, error) { return nil, errors.New("cannot serialise") }
	defer func() { marshalLogEntry = orig }()

	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(context.Background(), Row{Key: "a", Payload: []byte("1")}); err == nil {
		t.Fatal("a commit succeeded although its log entry could not be serialised")
	}
	if v, err := tb.Version(context.Background()); err != nil || v != 0 {
		t.Errorf("version = %d (err %v), want 0", v, err)
	}
}

// commit applies its own entry to the state it built it from, and refuses if
// that fails. No caller in this package can produce such an action — Put and
// the compactor both build well-formed ones — so it is driven directly here.
// It matters because the entry is already in the store by this point: a commit
// that returned success after failing to apply would leave the table's
// in-memory state disagreeing with its log.
func TestCommitRefusesAnActionItCannotApply(t *testing.T) {
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if _, err := tb.Put(ctx, Row{Key: "seed", Payload: []byte("1")}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, c := range []struct {
		name string
		act  action
	}{
		{"the same file added twice", action{op: "append", add: []fileEntry{
			{Path: "data/dup.avro", Rows: 1}, {Path: "data/dup.avro", Rows: 1},
		}}},
		{"a file outside data/", action{op: "append", add: []fileEntry{
			{Path: "../escape.avro", Rows: 1},
		}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := tb.commit(ctx, c.act); err == nil {
				t.Error("commit accepted an action it cannot apply")
			}
		})
	}
}

// Compaction re-plans when it loses a race, and waits between plans. If the
// caller cancels during that wait, Compact must return their error and stop,
// rather than carry on rewriting files for a request that is gone.
//
// The conflict is real and deterministic — a second writer removes exactly the
// files the plan is built on — while the wait itself is stubbed. Three earlier
// versions of this test tried to cancel a live context at the right instant
// and all three passed while covering nothing: cancel before the plan's reads
// and they fail with context.Canceled so no conflict is ever detected; cancel
// after and commit's own retry backs off first and returns the cancellation
// from inside. The window between them contains no I/O to hook. Stubbing the
// wait tests the contract that line actually has.
type compactRacer struct {
	Store
	armed  bool
	fired  bool
	remove func()
}

// Data files are written with PutIfAbsent, not Put — their names are content
// addressed, so a collision means someone else wrote this exact file.
func (c *compactRacer) PutIfAbsent(ctx context.Context, k string, d []byte) error {
	if c.armed && !c.fired && contains(k, "/data/") {
		c.fired = true
		c.remove()
	}
	return c.Store.PutIfAbsent(ctx, k, d)
}

func TestCompactStopsWhenTheWaitBetweenPlansEnds(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	racer := &compactRacer{Store: ms}

	tb, err := Open(racer, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0), WithMaxAttempts(5))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	other, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open other: %v", err)
	}
	for i := range 8 {
		if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	s, err := tb.loadState(ctx, -1)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	var paths []string
	for _, f := range s.sortedFiles() {
		paths = append(paths, f.Path)
	}
	if len(paths) < 2 {
		t.Fatalf("only %d files: compactOnce returns early and this proves nothing", len(paths))
	}
	racer.remove = func() {
		if _, err := other.commit(ctx, action{op: "append", remove: paths}); err != nil {
			t.Errorf("competing removal: %v", err)
		}
	}
	racer.armed = true

	var waits int
	orig := waitBetweenPlans
	waitBetweenPlans = func(*Table, context.Context, int) error {
		waits++
		return context.Canceled
	}
	defer func() { waitBetweenPlans = orig }()

	_, err = tb.Compact(ctx)

	if !racer.fired {
		t.Fatal("the competing removal never ran: no conflict was created")
	}
	if waits != 1 {
		t.Errorf("waited %d times, want exactly 1 — the plan should have been re-planned once", waits)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled from the wait", err)
	}
}
