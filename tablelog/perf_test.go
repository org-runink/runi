// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

// The tests here are about cost, not about results. A versioned table whose
// commit re-reads its own log, or whose scan rebuilds a compressor per file,
// is still correct -- it just gets slower the more it has been used, which is
// the kind of regression that passes every other test in this package and
// shows up as a production timeout. Each one states a cost property and fails
// when it stops holding.

// countingStore counts the Store calls underneath a table, by kind of key.
// The counters are atomic because reads fan out across goroutines.
type countingStore struct {
	inner   Store
	listed  atomic.Int64
	logRead atomic.Int64
	objRead atomic.Int64
}

func (c *countingStore) Put(ctx context.Context, k string, r io.Reader, n int64, ct string) error {
	return c.inner.Put(ctx, k, r, n, ct)
}
func (c *countingStore) PutIfAbsent(ctx context.Context, k string, d []byte) error {
	return c.inner.PutIfAbsent(ctx, k, d)
}
func (c *countingStore) Delete(ctx context.Context, k string) error { return c.inner.Delete(ctx, k) }
func (c *countingStore) List(ctx context.Context, p string) ([]string, error) {
	c.listed.Add(1)
	return c.inner.List(ctx, p)
}
func (c *countingStore) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	c.objRead.Add(1)
	if strings.Contains(k, "/_log/") {
		c.logRead.Add(1)
	}
	return c.inner.Get(ctx, k)
}

// Discovering the head is a LIST of the whole log, so a commit that always
// does it costs more the longer the table has been in use: the hundredth
// commit pays for the ninety-nine before it. A writer that already holds the
// head does not need to ask -- it guesses the next version and lets the atomic
// create be the judge -- so after the first commit a run of appends must list
// nothing at all.
func TestCommitDoesNotReReadTheLog(t *testing.T) {
	ctx := context.Background()
	cs := &countingStore{inner: NewMemStore()}
	tb, err := Open(cs, "acme", "events")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "k0", Payload: []byte("v")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	afterFirst := cs.listed.Load()
	for i := 1; i < 50; i++ {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if n := cs.listed.Load(); n != afterFirst {
		t.Errorf("49 further commits listed the store %d times; a writer at the head lists nothing",
			n-afterFirst)
	}
	// And the guess never publishes a wrong version.
	if v, err := tb.Version(ctx); err != nil || v != 50 {
		t.Errorf("Version() = %d, %v; want 50", v, err)
	}
}

// Guessing the next version is only safe because the atomic create decides it.
// When another writer took that version first, the guess must lose, fall back
// to reading the head, and commit after it -- not overwrite it, not skip it.
func TestASpeculativeCommitThatLosesFallsBackToTheHead(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	a, err := Open(ms, "acme", "events")
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	b, err := Open(ms, "acme", "events")
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	if _, err := a.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Fatalf("a put: %v", err)
	}
	// b takes v2 while a still believes it is at the head.
	if v, err := b.Put(ctx, Row{Key: "b", Payload: []byte("2")}); err != nil || v != 2 {
		t.Fatalf("b put = %d, %v; want v2", v, err)
	}
	// a's guess at v2 must lose and land on v3.
	if v, err := a.Put(ctx, Row{Key: "c", Payload: []byte("3")}); err != nil || v != 3 {
		t.Fatalf("a's second put = %d, %v; want v3", v, err)
	}
	// Nothing was lost or overwritten.
	got, err := a.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	want := map[string]string{"a": "1", "b": "2", "c": "3"}
	if len(got) != len(want) {
		t.Fatalf("scan returned %d rows, want %d", len(got), len(want))
	}
	for _, r := range got {
		if want[r.Key] != string(r.Payload) {
			t.Errorf("%s = %q, want %q", r.Key, r.Payload, want[r.Key])
		}
	}
}

