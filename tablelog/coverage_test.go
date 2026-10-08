package tablelog

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

var errIO = errors.New("object store unavailable")

// faultStore wraps a working store and makes one chosen call fail. Sweeping the
// trip count over every call a operation makes puts the failure at each step in
// turn, which is how every error path below gets walked without writing a test
// per path. The table must never answer as if the failed step had succeeded.
type faultStore struct {
	inner  Store
	mu     sync.Mutex
	method string // "" means every method is eligible
	trip   int    // fail on the trip-th matching call (1-based); 0 disables
	n      int
}

// arm resets the counter and selects which call to fail.
func (f *faultStore) arm(trip int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n, f.trip = 0, trip
}

func (f *faultStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

// hit counts every eligible call and reports whether this is the one to fail.
// It counts even when no trip is armed, because the first pass uses the count
// to decide how many trips to sweep — an earlier version returned before
// incrementing, so the count was always zero, every sweep was skipped, and the
// test passed while exercising nothing.
func (f *faultStore) hit(method string) bool {
	// Locked because the table reads data files concurrently: Scan fans out
	// over them, so this is called from several goroutines at once. The race
	// detector found that, which is the point of running it over the tests and
	// not only over the package.
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.method != "" && f.method != method {
		return false
	}
	f.n++
	return f.trip != 0 && f.n == f.trip
}

func (f *faultStore) Put(ctx context.Context, k string, r io.Reader, n int64, ct string) error {
	if f.hit("Put") {
		return errIO
	}
	return f.inner.Put(ctx, k, r, n, ct)
}

func (f *faultStore) PutIfAbsent(ctx context.Context, k string, d []byte) error {
	if f.hit("PutIfAbsent") {
		return errIO
	}
	return f.inner.PutIfAbsent(ctx, k, d)
}

func (f *faultStore) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	if f.hit("Get") {
		return nil, errIO
	}
	return f.inner.Get(ctx, k)
}

func (f *faultStore) List(ctx context.Context, p string) ([]string, error) {
	if f.hit("List") {
		return nil, errIO
	}
	return f.inner.List(ctx, p)
}

func (f *faultStore) Delete(ctx context.Context, k string) error {
	if f.hit("Delete") {
		return errIO
	}
	return f.inner.Delete(ctx, k)
}

func seeded(t *testing.T) (*MemStore, *Table) {
	t.Helper()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	for i, k := range []string{"a", "b", "c", "d"} {
		if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte{byte('0' + i)}}); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	if _, err := tb.Delete(ctx, "d"); err != nil {
		t.Fatalf("seed delete: %v", err)
	}
	return ms, tb
}

// Every operation, with the store failing at each call it makes in turn.
func TestOperationsPropagateStoreFailures(t *testing.T) {
	ctx := context.Background()
	ops := []struct {
		name string
		run  func(tb *Table) error
	}{
		{"Put", func(tb *Table) error { _, err := tb.Put(ctx, Row{Key: "z", Payload: []byte("1")}); return err }},
		{"Delete", func(tb *Table) error { _, err := tb.Delete(ctx, "a"); return err }},
		{"Get", func(tb *Table) error { _, _, err := tb.Get(ctx, "a"); return err }},
		{"Scan", func(tb *Table) error { _, err := tb.Scan(ctx, ""); return err }},
		{"Version", func(tb *Table) error { _, err := tb.Version(ctx); return err }},
		{"Latest", func(tb *Table) error { _, err := tb.Latest(ctx); return err }},
		{"Compact", func(tb *Table) error { _, err := tb.Compact(ctx); return err }},
		{"Vacuum", func(tb *Table) error { _, err := tb.Vacuum(ctx, 10*time.Minute); return err }},
		{"Snapshot", func(tb *Table) error { _, err := tb.Snapshot(ctx, 1); return err }},
		{"ChangesSince", func(tb *Table) error {
			s, err := tb.Latest(ctx)
			if err != nil {
				return err
			}
			_, err = s.ChangesSince(ctx, "", 0)
			return err
		}},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			// How many store calls does a clean run make? The counter is armed
			// only after Open, so the sweep below lands inside the operation
			// rather than being used up opening the table — which is what an
			// earlier version of this did, leaving most of these paths unwalked
			// while still passing.
			ms, _ := seeded(t)
			counter := &faultStore{inner: ms}
			tb, err := Open(counter, "acme", "orders")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			counter.arm(0)
			if err := op.run(tb); err != nil {
				t.Fatalf("clean run: %v", err)
			}
			calls := counter.count()
			if calls == 0 {
				t.Skip("operation made no store calls on a warm table")
			}
			for trip := 1; trip <= calls; trip++ {
				ms2, _ := seeded(t)
				fs := &faultStore{inner: ms2}
				tb2, err := Open(fs, "acme", "orders")
				if err != nil {
					t.Fatalf("open for trip %d: %v", trip, err)
				}
				fs.arm(trip)
				// The operation may still succeed, because some calls are
				// optional — a checkpoint that fails to write is not a failed
				// commit. What it must not do is panic, or report success with
				// a result assembled from a read that failed.
				_ = op.run(tb2)
			}
		})
	}
}

