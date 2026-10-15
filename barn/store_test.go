package barn

import (
	"context"
	"errors"
	"io"
	"testing"
)

// Every store call checks the caller's context first. A cancelled request that
// still writes is the difference between a timeout and a surprise commit.
func TestMemStoreHonoursContext(t *testing.T) {
	ms := NewMemStore()
	ctx := context.Background()
	if err := ms.PutIfAbsent(ctx, "k", []byte("v")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dead, cancel := context.WithCancel(ctx)
	cancel()

	if err := ms.Put(dead, "a", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("Put: %v", err)
	}
	if err := ms.PutIfAbsent(dead, "b", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("PutIfAbsent: %v", err)
	}
	if _, err := ms.Get(dead, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get: %v", err)
	}
	if _, err := ms.List(dead, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("List: %v", err)
	}
	if err := ms.Delete(dead, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete: %v", err)
	}
	// None of it happened.
	if _, err := ms.Get(ctx, "a"); err == nil {
		t.Error("a write on a cancelled context landed anyway")
	}
}

// Put and PutIfAbsent keep their own copy: a caller that reuses its buffer
// after the call must not change what was stored.
func TestMemStoreCopiesWrites(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()
	buf := []byte("one")
	if err := ms.Put(ctx, "p", buf); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := ms.PutIfAbsent(ctx, "q", buf); err != nil {
		t.Fatalf("put if absent: %v", err)
	}
	copy(buf, "two")
	for _, k := range []string{"p", "q"} {
		rc, err := ms.Get(ctx, k)
		if err != nil {
			t.Fatalf("get %s: %v", k, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		if string(b) != "one" {
			t.Errorf("%s = %q after the caller reused its buffer, want one", k, b)
		}
	}
}

// The zero value is an empty store, and every method works on it, the
// writing ones included: the map is created on first write.
func TestMemStoreZeroValue(t *testing.T) {
	ctx := context.Background()
	for _, write := range []func(*MemStore) error{
		func(m *MemStore) error { return m.Put(ctx, "k", []byte("v")) },
		func(m *MemStore) error { return m.PutIfAbsent(ctx, "k", []byte("v")) },
	} {
		var ms MemStore
		if _, err := ms.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get on the zero value = %v, want ErrNotFound", err)
		}
		if keys, err := ms.List(ctx, ""); err != nil || len(keys) != 0 {
			t.Fatalf("List on the zero value = %v, %v", keys, err)
		}
		if err := ms.Delete(ctx, "k"); err != nil {
			t.Fatalf("Delete on the zero value: %v", err)
		}
		if err := write(&ms); err != nil {
			t.Fatalf("first write on the zero value: %v", err)
		}
		if ms.numObjects() != 1 {
			t.Fatalf("zero value holds %d objects after one write", ms.numObjects())
		}
	}
	var ms MemStore
	ms.overwrite("k", []byte("v"))
	if ms.numObjects() != 1 {
		t.Fatal("overwrite on the zero value stored nothing")
	}

	// A table works on it end to end.
	tb, err := Open(&MemStore{}, "acme", "orders")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Put(ctx, Row{Key: "a", Payload: []byte("1")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if rec, ok, err := tb.Get(ctx, "a"); err != nil || !ok || string(rec.Payload) != "1" {
		t.Fatalf("get = %+v, %v, %v", rec, ok, err)
	}
}

// failHook reads the fault-injection hook under the lock, for a test whose
// hook swaps itself out and back.
func (m *MemStore) failHook() func(string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failPutIfAbsent
}

func TestMemStoreSemantics(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore()

	if err := ms.PutIfAbsent(ctx, "a", []byte("1")); err != nil {
		t.Fatalf("first: %v", err)
	}
	// The one guarantee the table rests on: the second writer loses.
	if err := ms.PutIfAbsent(ctx, "a", []byte("2")); !errors.Is(err, ErrExists) {
		t.Fatalf("second PutIfAbsent = %v, want ErrExists", err)
	}
	rc, err := ms.Get(ctx, "a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "1" {
		t.Errorf("value = %q, want the first writer's", b)
	}

	// Put overwrites where PutIfAbsent refuses.
	if err := ms.Put(ctx, "a", []byte("3")); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, _ = ms.Get(ctx, "a")
	b, _ = io.ReadAll(rc)
	rc.Close()
	if string(b) != "3" {
		t.Errorf("value after Put = %q, want 3", b)
	}

	if _, err := ms.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get of a missing key = %v, want ErrNotFound", err)
	}

	for _, k := range []string{"p/1", "p/2", "q/1"} {
		if err := ms.PutIfAbsent(ctx, k, []byte("x")); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	got, err := ms.List(ctx, "p/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("List(p/) = %v, want two keys", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("List is not sorted: %v", got)
		}
	}

	if err := ms.Delete(ctx, "a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Deleting what is gone is not an error: the caller wanted it gone.
	if err := ms.Delete(ctx, "a"); err != nil {
		t.Errorf("second delete: %v", err)
	}
}

// Open validates what it is given, because a bad tenant or table name would
// otherwise become an object key prefix and quietly address someone else's
// table.
func TestOpenRejectsBadArguments(t *testing.T) {
	if _, err := Open(nil, "acme", "orders"); err == nil {
		t.Error("a nil store was accepted")
	}
	for _, c := range []struct{ tenant, table string }{
		{"", "orders"},
		{"acme", ""},
		{"../etc", "orders"},
		{"acme", "../../secrets"},
		{"acme/orders", "x"},
		{"UPPER CASE", "orders"},
		{"acme", "has space"},
	} {
		if _, err := Open(NewMemStore(), c.tenant, c.table); err == nil {
			t.Errorf("accepted tenant=%q table=%q", c.tenant, c.table)
		}
	}
	if _, err := Open(NewMemStore(), "acme", "orders"); err != nil {
		t.Errorf("rejected a valid name: %v", err)
	}
}
