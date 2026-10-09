package tablelog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A name whose timestamp half is not hex was not written here, so its creation
// time cannot be read and must not be guessed.
func TestDataFileCreatedRejectsNonHexTimestamp(t *testing.T) {
	bad := "data/" + strings.Repeat("z", 16) + strings.Repeat("0", 32) + ".avro"
	if _, ok := dataFileCreated(bad); ok {
		t.Errorf("accepted a non-hex timestamp: %s", bad)
	}
}

// A negative version is not a version. It is rejected before the store is
// touched, so a caller's arithmetic slip does not become a read.
func TestSnapshotRejectsNegativeVersion(t *testing.T) {
	ms, ck := twoCommits(t)
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Snapshot(context.Background(), -1); !errors.Is(err, ErrNoVersion) {
		t.Errorf("err = %v, want ErrNoVersion", err)
	}
}

// Scanning a prefix must skip the data files whose key range cannot contain it,
// which is the whole reason the ranges are recorded.
func TestSnapshotScanSkipsFilesOutsideThePrefix(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Separate commits so each lands in its own file with its own key range.
	for _, k := range []string{"alpha-1", "beta-1", "gamma-1", "delta-1"} {
		if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(k)}); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}
	s, err := tb.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	got, err := s.Scan(ctx, "beta")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 1 || got[0].Key != "beta-1" {
		t.Errorf("scan(beta) = %v, want just beta-1", got)
	}
	// A prefix beyond every file's range.
	none, err := s.Scan(ctx, "zzzz")
	if err != nil {
		t.Fatalf("scan zzzz: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("scan(zzzz) = %v, want none", none)
	}
}

// Changes before the requested version are skipped, which is what makes
// ChangesSince a feed rather than a full scan.
func TestChangesSinceSkipsOlderVersions(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var mid int64
	for i, k := range []string{"a", "b", "c", "d"} {
		v, err := tb.Put(ctx, Row{Key: k, Payload: []byte(k)})
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		if i == 1 {
			mid = v
		}
	}
	s, err := tb.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	got, err := s.ChangesSince(ctx, "", mid)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("changes since v%d = %d, want 2", mid, len(got))
	}
	for _, c := range got {
		if c.Version <= mid {
			t.Errorf("change at v%d should have been skipped (after=%d)", c.Version, mid)
		}
	}
	// A prefix that excludes some of them narrows it further.
	only, err := s.ChangesSince(ctx, "c", 0)
	if err != nil {
		t.Fatalf("changes with prefix: %v", err)
	}
	if len(only) != 1 || only[0].Key != "c" {
		t.Errorf("changes(prefix=c) = %v", only)
	}
}

// Objects in the log directory whose names this package did not write are not
// versions. Treating one as version 0, or failing on it, would make a stray
// file able to break the table.
func TestListVersionsIgnoresForeignNames(t *testing.T) {
	ctx := context.Background()
	ms, ck := twoCommits(t)
	for _, k := range []string{
		"tables/acme/orders/_log/00000000000000000000.json", // version zero
		"tables/acme/orders/_log/not-a-version-at-all.json",
		"tables/acme/orders/_log/000000000000000000xx.json", // right length, not a number
		"tables/acme/orders/_log/README.txt",
	} {
		if err := ms.PutIfAbsent(ctx, k, []byte(`{"version":0}`)); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	v, err := tb.Version(ctx)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != 2 {
		t.Errorf("version = %d, want 2 — a stray file was counted", v)
	}
	if rows, err := tb.Scan(ctx, ""); err != nil || len(rows) != 2 {
		t.Errorf("scan = %v (%d rows), err %v", rows, len(rows), err)
	}
}

// The checkpoint hint only moves forward. A writer that checkpoints an older
// version must leave a newer hint alone, or readers would replay from further
// back every time two writers disagree.
func TestCheckpointHintOnlyMovesForward(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCheckpointInterval(1))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := range 4 {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// Re-checkpoint an older state while the hint is already ahead.
	old, err := tb.loadState(ctx, 2)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if err := tb.writeCheckpoint(ctx, old); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}
	body, err := tb.readObject(ctx, tb.lastKey())
	if err != nil {
		t.Fatalf("read hint: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "4" {
		t.Errorf("hint = %q, want 4 — an older checkpoint moved it back", got)
	}
}

// A commit cancelled while waiting to retry returns the caller's error rather
// than finishing the write after the caller has gone.
func TestCommitStopsWhenTheCallerCancels(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithMaxAttempts(20), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "seed", Payload: []byte("0")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	var n int
	ms.FailPutIfAbsent = func(key string) error {
		if strings.Contains(key, "_log/") {
			n++
			if n == 1 {
				cancel() // the caller gives up while the commit backs off
			}
			return ErrExists
		}
		return nil
	}
	if _, err := tb.Put(cancelCtx, Row{Key: "x", Payload: []byte("1")}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// Vacuum reads the whole log to learn when files were removed. If a log entry
// cannot be read it must stop, not delete files on a partial history.
func TestVacuumStopsOnAnUnreadableLog(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := range 6 {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("compact: %v", err)
	}
	ck.advance(48 * time.Hour)

	// Which Get matters is not obvious — the first is the advisory checkpoint
	// hint, whose failure Vacuum is right to tolerate — so every Get it makes
	// is failed in turn. At least one of them is a log read, and failing that
	// one has to stop the vacuum rather than delete files on a partial history.
	counter := &faultStore{inner: ms, method: "Get"}
	probe, err := Open(counter, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	counter.arm(0)
	if _, err := probe.Vacuum(ctx, time.Hour); err != nil {
		t.Fatalf("clean vacuum: %v", err)
	}
	gets := counter.count()
	if gets == 0 {
		t.Fatal("vacuum made no Get calls: the sweep would prove nothing")
	}

	stopped := 0
	for trip := 1; trip <= gets; trip++ {
		fs := &faultStore{inner: ms, method: "Get"}
		tb2, err := Open(fs, "acme", "orders", WithClock(ck.now))
		if err != nil {
			continue
		}
		fs.arm(trip)
		if _, err := tb2.Vacuum(ctx, time.Hour); err != nil {
			stopped++
		}
	}
	if stopped == 0 {
		t.Errorf("failing each of %d Gets in turn never stopped the vacuum", gets)
	}
}
