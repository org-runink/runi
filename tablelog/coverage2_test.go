package tablelog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a hand-wound clock, so a test can be older than the retention
// window without sleeping through it.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Millisecond) // every reading is distinct
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// A checkpoint every commit exercises writing one, reading it back, and
// choosing between several — the path that makes opening a long log cheap, and
// the one most likely to rot unnoticed because the table still works without it.
func TestCheckpointsAreWrittenAndUsed(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithCheckpointInterval(1), WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte{byte(i)}}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if err := tb.LastCheckpointError(); err != nil {
		t.Errorf("checkpoint error: %v", err)
	}
	keys, err := ms.List(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	cps := 0
	for _, k := range keys {
		if strings.Contains(k, "checkpoint") {
			cps++
		}
	}
	if cps == 0 {
		t.Fatal("no checkpoint was written despite an interval of 1")
	}

	// A fresh table must load from the checkpoint and see the same rows.
	tb2, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := tb2.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 6 {
		t.Errorf("rows after checkpoint load = %d, want 6", len(got))
	}
}

// A corrupt checkpoint must not make the table unreadable: the log is the
// truth and the checkpoint is only a shortcut, so a bad one is skipped.
func TestCorruptCheckpointFallsBackToTheLog(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithCheckpointInterval(1), WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte{byte(i)}}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	keys, _ := ms.List(ctx, "")
	wrecked := 0
	for _, k := range keys {
		if strings.Contains(k, "checkpoint") {
			ms.Overwrite(k, []byte("{ not a checkpoint"))
			wrecked++
		}
	}
	if wrecked == 0 {
		t.Skip("no checkpoint to corrupt")
	}
	tb2, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open with a corrupt checkpoint: %v", err)
	}
	got, err := tb2.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan with a corrupt checkpoint: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("rows = %d, want 4 — the log should have been replayed", len(got))
	}
}

// Vacuum only removes what no retained version needs, and only once it is
// older than the window. Winding the clock forward is the only way to see the
// deleting half of it.
func TestVacuumDeletesOldFiles(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders",
		WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := tb.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("compact: %v", err)
	}
	// An orphan: a well-formed data file that no commit ever referenced, as a
	// writer that crashed between writing its file and committing would leave.
	// The name has to carry a real creation time, because that is the only
	// thing Vacuum can judge an unreferenced file by.
	orphan := fmt.Sprintf("data/%016x%032x.avro", ck.now().UnixMicro(), 0)
	if err := ms.PutIfAbsent(ctx, "tables/acme/orders/"+orphan, []byte("not really avro")); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	// A file under data/ this package did not name, which must be left alone.
	if err := ms.PutIfAbsent(ctx, "tables/acme/orders/data/not-ours.txt", []byte("hands off")); err != nil {
		t.Fatalf("foreign: %v", err)
	}

	ck.advance(48 * time.Hour)
	res, err := tb.Vacuum(ctx, time.Hour)
	if err != nil {
		t.Fatalf("vacuum: %v", err)
	}
	if len(res.Deleted) == 0 {
		t.Error("vacuum deleted nothing after the window passed")
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "not-ours") {
		t.Errorf("skipped = %v, want only the foreign file", res.Skipped)
	}
	var sawOrphan bool
	for _, d := range res.Deleted {
		if d == orphan {
			sawOrphan = true
		}
	}
	if !sawOrphan {
		t.Errorf("the orphaned data file was not collected: deleted = %v", res.Deleted)
	}
	// The table still reads correctly afterwards.
	got, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan after vacuum: %v", err)
	}
	if len(got) != 8 {
		t.Errorf("rows after vacuum = %d, want 8", len(got))
	}
}

// When every attempt loses the race the commit has to give up and say so,
// rather than retrying forever or reporting a success it did not achieve.
func TestCommitGivesUpAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithMaxAttempts(3), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ms.FailPutIfAbsent = func(key string) error {
		if strings.Contains(key, "_log/") {
			return ErrExists
		}
		return nil
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err == nil {
		t.Fatal("put succeeded although every log write lost its race")
	}
}

// An ambiguous log write — the object store failed, but may have applied it —
// must not be reported as a clean failure, because retrying it blindly could
// write the same version twice.
func TestCommitSurfacesAmbiguousWriteFailures(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ms.FailPutIfAbsent = func(key string) error {
		if strings.Contains(key, "_log/") {
			return errors.New("connection reset after send")
		}
		return nil
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err == nil {
		t.Fatal("an ambiguous log failure was reported as success")
	}
}

// Compaction rewrites many small files into fewer, and must leave the table
// reading exactly as it did before.
func TestCompactionPreservesContents(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now), WithCompaction(1<<20, 0.0))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	want := map[string]string{}
	for i := 0; i < 10; i++ {
		k := string(rune('a' + i))
		v := strings.Repeat(k, i+1)
		want[k] = v
		if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(v)}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if _, err := tb.Delete(ctx, "a", "b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	delete(want, "a")
	delete(want, "b")

	before, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan before: %v", err)
	}
	res, err := tb.Compact(ctx)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	_ = res
	after, err := tb.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan after: %v", err)
	}
	if len(before) != len(after) || len(after) != len(want) {
		t.Fatalf("rows before=%d after=%d want=%d", len(before), len(after), len(want))
	}
	for _, r := range after {
		if string(r.Payload) != want[r.Key] {
			t.Errorf("%s = %q, want %q", r.Key, r.Payload, want[r.Key])
		}
	}
	// Compacting an already-compact table is a no-op, not an error.
	if _, err := tb.Compact(ctx); err != nil {
		t.Errorf("second compact: %v", err)
	}
}

// Time travel: an older version must still read as it did, which is the whole
// point of keeping the log.
func TestSnapshotReadsHistory(t *testing.T) {
	ctx := context.Background()
	ck := newClock()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders", WithClock(ck.now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	v1, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("first")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "k", Payload: []byte("second")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	s, err := tb.Snapshot(ctx, v1)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	rec, ok, err := s.Get(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("snapshot get: ok=%v err=%v", ok, err)
	}
	if string(rec.Payload) != "first" {
		t.Errorf("snapshot value = %q, want %q", rec.Payload, "first")
	}
	ch, err := s.ChangesSince(ctx, "", 0)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(ch) == 0 {
		t.Error("no changes reported since version 0")
	}
	// A version that was never written cannot be travelled to.
	if _, err := tb.Snapshot(ctx, 9999); err == nil {
		t.Error("snapshot of a future version succeeded")
	}
}

var _ io.Reader = strings.NewReader("")
