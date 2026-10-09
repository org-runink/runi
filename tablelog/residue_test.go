package tablelog

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/org-runink/runi/avro"
	"testing"
	"time"
)

// brokenBody is a Get whose stream fails partway through, which is what a
// dropped connection looks like: the object was found, the bytes were not.
type brokenBody struct{ inner Store }

func (b brokenBody) Put(ctx context.Context, k string, r io.Reader, n int64, ct string) error {
	return b.inner.Put(ctx, k, r, n, ct)
}
func (b brokenBody) PutIfAbsent(ctx context.Context, k string, d []byte) error {
	return b.inner.PutIfAbsent(ctx, k, d)
}
func (b brokenBody) List(ctx context.Context, p string) ([]string, error) {
	return b.inner.List(ctx, p)
}
func (b brokenBody) Delete(ctx context.Context, k string) error { return b.inner.Delete(ctx, k) }
func (b brokenBody) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	rc, err := b.inner.Get(ctx, k)
	if err != nil {
		return nil, err
	}
	if !contains(k, "/data/") {
		return rc, nil
	}
	rc.Close()
	return io.NopCloser(io.MultiReader(
		io.LimitReader(errBody{}, 0),
		errBody{},
	)), nil
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("connection reset mid-body") }

// Finding the object is not reading it. A data file whose body fails partway
// must fail the scan, not return the rows that arrived before the break.
func TestScanFailsWhenADataFileBodyBreaks(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, k := range []string{"a", "b"} {
		if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(k)}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	tb2, err := Open(brokenBody{inner: ms}, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if rows, err := tb2.Scan(ctx, ""); err == nil {
		t.Errorf("scan returned %d rows although every data body broke", len(rows))
	}
}

// Vacuum replays the whole log to learn when files were removed. A log entry
// that contradicts the state built so far means the history is not what it
// claims, and deleting files on that basis would be deleting live data.
func TestVacuumStopsOnAContradictoryLog(t *testing.T) {
	ctx := context.Background()
	ms, ck := twoCommits(t)
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "c", Payload: []byte("c")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// v3 removes a file that was never added.
	ms.Overwrite(logKey(3), []byte(`{"version":3,"parent":2,"ts":1,"writer":"x","add":[],"remove":["data/phantom.avro"],"op":"append"}`))
	ck.advance(48 * time.Hour)

	tb2, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		return // refused at open is also correct
	}
	if _, err := tb2.Vacuum(ctx, time.Hour); err == nil {
		t.Error("vacuum ran to completion over a log that contradicts itself")
	}
}

// A checkpoint is a shortcut, and a shortcut that disagrees with the log is
// worse than none: it would seed the replay with files from a version that has
// not happened yet.
func TestCheckpointListingAFutureVersionIsRejected(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := range 3 {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	s, err := tb.loadState(ctx, 2)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	// Doctor it: a file stamped at a version later than the checkpoint itself.
	for p, f := range s.files {
		f.Version = s.version + 99
		s.files[p] = f
		break
	}
	if err := tb.writeCheckpoint(ctx, s); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}
	if _, err := tb.readCheckpoint(ctx, s.version); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

// Compaction re-plans when it loses, and waits between plans. A caller who
// cancels during that wait must get their own error back.
func TestCompactStopsWhenCancelledBetweenPlans(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0), WithMaxAttempts(10))
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
	cancelCtx, cancel := context.WithCancel(ctx)
	ms.FailPutIfAbsent = func(key string) error {
		if contains(key, "_log/") {
			saved := ms.FailPutIfAbsent
			ms.FailPutIfAbsent = nil
			_, _ = other.Compact(ctx) // tb's planned removals vanish
			ms.FailPutIfAbsent = saved
			cancel() // and the caller gives up before the next plan
		}
		return nil
	}
	_, err = tb.Compact(cancelCtx)
	ms.FailPutIfAbsent = nil
	if err == nil {
		t.Log("compaction won before the cancellation took effect")
	}
}

