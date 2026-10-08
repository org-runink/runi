package tablelog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func logKey(v int64) string { return fmt.Sprintf("tables/acme/orders/_log/%020d.json", v) }

func twoCommits(t *testing.T) (*MemStore, *clock) {
	t.Helper()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	for _, k := range []string{"a", "b"} {
		if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(k)}); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}
	return ms, ck
}

// The log is the table. Every one of these is a way a log can stop meaning what
// it says, and each must be reported as corruption rather than silently
// producing a table with the wrong rows in it — which is the failure nobody
// notices until it is in a report.
func TestCorruptLogIsDetected(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		corrupt func(ms *MemStore)
	}{
		{"version in the object disagrees with its name", func(ms *MemStore) {
			ms.Overwrite(logKey(2), []byte(`{"version":7,"parent":1,"ts":1,"writer":"x","add":[],"remove":[],"op":"append"}`))
		}},
		{"parent does not follow the previous version", func(ms *MemStore) {
			ms.Overwrite(logKey(2), []byte(`{"version":2,"parent":99,"ts":1,"writer":"x","add":[],"remove":[],"op":"append"}`))
		}},
		{"removes a file that is not live", func(ms *MemStore) {
			ms.Overwrite(logKey(2), []byte(`{"version":2,"parent":1,"ts":1,"writer":"x","add":[],"remove":["data/never-existed.avro"],"op":"append"}`))
		}},
		{"adds a file outside data/", func(ms *MemStore) {
			ms.Overwrite(logKey(2), []byte(`{"version":2,"parent":1,"ts":1,"writer":"x","add":[{"path":"../escape.avro","rows":1,"minKey":"a","maxKey":"a","bytes":1}],"remove":[],"op":"append"}`))
		}},
		{"adds the same file twice", func(ms *MemStore) {
			ms.Overwrite(logKey(2), []byte(`{"version":2,"parent":1,"ts":1,"writer":"x","add":[{"path":"data/dup.avro","rows":1,"minKey":"a","maxKey":"a","bytes":1},{"path":"data/dup.avro","rows":1,"minKey":"a","maxKey":"a","bytes":1}],"remove":[],"op":"append"}`))
		}},
		{"not JSON at all", func(ms *MemStore) {
			ms.Overwrite(logKey(2), []byte(`{"version":`))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ms, ck := twoCommits(t)
			c.corrupt(ms)
			tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
			if err != nil {
				return // refused at open, which is also correct
			}
			if _, err := tb.Scan(ctx, ""); err == nil {
				t.Error("a corrupt log read back as a healthy table")
			}
		})
	}
}

// A log with a hole in it cannot be replayed: the versions after the hole
// depend on state the missing entry carried.
func TestLogGapIsDetected(t *testing.T) {
	ctx := context.Background()
	ms, ck := twoCommits(t)
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "c", Payload: []byte("c")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := ms.Delete(ctx, logKey(2)); err != nil {
		t.Fatalf("delete log entry: %v", err)
	}
	tb2, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		return
	}
	if _, err := tb2.Scan(ctx, ""); err == nil {
		t.Error("a log with a missing entry read back as a healthy table")
	}
}

// Travelling to a version that was never committed is a caller error, and has
// to be distinguishable from an empty result.
func TestSnapshotOfUnknownVersion(t *testing.T) {
	ms, ck := twoCommits(t)
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Snapshot(context.Background(), 404); !errors.Is(err, ErrNoVersion) {
		t.Errorf("err = %v, want ErrNoVersion", err)
	}
}

// Writes are validated before anything is committed. An empty key would become
// an object no reader could ask for by name.
func TestWritesAreValidated(t *testing.T) {
	ctx := context.Background()
	ms, ck := twoCommits(t)
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx); err == nil {
		t.Error("Put with no rows was accepted")
	}
	if _, err := tb.Put(ctx, Row{Key: "", Payload: []byte("x")}); err == nil {
		t.Error("Put with an empty key was accepted")
	}
	if _, err := tb.Put(ctx, Row{Key: "ok", Payload: []byte("x")}, Row{Key: "", Payload: nil}); err == nil {
		t.Error("Put with one empty key among several was accepted")
	}
	if _, err := tb.Delete(ctx, "a", ""); err == nil {
		t.Error("Delete with an empty key was accepted")
	}
}

// An attempt count below one would mean never trying at all.
func TestMaxAttemptsIsClamped(t *testing.T) {
	ctx := context.Background()
	tb, err := Open(NewMemStore(), "acme", "orders", WithMaxAttempts(0), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Errorf("put with maxAttempts 0: %v", err)
	}
}

// Compaction races a concurrent commit: the files it planned to remove are
// gone by the time it commits, so it re-plans. If every plan loses, it must
// report that rather than loop.
func TestCompactGivesUpWhenEveryPlanLoses(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0), WithMaxAttempts(2))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := range 6 {
		if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// Every commit the compaction attempts loses its race.
	ms.FailPutIfAbsent = func(key string) error {
		if strings.Contains(key, "_log/") {
			return ErrExists
		}
		return nil
	}
	if _, err := tb.Compact(ctx); err == nil {
		t.Error("compaction reported success although every plan lost its race")
	}
}

// A commit cancelled while it waits to retry must return the caller's error.
func TestCompactHonoursContextWhileRetrying(t *testing.T) {
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0), WithMaxAttempts(5))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	for i := range 6 {
		if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	ms.FailPutIfAbsent = func(key string) error {
		if strings.Contains(key, "_log/") {
			return ErrExists
		}
		return nil
	}
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := tb.Compact(dead); err == nil {
		t.Error("compaction on a cancelled context reported success")
	}
}

// Changes are ordered by position within a commit, then by key, so a consumer
// replaying them applies them in the order they were written.
func TestChangesSinceOrdering(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx,
		Row{Key: "z", Payload: []byte("1")},
		Row{Key: "a", Payload: []byte("2")},
		Row{Key: "m", Payload: []byte("3")},
	); err != nil {
		t.Fatalf("put: %v", err)
	}
	s, err := tb.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	ch, err := s.ChangesSince(ctx, "", 0)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(ch) != 3 {
		t.Fatalf("changes = %d, want 3", len(ch))
	}
	for i := 1; i < len(ch); i++ {
		if ch[i-1].Ord > ch[i].Ord {
			t.Errorf("changes out of order at %d: %+v", i, ch)
		}
	}
	// Nothing has happened since the latest version.
	after, err := s.ChangesSince(ctx, "", s.Version())
	if err != nil {
		t.Fatalf("changes after head: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("changes after head = %v, want none", after)
	}
	// A prefix that matches nothing yields nothing.
	none, err := s.ChangesSince(ctx, "zzz-no-such-prefix", 0)
	if err != nil {
		t.Fatalf("changes with prefix: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("changes for an unmatched prefix = %v", none)
	}
}

var _ = time.Second
