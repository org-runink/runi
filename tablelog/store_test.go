package tablelog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
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

	if err := ms.Put(dead, "a", strings.NewReader("x"), 1, ""); !errors.Is(err, context.Canceled) {
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

// A reader that fails part-way through must fail the Put rather than storing
// the prefix it managed to read.
func TestMemStorePutReaderError(t *testing.T) {
	ms := NewMemStore()
	err := ms.Put(context.Background(), "k", iotest(), 10, "")
	if err == nil {
		t.Fatal("a failing reader was accepted")
	}
	if _, err := ms.Get(context.Background(), "k"); err == nil {
		t.Error("a partially read object was stored")
	}
}

func iotest() io.Reader {
	return io.MultiReader(strings.NewReader("ab"), errReaderOnly{})
}

type errReaderOnly struct{}

func (errReaderOnly) Read([]byte) (int, error) { return 0, errors.New("read failed") }

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
	if err := ms.Put(ctx, "a", bytes.NewReader([]byte("3")), 1, "application/json"); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, _ = ms.Get(ctx, "a")
	b, _ = io.ReadAll(rc)
	rc.Close()
	if string(b) != "3" {
		t.Errorf("value after Put = %q, want 3", b)
	}

	if _, err := ms.Get(ctx, "missing"); err == nil {
		t.Error("Get of a missing key succeeded")
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