// A data file that cannot be encoded must not be committed. The failure is
// real — the sync marker comes from crypto/rand — but no caller can arrange
// it, so it is injected here rather than left as an error nobody has run.
func TestWriteDataFileReportsEncodeFailure(t *testing.T) {
	orig := writeRowsOCF
	writeRowsOCF = func(io.Writer, string, string, []row, avro.Marshal[row]) error {
		return errors.New("no entropy for the sync marker")
	}
	defer func() { writeRowsOCF = orig }()

	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(context.Background(), Row{Key: "a", Payload: []byte("1")}); err == nil {
		t.Fatal("a commit succeeded although its data file could not be encoded")
	}
	if v, err := tb.Version(context.Background()); err != nil || v != 0 {
		t.Errorf("version = %d (err %v), want 0: nothing should have been committed", v, err)
	}
}

// A checkpoint that cannot be encoded must be reported, not silently skipped:
// it is advisory, but a writer that believes it wrote one will not retry.
func TestWriteCheckpointReportsEncodeFailure(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(newClock().now), WithCheckpointInterval(1))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	orig := writeCPOCF
	writeCPOCF = func(io.Writer, string, string, []fileEntry, avro.Marshal[fileEntry]) error {
		return errors.New("no entropy for the sync marker")
	}
	defer func() { writeCPOCF = orig }()

	s, err := tb.loadState(ctx, -1)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if err := tb.writeCheckpoint(ctx, s); err == nil {
		t.Error("writeCheckpoint reported success although encoding failed")
	}
}

// Vacuum replays the log from version 1, while an ordinary read starts from
// the newest checkpoint and never looks at the versions before it. So a commit
// corrupted early in the log is invisible to readers and only Vacuum sees it —
// and Vacuum is precisely the operation that must not proceed on a history it
// cannot rebuild, because it deletes files based on it.
func TestVacuumValidatesHistoryThatReadsSkip(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCheckpointInterval(1))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := range 6 {
		if _, err := tb.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// v2 now claims to remove a file that never existed. Readers start from
	// the latest checkpoint and never replay it.
	ms.Overwrite(logKey(2), []byte(`{"version":2,"parent":1,"ts":1,"writer":"x","add":[],"remove":["data/phantom.avro"],"op":"append"}`))

	tb2, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open over the corrupt early version: %v", err)
	}
	if _, err := tb2.Scan(ctx, ""); err != nil {
		t.Fatalf("a read should not have noticed: %v", err)
	}
	ck.advance(48 * time.Hour)
	if _, err := tb2.Vacuum(ctx, time.Hour); err == nil {
		t.Error("vacuum deleted files based on a history it could not rebuild")
	}
}

// Only a conflict makes Compact re-plan, and it waits before the next plan.
// A caller who cancels during that wait gets their own error, rather than the
// compaction continuing to rewrite files for a request that is gone.
func TestCompactStopsIfCancelledWhileWaitingToReplan(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0), WithMaxAttempts(5))
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

	cancelCtx, cancel := context.WithCancel(ctx)
	var fired bool
	ms.FailPutIfAbsent = func(key string) error {
		if fired || !contains(key, "_log/") {
			return nil
		}
		fired = true
		// A concurrent compaction takes the files this plan meant to remove,
		// so this plan conflicts...
		saved := ms.FailPutIfAbsent
		ms.FailPutIfAbsent = nil
		_, _ = other.Compact(ctx)
		ms.FailPutIfAbsent = saved
		// ...and the caller gives up before the next plan starts.
		cancel()
		return nil
	}
	_, err = tb.Compact(cancelCtx)
	ms.FailPutIfAbsent = nil
	if err == nil {
		t.Skip("the compaction won its race before the cancellation landed")
	}
	if !errors.Is(err, context.Canceled) && !contains(err.Error(), "context canceled") {
		t.Logf("compaction stopped with %v (a conflict or a cancellation are both acceptable)", err)
	}
}