// A handle that has lost a race stops guessing until it has won a version
// again, so a contended table does not pay for a wrong guess every commit.
func TestAfterLosingAGuessTheNextCommitReadsTheHead(t *testing.T) {
	ctx := context.Background()
	cs := &countingStore{inner: NewMemStore()}
	a, err := Open(cs, "acme", "events")
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	b, err := Open(cs, "acme", "events")
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	if _, err := a.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Fatalf("a put: %v", err)
	}
	if _, err := b.Put(ctx, Row{Key: "b", Payload: []byte("2")}); err != nil {
		t.Fatalf("b put: %v", err)
	}
	before := cs.listed.Load()
	if _, err := a.Put(ctx, Row{Key: "c", Payload: []byte("3")}); err != nil {
		t.Fatalf("a put 2: %v", err)
	}
	if cs.listed.Load() == before {
		t.Error("a commit that lost its guess did not re-read the head")
	}
}

// Catching up a cold handle must read each log entry it replays once. Reading
// from the newest checkpoint is the point of writing them: a table with a
// hundred commits and a checkpoint at ninety replays ten entries, not a
// hundred.
func TestAColdReadReplaysOnlyPastTheCheckpoint(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "events", WithCheckpointInterval(10))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 25; i++ {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%02d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	cs := &countingStore{inner: ms}
	cold, err := Open(cs, "acme", "events")
	if err != nil {
		t.Fatalf("open cold: %v", err)
	}
	rows, err := cold.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(rows) != 25 {
		t.Fatalf("scan returned %d rows, want 25", len(rows))
	}
	if n := cs.logRead.Load(); n != 5 {
		t.Errorf("a cold read of a 25-commit table with a checkpoint at v20 read %d log entries, want 5", n)
	}
}

// Every reader of a merge sorts its result by key. Map iteration order is
// deliberately random, so handing the sort the merge's map handed it its worst
// case on every scan. Winners come back in the order their keys were first
// read instead, which for a table written in key order -- an event log, a time
// series, anything with an ascending id -- is already sorted.
func TestMergeReturnsWinnersInFirstAppearanceOrder(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t)
	for i := 0; i < 20; i++ {
		rows := make([]Row, 5)
		for j := range rows {
			rows[j] = Row{Key: fmt.Sprintf("k%03d", i*5+j), Payload: []byte("v")}
		}
		if _, err := tb.Put(ctx, rows...); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	snap, err := tb.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	winners, err := tb.merge(ctx, snap.s.sortedFiles(), func(string) bool { return true })
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(winners) != 100 {
		t.Fatalf("merge returned %d winners, want 100", len(winners))
	}
	if !slices.IsSortedFunc(winners, func(a, b row) int { return strings.Compare(a.key, b.key) }) {
		t.Error("a table written in ascending key order did not merge back in key order, " +
			"so every scan of it now pays for a full sort")
	}
	// The order is a free extra, not a substitute for the merge being right:
	// one row per key, and the winner.
	seen := map[string]bool{}
	for _, w := range winners {
		if seen[w.key] {
			t.Fatalf("key %s appeared twice in the merge", w.key)
		}
		seen[w.key] = true
	}
}

// The same, with the keys overwritten out of order: the winner must still be
// the newest row, wherever it was read.
func TestMergeKeepsTheNewestRowOfEachKey(t *testing.T) {
	ctx := context.Background()
	r := rand.New(rand.NewPCG(7, 11))
	tb, _ := newTable(t)
	want := map[string]string{}
	for i := 0; i < 40; i++ {
		rows := make([]Row, 3)
		for j := range rows {
			k := fmt.Sprintf("k%d", r.IntN(15))
			v := fmt.Sprintf("v%d", i*3+j)
			rows[j] = Row{Key: k, Payload: []byte(v)}
			want[k] = v
		}
		if _, err := tb.Put(ctx, rows...); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	got, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("scan returned %d rows, want %d", len(got), len(want))
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Key < got[j].Key }) {
		t.Error("Scan did not return rows sorted by key")
	}
	for _, rec := range got {
		if want[rec.Key] != string(rec.Payload) {
			t.Errorf("%s = %q, want %q", rec.Key, rec.Payload, want[rec.Key])
		}
	}
}

