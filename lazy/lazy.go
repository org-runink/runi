package lazy

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrPanic wraps a panic recovered from a lazy function.
var ErrPanic = errors.New("lazy: function panicked")

// Value is a deferred computation evaluated at most once.
//
// The zero Value is not usable; build one with New.
type Value[T any] struct {
	fn func(context.Context) (T, error)

	mu      sync.Mutex
	started bool
	done    chan struct{} // closed when the result is ready

	val T
	err error
}

// New returns a Value that will call fn at most once.
//
// fn is not called until Start or Get. The context passed to fn is the one from
// whichever call first triggers evaluation.
func New[T any](fn func(context.Context) (T, error)) *Value[T] {
	return &Value[T]{fn: fn, done: make(chan struct{})}
}

// Of returns an already-resolved Value. Useful as a base case in composition and
// in tests.
func Of[T any](v T) *Value[T] {
	l := &Value[T]{done: make(chan struct{})}
	l.started, l.val = true, v
	close(l.done)
	return l
}

// Start begins evaluation in the background if it has not begun, and returns
// immediately. Calling it more than once is harmless.
//
// This is the latency optimisation: start a value as soon as you suspect you
// will need it, and Get it later for free. If it turns out you never Get it, the
// only cost is the work done — the result is simply discarded, which is why this
// is worth doing for expensive-but-likely values and not for everything.
func (l *Value[T]) Start(ctx context.Context) {
	l.mu.Lock()
	if l.started {
		l.mu.Unlock()
		return
	}
	l.started = true
	fn := l.fn
	l.mu.Unlock()

	go func() {
		v, err := safeCall(ctx, fn)
		l.mu.Lock()
		l.val, l.err = v, err
		l.mu.Unlock()
		close(l.done)
	}()
}

// Get returns the value, evaluating it if necessary and waiting if another
// caller is already evaluating it.
//
// If ctx is cancelled while waiting, Get returns ctx.Err() — but the evaluation
// itself continues, because other callers may still be waiting on it and the
// result is still wanted. A cancelled Get does not poison the Value.
func (l *Value[T]) Get(ctx context.Context) (T, error) {
	l.Start(ctx)
	select {
	case <-l.done:
		return l.val, l.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// Resolved reports whether the value is ready, without evaluating or waiting.
func (l *Value[T]) Resolved() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

func safeCall[T any](ctx context.Context, fn func(context.Context) (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			v = zero
			err = fmt.Errorf("%w: %v", ErrPanic, r)
		}
	}()
	return fn(ctx)
}

// Map returns a Value that applies f to the result of src, without forcing src.
//
// The returned Value is itself lazy: nothing runs until it is started or
// got, and then src is evaluated first. Because src is a Value, several Maps
// over the same source share ONE evaluation of it.
func Map[A, B any](src *Value[A], f func(A) B) *Value[B] {
	return New(func(ctx context.Context) (B, error) {
		a, err := src.Get(ctx)
		if err != nil {
			var zero B
			return zero, err
		}
		return f(a), nil
	})
}

// Then chains a second deferred computation onto the first. It is Map for
// functions that can themselves fail.
func Then[A, B any](src *Value[A], f func(context.Context, A) (B, error)) *Value[B] {
	return New(func(ctx context.Context) (B, error) {
		a, err := src.Get(ctx)
		if err != nil {
			var zero B
			return zero, err
		}
		return f(ctx, a)
	})
}

// All starts every value and waits for all of them, returning results in order.
//
// Starting them all first is the point: n values that each take t resolve in
// about t rather than n·t. The first error encountered is returned, and the
// others still complete — they may be shared with other callers.
func All[T any](ctx context.Context, vs ...*Value[T]) ([]T, error) {
	for _, v := range vs {
		v.Start(ctx)
	}
	out := make([]T, len(vs))
	var firstErr error
	for i, v := range vs {
		got, err := v.Get(ctx)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		out[i] = got
	}
	return out, firstErr
}
