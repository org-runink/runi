package lazy

import (
	"context"
	"errors"
	"testing"
)

// Gaps the existing tests leave: Map's error path, Then's success path, and
// All's behaviour when one of the values fails.

func TestMapPropagatesSourceError(t *testing.T) {
	boom := errors.New("boom")
	src := New(func(ctx context.Context) (int, error) { return 0, boom })

	mapped := Map(src, func(v int) string {
		t.Fatal("the mapping function must not run when the source failed")
		return ""
	})

	got, err := mapped.Get(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Map error = %v; want boom", err)
	}
	if got != "" {
		t.Fatalf("Map value on error = %q; want the zero value", got)
	}
}

func TestThenRunsOnSuccessAndCanFailItself(t *testing.T) {
	src := New(func(ctx context.Context) (int, error) { return 21, nil })

	doubled := Then(src, func(ctx context.Context, v int) (int, error) { return v * 2, nil })
	got, err := doubled.Get(context.Background())
	if err != nil || got != 42 {
		t.Fatalf("Then = %v,%v; want 42,nil", got, err)
	}

	boom := errors.New("second stage failed")
	failing := Then(src, func(ctx context.Context, v int) (int, error) { return 0, boom })
	if _, err := failing.Get(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("Then error = %v; want boom", err)
	}
}

func TestAllReturnsTheFirstErrorAndStillFillsTheRest(t *testing.T) {
	boom := errors.New("boom")
	ok1 := New(func(ctx context.Context) (int, error) { return 1, nil })
	bad := New(func(ctx context.Context) (int, error) { return 0, boom })
	ok2 := New(func(ctx context.Context) (int, error) { return 3, nil })

	out, err := All(context.Background(), ok1, bad, ok2)
	if !errors.Is(err, boom) {
		t.Fatalf("All error = %v; want boom", err)
	}
	if len(out) != 3 {
		t.Fatalf("All returned %d results; want 3 — results stay positional", len(out))
	}
	// The successful values are still present, in order, around the failure.
	if out[0] != 1 || out[1] != 0 || out[2] != 3 {
		t.Fatalf("All = %v; want [1 0 3] with the zero value in the failed slot", out)
	}
}

func TestAllOfNothing(t *testing.T) {
	out, err := All[int](context.Background())
	if err != nil {
		t.Fatalf("All() error = %v; want nil", err)
	}
	if len(out) != 0 {
		t.Fatalf("All() = %v; want an empty slice", out)
	}
}

func TestMapChainsOntoMap(t *testing.T) {
	src := New(func(ctx context.Context) (int, error) { return 2, nil })
	a := Map(src, func(v int) int { return v * 10 })
	b := Map(a, func(v int) int { return v + 1 })

	got, err := b.Get(context.Background())
	if err != nil || got != 21 {
		t.Fatalf("chained Map = %v,%v; want 21,nil", got, err)
	}
}
