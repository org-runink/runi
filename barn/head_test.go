package barn

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// A table that has been in use for a while has thousands of log entries, and
// none are ever deleted. Finding the head by listing _log/ made every read pay
// for all of them; a reader that has a checkpoint to start from must list
// nothing and read only the log past it.
func TestAReadNeverListsTheLog(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	w, err := Open(ms, "acme", "events", WithCheckpointInterval(10), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 52; i++ {
		if _, err := w.Put(ctx, Row{Key: fmt.Sprintf("k%02d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if err := w.LastCheckpointError(); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	cs := &countingStore{inner: ms}
	r, err := Open(cs, "acme", "events", WithCheckpointInterval(10), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	recs, err := r.Scan(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(recs) != 52 {
		t.Fatalf("scan returned %d rows, want 52", len(recs))
	}
	if n := cs.listed.Load(); n != 0 {
		t.Errorf("a read listed the store %d times; with a checkpoint hint it lists nothing", n)
	}
	// v51 and v52 past the v50 checkpoint, the miss at v53, and the gap check at v54.
	if n := cs.logRead.Load(); n != 4 {
		t.Errorf("a read fetched %d log entries; want 4 (two past the checkpoint, the miss, the gap check)", n)
	}

	// Another writer commits; the same handle sees it from its cache, still
	// without listing.
	if _, err := w.Put(ctx, Row{Key: "late", Payload: []byte("v")}); err != nil {
		t.Fatalf("late put: %v", err)
	}
	if v, err := r.Version(ctx); err != nil || v != 53 {
		t.Fatalf("Version() = %d, %v; want 53", v, err)
	}
	if n := cs.listed.Load(); n != 0 {
		t.Errorf("following the head listed the store %d times", n)
	}
}

// A hole in the log can only be made from outside this package. It must not be
// read as the head: the next commit would take the missing version and splice
// the entries after it back into history.
func TestAGapInTheLogIsCorruptNotTheHead(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	w, err := Open(ms, "acme", "events", WithCheckpointInterval(0), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := w.Put(ctx, Row{Key: fmt.Sprintf("k%d", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if err := ms.Delete(ctx, "tables/acme/events/_log/00000000000000000005.json"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	r, err := Open(ms, "acme", "events", WithCheckpointInterval(0), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	if _, err := r.Latest(ctx); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Latest = %v, want ErrCorrupt", err)
	}
	if _, err := r.Put(ctx, Row{Key: "fill", Payload: []byte("v")}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Put over a gap = %v, want ErrCorrupt", err)
	}
	if _, err := ms.Get(ctx, "tables/acme/events/_log/00000000000000000005.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the gap was filled: %v", err)
	}
	// Time travel to before the gap still reads.
	if s, err := r.Snapshot(ctx, 4); err != nil || s.Version() != 4 {
		t.Fatalf("Snapshot(4) = %v, %v", s, err)
	}
}

// The store failing to answer is not the end of the log.
func TestAFailedLogReadIsNotTheHead(t *testing.T) {
	_, kf, tb := checkpointed(t)
	kf.failGet("_log/00000000000000000012.json", errIO)
	if _, err := tb.Latest(context.Background()); !errors.Is(err, errIO) {
		t.Fatalf("Latest = %v, want the store's error", err)
	}
	_, kf, tb = checkpointed(t)
	kf.failGet("_log/00000000000000000014.json", errIO) // the gap check past the miss at v13
	if _, err := tb.Latest(context.Background()); !errors.Is(err, errIO) {
		t.Fatalf("Latest = %v, want the store's error from the gap check", err)
	}
}
