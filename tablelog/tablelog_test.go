package tablelog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/org-runink/runi/barn"
)

// Code written against v0.8.0 must compile and behave unchanged, and its
// values must flow into code already moved to barn.
func TestOldCodeStillWorks(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tb, err := Open(st, "acme", "events",
		WithMaxAttempts(5), WithCheckpointInterval(2), WithCompaction(1<<10, 0.5),
		WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("one")}); err != nil {
		t.Fatal(err)
	}

	// The same table, read through the new package: an alias, not a copy.
	var moved *barn.Table = tb
	rec, ok, err := moved.Get(ctx, "a")
	if err != nil || !ok || string(rec.Payload) != "one" || !rec.TS.Equal(now) {
		t.Fatalf("Get through barn = %+v %v %v", rec, ok, err)
	}

	// And a table opened by barn, read back through the old name.
	again, err := barn.Open(st, "acme", "events")
	if err != nil {
		t.Fatal(err)
	}
	var old *Table = again
	if v, err := old.Version(ctx); err != nil || v != 1 {
		t.Fatalf("Version through tablelog = %d %v", v, err)
	}

	if _, err := Open(st, "bad name!", "x"); err == nil {
		t.Fatal("Open accepted an invalid tenant through the old name")
	}
}

func TestErrorsAreBarnsOwn(t *testing.T) {
	for _, pair := range [][2]error{
		{ErrExists, barn.ErrExists}, {ErrConflict, barn.ErrConflict},
		{ErrRetriesExhausted, barn.ErrRetriesExhausted}, {ErrNoVersion, barn.ErrNoVersion},
		{ErrCorrupt, barn.ErrCorrupt}, {ErrStaleCommit, barn.ErrStaleCommit},
	} {
		if !errors.Is(pair[0], pair[1]) || !errors.Is(pair[1], pair[0]) {
			t.Errorf("%v and %v do not match each other", pair[0], pair[1])
		}
	}
	if MinRetention != barn.MinRetention || MaxCommitAge != barn.MaxCommitAge {
		t.Error("retention limits differ from barn's")
	}
}
