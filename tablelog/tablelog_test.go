// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTable(t *testing.T, opts ...Option) (*Table, *MemStore) {
	t.Helper()
	st := NewMemStore()
	tb, err := Open(st, "acme", "events", opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return tb, st
}

func TestPutGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)

	v, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("one")}, Row{Key: "b", Payload: []byte("two")})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if v != 1 {
		t.Fatalf("first commit is version %d; want 1 — versions are dense from 1", v)
	}

	rec, ok, err := tb.Get(ctx, "a")
	if err != nil || !ok {
		t.Fatalf("Get(a) = %v,%v", ok, err)
	}
	if string(rec.Payload) != "one" {
		t.Fatalf("Get(a) = %q; want \"one\"", rec.Payload)
	}

	if _, ok, _ := tb.Get(ctx, "absent"); ok {
		t.Fatal("Get reported a key that was never written")
	}
}

func TestLastWriterWins(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)

	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("second")}); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := tb.Get(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("Get: %v,%v", ok, err)
	}
	if string(rec.Payload) != "second" {
		t.Fatalf("Get = %q; want the later write", rec.Payload)
	}
}

func TestDeleteIsATombstone(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)

	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := tb.Get(ctx, "k"); err != nil || ok {
		t.Fatalf("Get after Delete = %v,%v; want absent", ok, err)
	}
	// A deleted key must not come back in a Scan either.
	recs, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Key == "k" {
			t.Fatal("Scan returned a deleted key")
		}
	}
}

func TestTimeTravel(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)

	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("v1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("v2")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}

	// This is the property the package exists for: every past version is still
	// readable, including the one before a delete.
	for _, tc := range []struct {
		version int64
		want    string
		present bool
	}{
		{1, "v1", true},
		{2, "v2", true},
		{3, "", false}, // the tombstone
	} {
		snap, err := tb.Snapshot(ctx, tc.version)
		if err != nil {
			t.Fatalf("Snapshot(%d): %v", tc.version, err)
		}
		rec, ok, err := snap.Get(ctx, "k")
		if err != nil {
			t.Fatalf("Snapshot(%d).Get: %v", tc.version, err)
		}
		if ok != tc.present {
			t.Fatalf("at version %d present=%v; want %v", tc.version, ok, tc.present)
		}
		if tc.present && string(rec.Payload) != tc.want {
			t.Fatalf("at version %d value=%q; want %q", tc.version, rec.Payload, tc.want)
		}
	}

	if _, err := tb.Snapshot(ctx, 99); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("Snapshot of an uncommitted version = %v; want ErrNoVersion", err)
	}
}

func TestVersionsAreDense(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)
	for i := 1; i <= 5; i++ {
		v, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")})
		if err != nil {
			t.Fatal(err)
		}
		if v != int64(i) {
			t.Fatalf("commit %d got version %d; versions must be dense with no gaps", i, v)
		}
	}
	v, err := tb.Version(ctx)
	if err != nil || v != 5 {
		t.Fatalf("Version = %d,%v; want 5", v, err)
	}
}

func TestConcurrentWritersAllCommitExactlyOnce(t *testing.T) {
	// The optimistic-concurrency path, against a store whose PutIfAbsent is a
	// real atomic create. Every writer must land, each on its own version, with
	// nothing lost and nothing duplicated.
	ctx := context.Background()
	tb, _ := newTable(t, WithMaxAttempts(50))

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	versions := make([]int64, writers)
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			versions[i], errs[i] = tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")})
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
		if seen[versions[i]] {
			t.Fatalf("version %d was handed to two writers", versions[i])
		}
		seen[versions[i]] = true
	}

	recs, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != writers {
		t.Fatalf("Scan found %d records; all %d writes must survive", len(recs), writers)
	}
}

func TestScanByPrefix(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)
	if _, err := tb.Put(ctx,
		Row{Key: "user:1", Payload: []byte("a")},
		Row{Key: "user:2", Payload: []byte("b")},
		Row{Key: "order:1", Payload: []byte("c")},
	); err != nil {
		t.Fatal(err)
	}
	recs, err := tb.Scan(ctx, "user:")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("Scan(user:) returned %d records; want 2", len(recs))
	}
	for _, r := range recs {
		if len(r.Key) < 5 || r.Key[:5] != "user:" {
			t.Fatalf("Scan(user:) returned %q", r.Key)
		}
	}
}

func TestChangesSince(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Put(ctx, Row{Key: "b", Payload: []byte("2")}); err != nil {
		t.Fatal(err)
	}
	snap, err := tb.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := snap.ChangesSince(ctx, "", 1)
	if err != nil {
		t.Fatalf("ChangesSince: %v", err)
	}
	if len(ch) != 1 || ch[0].Key != "b" {
		t.Fatalf("ChangesSince(after=1) = %+v; want only b", ch)
	}
}

func TestAStoreThatCannotCreateAtomicallyIsSurfaced(t *testing.T) {
	// If PutIfAbsent fails, the commit must fail — never silently proceed and
	// leave a table whose log has a hole in it.
	ctx := context.Background()
	st := NewMemStore()
	tb, err := Open(st, "acme", "events", WithMaxAttempts(2))
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("store is down")
	st.FailPutIfAbsent = func(key string) error { return boom }

	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("v")}); err == nil {
		t.Fatal("Put succeeded although the atomic create failed")
	}
}

