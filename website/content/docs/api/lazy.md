---
title: "runi/lazy"
weight: 9
---

```go
import "github.com/org-runink/runi/lazy"
```

Package lazy provides deferred values that can be started early.

A lazy value computes at most once, on demand. That trades latency for not
doing unnecessary work: nothing is computed until something forces it, and
then the forcing caller waits the full cost.

This package adds the missing half. Start begins the computation in the
background without forcing it, so by the time a caller needs the value the
work may already be done:

v := lazy.New(func(ctx context.Context) (Report, error) {
	return build(ctx) // 200ms
})
v.Start(ctx)          // returns immediately; work proceeds
... do other things ...
r, err := v.Get(ctx)  // ~0 if Start had time to finish

Laziness decides WHETHER work happens. Starting decides WHEN. Keeping them
separate means a pipeline can stay lazy — a value that is never needed is
never computed, even if Start was called and the result discarded — while the
values you know you will need are already in flight.

# Why not sync.OnceValue

The standard library's sync.OnceValue and sync.OnceValues cover compute-once
well, and if that is all you need, use them. This package exists for three
things they do not do:

  - context: Get takes a ctx, so a caller can stop waiting without stopping
    the computation that other callers are also waiting on;
  - speculative start: the latency optimisation above;
  - composition: Map and Then build a graph of deferred values, so an
    expensive dependency is computed once and shared by everything downstream
    rather than once per consumer.

# Errors and panics

A function returning an error is still memoised: the error is the value, and
every caller sees it. That is deliberate — a lazy value represents one
evaluation, not a retry policy. Wrap with a retry before New if you want one.

A panic in the function is recovered and returned to every caller as an error
wrapping ErrPanic, rather than taking down whichever goroutine happened to be
forcing the value. Which caller that is depends on scheduling, so letting it
propagate would make the crash site arbitrary.

All operations are safe for concurrent use.

## Index

- `type` [Value](#value)
- `func` [All](#all)
- `const` [ErrPanic](#errpanic)
- `func` [Map](#map)
- `func` [New](#new)
- `func` [Of](#of)
- `func` [Then](#then)
- `method` [Value.Get](#valueget)
- `method` [Value.Resolved](#valueresolved)
- `method` [Value.Start](#valuestart)

### Value

```go
type Value[T any] struct
```

Value is a deferred computation evaluated at most once.

The zero Value is not usable; build one with New.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### All

```go
func All[T any](ctx context.Context, vs ...*Value[T]) ([]T, error)
```

All starts every value and waits for all of them, returning results in order.

Starting them all first is the point: n values that each take t resolve in
about t rather than n·t. The first error encountered is returned, and the
others still complete — they may be shared with other callers.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### ErrPanic

```go
var ErrPanic = errors.New("lazy: function panicked")
```

ErrPanic wraps a panic recovered from a lazy function.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### Map

```go
func Map[A, B any](src *Value[A], f func(A) B) *Value[B]
```

Map returns a Value that applies f to the result of src, without forcing src.

The returned Value is itself lazy: nothing runs until it is started or
got, and then src is evaluated first. Because src is a Value, several Maps
over the same source share ONE evaluation of it.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### New

```go
func New[T any](fn func(context.Context) (T, error)) *Value[T]
```

New returns a Value that will call fn at most once.

fn is not called until Start or Get. The context passed to fn is the one from
whichever call first triggers evaluation.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### Of

```go
func Of[T any](v T) *Value[T]
```

Of returns an already-resolved Value. Useful as a base case in composition and
in tests.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### Then

```go
func Then[A, B any](src *Value[A], f func(context.Context, A) (B, error)) *Value[B]
```

Then chains a second deferred computation onto the first. It is Map for
functions that can themselves fail.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### Value.Get

```go
func (l *Value[T]) Get(ctx context.Context) (T, error)
```

Get returns the value, evaluating it if necessary and waiting if another
caller is already evaluating it.

If ctx is cancelled while waiting, Get returns ctx.Err() — but the evaluation
itself continues, because other callers may still be waiting on it and the
result is still wanted. A cancelled Get does not poison the Value.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### Value.Resolved

```go
func (l *Value[T]) Resolved() bool
```

Resolved reports whether the value is ready, without evaluating or waiting.

<small>Defined in <code>lazy/lazy.go</code>.</small>

### Value.Start

```go
func (l *Value[T]) Start(ctx context.Context)
```

Start begins evaluation in the background if it has not begun, and returns
immediately. Calling it more than once is harmless.

This is the latency optimisation: start a value as soon as you suspect you
will need it, and Get it later for free. If it turns out you never Get it, the
only cost is the work done — the result is simply discarded, which is why this
is worth doing for expensive-but-likely values and not for everything.

<small>Defined in <code>lazy/lazy.go</code>.</small>