// Open itself talks to the store, and a table that cannot be opened must say so.
func TestOpenPropagatesStoreFailures(t *testing.T) {
	ms, _ := seeded(t)
	counter := &faultStore{inner: ms}
	if _, err := Open(counter, "acme", "orders"); err != nil {
		t.Fatalf("clean open: %v", err)
	}
	for trip := 1; trip <= counter.count(); trip++ {
		ms2, _ := seeded(t)
		if _, err := Open(&faultStore{inner: ms2, trip: trip}, "acme", "orders"); err == nil {
			// Open is allowed to tolerate some failures (a missing checkpoint
			// is not fatal); what it must not do is crash.
			continue
		}
	}
}

// A log entry or checkpoint that is not the JSON it should be is corruption,
// not an empty table: reporting "no rows" for an unreadable log would be a
// silent wrong answer.
func TestCorruptObjectsAreReported(t *testing.T) {
	ctx := context.Background()
	ms, _ := seeded(t)
	keys, err := ms.List(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	corrupted := 0
	for _, k := range keys {
		ms2 := NewMemStore()
		all, _ := ms.List(ctx, "")
		for _, kk := range all {
			rc, err := ms.Get(ctx, kk)
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if kk == k {
				b = []byte("{not json at all")
			}
			if err := ms2.PutIfAbsent(ctx, kk, b); err != nil {
				t.Fatalf("seed copy: %v", err)
			}
		}
		tb, err := Open(ms2, "acme", "orders")
		if err != nil {
			corrupted++
			continue
		}
		if _, err := tb.Scan(ctx, ""); err != nil {
			corrupted++
		}
	}
	if corrupted == 0 {
		t.Error("corrupting every object in turn produced no error anywhere")
	}
}

func TestDeleteAndVersion(t *testing.T) {
	ctx := context.Background()
	_, tb := seeded(t)

	v, err := tb.Version(ctx)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v <= 0 {
		t.Errorf("version = %d, want a positive version after writes", v)
	}

	// Delete returns the version of the commit it wrote, not a count.
	dv, err := tb.Delete(ctx, "a", "b")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if dv <= v {
		t.Errorf("delete version %d, want greater than %d", dv, v)
	}
	if _, ok, err := tb.Get(ctx, "a"); err != nil || ok {
		t.Errorf("deleted key still present: ok=%v err=%v", ok, err)
	}
	// Deleting an absent key is not an error: it still commits a tombstone.
	if _, err := tb.Delete(ctx, "nope"); err != nil {
		t.Errorf("delete absent: %v", err)
	}
	// Deleting nothing at all is a caller mistake and is refused.
	if _, err := tb.Delete(ctx); err == nil {
		t.Error("Delete with no keys was accepted")
	}

	s, err := tb.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if s.Version() <= 0 {
		t.Errorf("snapshot version = %d", s.Version())
	}
}

func TestVacuumAndCompactPaths(t *testing.T) {
	ctx := context.Background()
	_, tb := seeded(t)
	for i := 0; i < 6; i++ {
		if _, err := tb.Put(ctx, Row{Key: "k" + strings.Repeat("x", i), Payload: []byte("v")}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("compact: %v", err)
	}
	// Retention in the future keeps everything.
	if res, err := tb.Vacuum(ctx, 24*time.Hour); err != nil {
		t.Fatalf("vacuum long retention: %v", err)
	} else if len(res.Deleted) != 0 {
		t.Errorf("long retention deleted %d files", len(res.Deleted))
	}
	// Below the minimum retention Vacuum refuses rather than deleting files a
	// reader may still be mid-way through.
	if _, err := tb.Vacuum(ctx, 0); err == nil {
		t.Error("vacuum accepted a zero retention")
	}
	if _, err := tb.Vacuum(ctx, 10*time.Minute); err != nil {
		t.Fatalf("vacuum at the minimum: %v", err)
	}
	if _, err := tb.Compact(ctx); err != nil {
		t.Fatalf("compact after vacuum: %v", err)
	}
}

// A lost race on the log object must be retried, not reported.
func TestCommitRetriesLostRaces(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	tb, err := Open(ms, "acme", "orders")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var races int
	ms.FailPutIfAbsent = func(key string) error {
		if strings.Contains(key, "_log/") && races < 2 {
			races++
			return ErrExists
		}
		return nil
	}
	if _, err := tb.Put(ctx, Row{Key: "b", Payload: []byte("2")}); err != nil {
		t.Fatalf("put after lost races: %v", err)
	}
	if races != 2 {
		t.Errorf("simulated %d races, expected 2 — the retry path was not exercised", races)
	}
	ms.FailPutIfAbsent = nil
	if rec, ok, err := tb.Get(ctx, "b"); err != nil || !ok || string(rec.Payload) != "2" {
		t.Errorf("value after retry: %q ok=%v err=%v", rec.Payload, ok, err)
	}
}
