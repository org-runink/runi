package barn

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// keyFaults wraps a store and fails Get or List for chosen keys or prefixes,
// so a test can say exactly which read fails and how. The checkpoint paths
// treat ErrNotFound as "nothing there, fall back" and every other error as a
// failure to report; these tests drive each side of that line.
type keyFaults struct {
	inner Store

	mu      sync.Mutex
	getErr  map[string]error // by key suffix
	listErr map[string]error // by prefix suffix
}

func (k *keyFaults) fault(m map[string]error, s string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for suffix, err := range m {
		if strings.HasSuffix(s, suffix) {
			return err
		}
	}
	return nil
}

func (k *keyFaults) failGet(suffix string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.getErr == nil {
		k.getErr = map[string]error{}
	}
	k.getErr[suffix] = err
}

func (k *keyFaults) failList(suffix string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.listErr == nil {
		k.listErr = map[string]error{}
	}
	k.listErr[suffix] = err
}

func (k *keyFaults) Put(ctx context.Context, key string, d []byte) error {
	return k.inner.Put(ctx, key, d)
}
func (k *keyFaults) PutIfAbsent(ctx context.Context, key string, d []byte) error {
	return k.inner.PutIfAbsent(ctx, key, d)
}
func (k *keyFaults) Delete(ctx context.Context, key string) error { return k.inner.Delete(ctx, key) }
func (k *keyFaults) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := k.fault(k.getErr, key); err != nil {
		return nil, err
	}
	return k.inner.Get(ctx, key)
}
func (k *keyFaults) List(ctx context.Context, prefix string) ([]string, error) {
	if err := k.fault(k.listErr, prefix); err != nil {
		return nil, err
	}
	return k.inner.List(ctx, prefix)
}

// checkpointed returns a store holding a table with a checkpoint at v10 and a
// hint naming it, plus two more commits on top, and a fresh handle on it that
// has read nothing yet.
func checkpointed(t *testing.T) (*MemStore, *keyFaults, *Table) {
	t.Helper()
	ctx := context.Background()
	ms := NewMemStore()
	w, err := Open(ms, "acme", "orders", WithCheckpointInterval(10), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 12; i++ {
		if _, err := w.Put(ctx, Row{Key: string(rune('a' + i)), Payload: []byte{byte(i)}}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := w.LastCheckpointError(); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	kf := &keyFaults{inner: ms}
	tb, err := Open(kf, "acme", "orders", WithCheckpointInterval(10), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	return ms, kf, tb
}

func wantRows(t *testing.T, tb *Table, n int) {
	t.Helper()
	recs, err := tb.Scan(context.Background(), "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(recs) != n {
		t.Fatalf("scan returned %d rows, want %d", len(recs), n)
	}
}

// A hint that is not there is the normal state of a table that has never
// checkpointed, and of one whose hint was lost: the read lists _checkpoint/
// and still starts from the newest checkpoint.
func TestMissingHintFallsBackToListing(t *testing.T) {
	_, kf, tb := checkpointed(t)
	kf.failGet("_checkpoint/_last", errors.Join(errors.New("gone"), ErrNotFound))
	wantRows(t, tb, 12)
}

// A hint the store could not read is not a missing hint. Treating it as one
// would make every read a full replay of the log without anyone being told.
func TestUnreadableHintIsReported(t *testing.T) {
	_, kf, tb := checkpointed(t)
	kf.failGet("_checkpoint/_last", errIO)
	if _, err := tb.Latest(context.Background()); !errors.Is(err, errIO) {
		t.Fatalf("Latest = %v, want the store's error", err)
	}
}

// The same holds for the listing the read falls back to: an empty listing
// means no checkpoint, a failed one is an error.
func TestCheckpointListingFailureIsReported(t *testing.T) {
	ctx := context.Background()
	_, kf, tb := checkpointed(t)
	kf.failList("_checkpoint/", errIO)
	// Time travel always lists checkpoints.
	if _, err := tb.Snapshot(ctx, 11); !errors.Is(err, errIO) {
		t.Fatalf("Snapshot = %v, want the store's error", err)
	}
	// At the head, a missing hint sends the read to the listing too.
	kf.failGet("_checkpoint/_last", ErrNotFound)
	if _, err := tb.Latest(ctx); !errors.Is(err, errIO) {
		t.Fatalf("Latest = %v, want the store's error", err)
	}
}

// A checkpoint the hint names but that does not exist is skipped, and the log
// replayed; one the store failed to read is reported.
func TestCheckpointReadMissingVersusFailing(t *testing.T) {
	ctx := context.Background()
	_, kf, tb := checkpointed(t)
	kf.failGet("_checkpoint/00000000000000000010.avro", ErrNotFound)
	wantRows(t, tb, 12)

	_, kf, tb = checkpointed(t)
	kf.failGet("_checkpoint/00000000000000000010.avro", errIO)
	if _, err := tb.Latest(ctx); !errors.Is(err, errIO) {
		t.Fatalf("Latest = %v, want the store's error", err)
	}
}

// Writing a checkpoint reads the hint to move it only forward. With no hint it
// writes one; when the hint cannot be read it reports the failure instead of
// overwriting a hint it never saw. Neither fails the commit.
func TestCheckpointWriteHintMissingVersusFailing(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	kf := &keyFaults{inner: ms}
	tb, err := Open(kf, "acme", "orders", WithCheckpointInterval(1), WithClock(newClock().now))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := tb.LastCheckpointError(); err != nil {
		t.Fatalf("first checkpoint, no hint yet: %v", err)
	}
	if v, ok, err := tb.readHint(ctx); err != nil || !ok || v != 1 {
		t.Fatalf("hint = %d, %v, %v; want 1", v, ok, err)
	}

	kf.failGet("_checkpoint/_last", errIO)
	if _, err := tb.Put(ctx, Row{Key: "b", Payload: []byte("2")}); err != nil {
		t.Fatalf("a checkpoint failure failed the commit: %v", err)
	}
	if err := tb.LastCheckpointError(); !errors.Is(err, errIO) {
		t.Fatalf("LastCheckpointError = %v, want the store's error", err)
	}
	// Read the hint from the store underneath, past the injected failure.
	rc, err := ms.Get(ctx, tb.lastKey())
	if err != nil {
		t.Fatalf("hint: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "1" {
		t.Fatalf("hint = %q, want it left at 1", b)
	}
}