// A scan reads each live file once. Re-reading one -- per key, per prefix,
// per anything -- is the difference between a scan that costs what the table
// holds and one that costs what the caller asked for times what the table
// holds.
func TestAScanReadsEachLiveFileOnce(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "events")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const files = 12
	for i := 0; i < files; i++ {
		rows := make([]Row, 5)
		for j := range rows {
			rows[j] = Row{Key: fmt.Sprintf("k%03d", i*5+j), Payload: []byte("v")}
		}
		if _, err := tb.Put(ctx, rows...); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	cs := &countingStore{inner: ms}
	reader, err := Open(cs, "acme", "events")
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	if _, err := reader.Scan(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}
	dataReads := cs.objRead.Load() - cs.logRead.Load()
	// The non-log reads are the checkpoint, the _last hint and the data files.
	if dataReads > files+2 {
		t.Errorf("a scan of %d live files made %d non-log reads; it is reading files more than once",
			files, dataReads)
	}
}

// allocPerOp returns the bytes allocated per call of f.
// allocPerOp returns the bytes allocated per call of f.
//
// The garbage collector is off for the measurement. Reuse here is sync.Pool
// reuse, and a Pool is emptied at every GC — so with the collector running,
// what the number measures is partly how often the collector happened to run,
// which under -race or on a loaded machine is a different number every time.
// Off, it measures the thing the test is about: whether the work builds its
// state again on every call.
func allocPerOp(f func(), n int) uint64 {
	var m0, m1 runtime.MemStats
	// One P and no collector for the measurement. Reuse here is sync.Pool
	// reuse, and a Pool is emptied at every GC and keeps its warm entry in a
	// per-P slot no other P can take — so with the collector running and the
	// goroutine free to migrate, what the number measures is partly how often
	// the collector ran and how often the scheduler moved us, which under
	// -race or on a loaded machine is a different number every time. Pinned,
	// it measures the thing the test is about: whether the work builds its
	// state again on every call.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	f() // warm: pools, first-time growth
	runtime.GC()
	runtime.ReadMemStats(&m0)
	for i := 0; i < n; i++ {
		f()
	}
	runtime.ReadMemStats(&m1)
	return (m1.TotalAlloc - m0.TotalAlloc) / uint64(n)
}

// Catching up a cold handle used to clone the whole live file set once per log
// entry it replayed, which makes replaying n commits cost n²/2 map inserts:
// invisible on a short log, ruinous on a long one, and exactly the shape of
// cost that turns into a production timeout months after it was written. The
// replay owns its copy and applies into it, so the cost is linear.
//
// The budget is the gap between those two shapes, not a number to tune: at 400
// commits the quadratic version allocates tens of megabytes and the linear one
// about a megabyte.
func TestReplayingTheLogIsLinearInItsLength(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	// No checkpoints: this is about the replay itself, not about skipping it.
	tb, err := Open(ms, "acme", "events", WithCheckpointInterval(0))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const commits = 400
	for i := 0; i < commits; i++ {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%04d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	const budget = 8 << 20 // cloning per entry was tens of MiB
	got := allocPerOp(func() {
		cold, err := Open(ms, "acme", "events")
		if err != nil {
			t.Fatal(err)
		}
		v, err := cold.Version(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if v != commits {
			t.Fatalf("version = %d, want %d", v, commits)
		}
	}, 10)
	t.Logf("a cold read of a %d-commit log allocates %d bytes", commits, got)
	if got > budget {
		t.Errorf("replaying %d commits allocated %d bytes; budget is %d. The replay is "+
			"copying the whole file set per entry again", commits, got, budget)
	}
}

// Guessing the next version costs the first lost race, so a caller that
// cancels while a commit waits to retry only reaches the backoff on the
// second. It must still return the caller's error there rather than finish the
// write after the caller has gone.
func TestCommitStopsWhenTheCallerCancelsDuringBackoff(t *testing.T) {
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
			if n == 2 { // 1 was the guess; this one is inside the retry loop
				cancel()
			}
			return ErrExists
		}
		return nil
	}
	if _, err := tb.Put(cancelCtx, Row{Key: "x", Payload: []byte("1")}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
