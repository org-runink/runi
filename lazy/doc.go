// Package lazy provides deferred values that can be started early.
//
// A lazy value computes at most once, on demand. That trades latency for not
// doing unnecessary work: nothing is computed until something forces it, and
// then the forcing caller waits the full cost.
//
// This package adds the missing half. Start begins the computation in the
// background without forcing it, so by the time a caller needs the value the
// work may already be done:
//
//	v := lazy.New(func(ctx context.Context) (Report, error) {
//		return build(ctx) // 200ms
//	})
//	v.Start(ctx)          // returns immediately; work proceeds
//	... do other things ...
//	r, err := v.Get(ctx)  // ~0 if Start had time to finish
//
// Laziness decides WHETHER work happens. Starting decides WHEN. Keeping them
// separate means a pipeline can stay lazy — a value that is never needed is
// never computed, even if Start was called and the result discarded — while the
// values you know you will need are already in flight.
//
// # Why not sync.OnceValue
//
// The standard library's sync.OnceValue and sync.OnceValues cover compute-once
// well, and if that is all you need, use them. This package exists for three
// things they do not do:
//
//   - context: Get takes a ctx, so a caller can stop waiting without stopping
//     the computation that other callers are also waiting on;
//   - speculative start: the latency optimisation above;
//   - composition: Map and Then build a graph of deferred values, so an
//     expensive dependency is computed once and shared by everything downstream
//     rather than once per consumer.
//
// # Errors and panics
//
// A function returning an error is still memoised: the error is the value, and
// every caller sees it. That is deliberate — a lazy value represents one
// evaluation, not a retry policy. Wrap with a retry before New if you want one.
//
// A panic in the function is recovered and returned to every caller as an error
// wrapping ErrPanic, rather than taking down whichever goroutine happened to be
// forcing the value. Which caller that is depends on scheduling, so letting it
// propagate would make the crash site arbitrary.
//
// All operations are safe for concurrent use.
package lazy
