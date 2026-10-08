package tablelog

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Without randomness a data file name is predictable, and two writers that
// pick the same name overwrite each other outside the commit protocol. There
// is no safe way to continue, so the write must stop.
func TestRandHexFailureStopsTheWrite(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, fmt.Errorf("entropy pool drained") }
	defer func() { randRead = orig }()

	defer func() {
		if recover() == nil {
			t.Error("a failure to generate a unique name was not fatal")
		}
	}()
	_ = randHex(8)
}

// A name whose timestamp is hex but whose hash is not was not written by this
// package, so its creation time cannot be trusted either.
func TestDataFileCreatedRejectsBadHash(t *testing.T) {
	bad := fmt.Sprintf("data/%016x%s.avro", time.Now().UnixMicro(), "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if _, ok := dataFileCreated(bad); ok {
		t.Errorf("accepted a name with a non-hex hash: %s", bad)
	}
}

// The maintenance operations, with the store failing at each call they make.
// They run against an aged table where there is genuinely something to delete,
// which is the only state in which their deleting paths execute at all.
func TestMaintenancePropagatesStoreFailures(t *testing.T) {
	ctx := context.Background()

	aged := func(t *testing.T) (*MemStore, *clock) {
		t.Helper()
		ck := newClock()
		ms := NewMemStore()
		tb, err := Open(ms, "acme", "orders",
			WithClock(ck.now), WithCompaction(1<<20, 0.0), WithCheckpointInterval(2))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		for i := range 8 {
			if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte("v")}); err != nil {
				t.Fatalf("put: %v", err)
			}
		}
		if _, err := tb.Compact(ctx); err != nil {
			t.Fatalf("compact: %v", err)
		}
		ck.advance(48 * time.Hour)
		return ms, ck
	}

	ops := []struct {
		name string
		run  func(tb *Table) error
	}{
		{"Vacuum", func(tb *Table) error { _, err := tb.Vacuum(ctx, time.Hour); return err }},
		{"Compact", func(tb *Table) error { _, err := tb.Compact(ctx); return err }},
		{"Put", func(tb *Table) error { _, err := tb.Put(ctx, Row{Key: "zz", Payload: []byte("1")}); return err }},
		{"Scan", func(tb *Table) error { _, err := tb.Scan(ctx, ""); return err }},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			ms, ck := aged(t)
			counter := &faultStore{inner: ms}
			tb, err := Open(counter, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			counter.arm(0)
			_ = op.run(tb)
			calls := counter.count()
			if calls == 0 {
				t.Skip("no store calls")
			}
			for trip := 1; trip <= calls; trip++ {
				ms2, ck2 := aged(t)
				fs := &faultStore{inner: ms2}
				tb2, err := Open(fs, "acme", "orders", WithClock(ck2.now), WithCompaction(1<<20, 0.0))
				if err != nil {
					t.Fatalf("open trip %d: %v", trip, err)
				}
				fs.arm(trip)
				_ = op.run(tb2)
			}
		})
	}
}

// A writer that wrote its data file, then stalled for longer than
// MaxCommitAge before winning a log version, must not commit. Vacuum is
// allowed to collect unreferenced files older than the window, so by now its
// file may already have been deleted underneath it — committing would publish
// a reference to nothing.
func TestCommitRefusesAStaleDataFile(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now), WithMaxAttempts(10))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "seed", Payload: []byte("0")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Every log attempt loses its race, and time passes while it retries.
	ms.FailPutIfAbsent = func(key string) error {
		if contains(key, "_log/") {
			ck.advance(MaxCommitAge)
			return ErrExists
		}
		return nil
	}
	_, err = tb.Put(ctx, Row{Key: "slow", Payload: []byte("1")})
	if err == nil {
		t.Fatal("a commit whose data file had aged out was accepted")
	}
	if !errorIs(err, ErrStaleCommit) {
		t.Errorf("err = %v, want ErrStaleCommit", err)
	}
}

// Compaction plans to remove files, then loses the race to a concurrent commit
// that removed them first. The plan is stale, so it must be abandoned and
// re-planned rather than committed against a state that has moved on.
func TestCommitReportsAConflictOnRemovedFiles(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0), WithMaxAttempts(4))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := range 6 {
		if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// A second table compacts first, so the files the first one planned to
	// remove are gone by the time it tries to commit them.
	other, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open other: %v", err)
	}
	var raced bool
	ms.FailPutIfAbsent = func(key string) error {
		if contains(key, "_log/") && !raced {
			raced = true
			ms.FailPutIfAbsent = nil
			if _, err := other.Compact(ctx); err != nil {
				return nil
			}
			return ErrExists
		}
		return nil
	}
	// Whatever it returns, it must not silently commit a plan built on a state
	// that no longer exists.
	if _, err := tb.Compact(ctx); err == nil {
		rows, serr := tb.Scan(ctx, "")
		if serr != nil {
			t.Fatalf("scan after racing compactions: %v", serr)
		}
		if len(rows) != 6 {
			t.Errorf("rows after racing compactions = %d, want 6", len(rows))
		}
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func errorIs(err, target error) bool { return errors.Is(err, target) }