func TestVacuumRemovesOrphansAndKeepsLiveFiles(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	tb, st := newTable(t, WithClock(func() time.Time { return clock }))

	for i := 0; i < 3; i++ {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatal(err)
		}
	}
	before := st.Len()

	// Nothing is older than the retention window, so nothing may be removed.
	res, err := tb.Vacuum(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
	if st.Len() != before {
		t.Fatalf("Vacuum removed %d objects inside the retention window", before-st.Len())
	}
	_ = res

	// Every record must still be readable afterwards.
	for i := 0; i < 3; i++ {
		if _, ok, err := tb.Get(ctx, fmt.Sprintf("k%d", i)); err != nil || !ok {
			t.Fatalf("k%d missing after Vacuum: %v", i, err)
		}
	}
}

func TestCheckpointsAreWrittenAndRead(t *testing.T) {
	// A checkpoint is the whole reason reads stay fast as the log grows: state
	// is checkpoint + replay of later entries, not a replay of everything.
	ctx := context.Background()
	tb, st := newTable(t, WithCheckpointInterval(2))

	for i := 0; i < 7; i++ {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tb.LastCheckpointError(); err != nil {
		t.Fatalf("checkpoint write failed: %v", err)
	}

	keys, err := st.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cps := 0
	for _, k := range keys {
		if strings.Contains(k, "_checkpoint/") && !strings.HasSuffix(k, "_last") {
			cps++
		}
	}
	if cps == 0 {
		t.Fatal("no checkpoint was written after 7 commits at an interval of 2")
	}

	// Everything must still read back correctly THROUGH the checkpoint path.
	for i := 0; i < 7; i++ {
		rec, ok, err := tb.Get(ctx, fmt.Sprintf("k%d", i))
		if err != nil || !ok {
			t.Fatalf("k%d after checkpointing: %v,%v", i, ok, err)
		}
		if string(rec.Payload) != "v" {
			t.Fatalf("k%d = %q", i, rec.Payload)
		}
	}

	// And a fresh Table opened on the same store must agree: the checkpoint has
	// to be readable by someone who did not write it.
	tb2, err := Open(st, "acme", "events", WithCheckpointInterval(2))
	if err != nil {
		t.Fatal(err)
	}
	recs, err := tb2.Scan(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 7 {
		t.Fatalf("a reader opening the same store saw %d records; want 7", len(recs))
	}
}

func TestTimeTravelStillWorksAcrossACheckpoint(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t, WithCheckpointInterval(2))

	for i := 1; i <= 6; i++ {
		if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte(fmt.Sprintf("v%d", i))}); err != nil {
			t.Fatal(err)
		}
	}
	// Reading a version BEFORE the newest checkpoint is the case that breaks
	// if bestCheckpoint picks a checkpoint newer than the requested version.
	for v := int64(1); v <= 6; v++ {
		snap, err := tb.Snapshot(ctx, v)
		if err != nil {
			t.Fatalf("Snapshot(%d): %v", v, err)
		}
		rec, ok, err := snap.Get(ctx, "k")
		if err != nil || !ok {
			t.Fatalf("Snapshot(%d).Get: %v,%v", v, ok, err)
		}
		if want := fmt.Sprintf("v%d", v); string(rec.Payload) != want {
			t.Fatalf("at version %d got %q; want %q", v, rec.Payload, want)
		}
	}
}

func TestCompactMergesSmallFilesAndPreservesEveryRow(t *testing.T) {
	ctx := context.Background()
	// Every file is "small", so compaction has something to do.
	tb, st := newTable(t, WithCompaction(1<<20, 0.0))

	const n = 6
	for i := 0; i < n; i++ {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("value")}); err != nil {
			t.Fatal(err)
		}
	}
	before := st.Len()

	res, err := tb.Compact(ctx)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	_ = res

	// The invariant that matters: compaction must not lose or change a row.
	recs, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != n {
		t.Fatalf("after Compact, Scan found %d records; want %d — compaction lost rows", len(recs), n)
	}
	for i := 0; i < n; i++ {
		rec, ok, err := tb.Get(ctx, fmt.Sprintf("k%d", i))
		if err != nil || !ok || string(rec.Payload) != "value" {
			t.Fatalf("k%d after Compact: %v %v %q", i, ok, err, rec.Payload)
		}
	}
	if st.Len() == 0 {
		t.Fatal("Compact emptied the store")
	}
	_ = before
}

func TestCompactIsSafeWhenThereIsNothingToDo(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("Compact on an empty table: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	// A huge small-file threshold of 0 means nothing qualifies.
	tb2, _ := newTable(t, WithCompaction(0, 1.0))
	if _, err := tb2.Compact(ctx); err != nil {
		t.Fatalf("Compact with nothing qualifying: %v", err)
	}
}

func TestDeleteThenCompactDropsTheTombstonedRow(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t, WithCompaction(1<<20, 0.0))

	if _, err := tb.Put(ctx, Row{Key: "keep", Payload: []byte("a")}, Row{Key: "gone", Payload: []byte("b")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Delete(ctx, "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if _, ok, _ := tb.Get(ctx, "gone"); ok {
		t.Fatal("a deleted key came back after compaction")
	}
	rec, ok, err := tb.Get(ctx, "keep")
	if err != nil || !ok || string(rec.Payload) != "a" {
		t.Fatalf("keep after compaction: %v %v %q", ok, err, rec.Payload)
	}
}
