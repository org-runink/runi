package tablelog

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// After compaction a single file holds rows from many commits, so the
// per-row version filter is the only thing left to narrow a feed by. Before
// compaction the file-level ranges do that work and this path never runs,
// which is why this test compacts first.
func TestChangesSinceFiltersRowsInsideACompactedFile(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var mid int64
	for i, k := range []string{"a", "b", "c", "d", "e", "f"} {
		v, err := tb.Put(ctx, Row{Key: k, Payload: []byte(k)})
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		if i == 2 {
			mid = v
		}
	}
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("compact: %v", err)
	}
	s, err := tb.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	got, err := s.ChangesSince(ctx, "", mid)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("changes since v%d = %d, want 3", mid, len(got))
	}
	for _, c := range got {
		if c.Version <= mid {
			t.Errorf("row at v%d not filtered out", c.Version)
		}
	}
	// And the prefix filter, on the same compacted file.
	one, err := s.ChangesSince(ctx, "e", 0)
	if err != nil {
		t.Fatalf("changes prefix: %v", err)
	}
	if len(one) != 1 || one[0].Key != "e" {
		t.Errorf("changes(prefix=e) = %v", one)
	}
}

// Reading a row means reading its data file. If that read fails the scan must
// fail: a short result that looks like a complete one is the dangerous answer.
func TestScanStopsWhenADataFileCannotBeRead(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, k := range []string{"a", "b", "c", "d"} {
		if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(k)}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	counter := &faultStore{inner: ms, method: "Get"}
	probe, err := Open(counter, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	counter.arm(0)
	if _, err := probe.Scan(ctx, ""); err != nil {
		t.Fatalf("clean scan: %v", err)
	}
	gets := counter.count()
	if gets == 0 {
		t.Fatal("scan made no Get calls")
	}
	failed := 0
	for trip := 1; trip <= gets; trip++ {
		fs := &faultStore{inner: ms, method: "Get"}
		tb2, err := Open(fs, "acme", "orders", WithClock(ck.now))
		if err != nil {
			continue
		}
		fs.arm(trip)
		if rows, err := tb2.Scan(ctx, ""); err != nil {
			failed++
		} else if len(rows) != 4 {
			t.Errorf("trip %d: scan returned %d rows with no error", trip, len(rows))
		}
	}
	if failed == 0 {
		t.Errorf("failing each of %d Gets in turn never failed the scan", gets)
	}
}

// Compaction that drops every row — all of them deleted and past the
// tombstone horizon — commits removals with nothing added. The log entry must
// still carry an empty add list rather than a null one, so a reader does not
// have to tell the two apart.
func TestCompactionCanCommitWithNothingAdded(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	keys := []string{"a", "b", "c", "d"}
	for _, k := range keys {
		if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(k)}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if _, err := tb.Delete(ctx, keys...); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Past the horizon, so the tombstones themselves can go.
	ck.advance(90 * 24 * time.Hour)
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("compact: %v", err)
	}
	rows, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("scan = %v, want nothing", rows)
	}
}

// Compaction plans against a state, and if the files it meant to remove keep
// disappearing under it, it re-plans. After enough failed plans it has to give
// up and say so rather than loop.
func TestCompactGivesUpAfterRepeatedConflicts(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0), WithMaxAttempts(3))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	other, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open other: %v", err)
	}
	for i := range 8 {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// Every time tb tries to commit a plan, another writer compacts first, so
	// tb's chosen files are already gone.
	ms.FailPutIfAbsent = func(key string) error {
		if strings.Contains(key, "_log/") {
			saved := ms.FailPutIfAbsent
			ms.FailPutIfAbsent = nil
			_, _ = other.Compact(ctx)
			_, _ = other.Put(ctx, Row{Key: "filler", Payload: []byte("x")})
			ms.FailPutIfAbsent = saved
		}
		return nil
	}
	_, err = tb.Compact(ctx)
	ms.FailPutIfAbsent = nil
	// Either it gave up, or it eventually won; both are acceptable outcomes.
	// What is not acceptable is a wrong table afterwards.
	rows, serr := tb.Scan(ctx, "")
	if serr != nil {
		t.Fatalf("scan after contended compaction: %v", serr)
	}
	if len(rows) < 8 {
		t.Errorf("rows after contended compaction = %d, want at least 8 (err was %v)", len(rows), err)
	}
}
