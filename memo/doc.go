// Package memo is an in-process memoization cache with single-flight
// coalescing, TTL expiry and LRU eviction, using only the standard library.
//
// It is built for the case where the same expensive call — an LLM completion, a
// tool invocation, a rate-limited API request — is made repeatedly with
// identical inputs, and the work can be skipped.
//
//	c := memo.New[string, Answer](memo.Options{Capacity: 4096, TTL: 10 * time.Minute})
//	ans, err := c.Do(ctx, key, func(ctx context.Context) (Answer, error) {
//		return expensive(ctx, req)
//	})
//
// # Why single-flight is part of the cache, not beside it
//
// A plain cache helps the SECOND caller. Under concurrency the problem is
// usually the first N callers arriving together on a cold key: each misses,
// each runs the expensive call, and the cache turns an N-fold stampede into an
// N-fold stampede that also writes N times. Do coalesces them — one execution,
// every caller gets its result. The two mechanisms cover different populations
// (sequential repeats, concurrent duplicates) and neither substitutes for the
// other, so this package does both rather than leaving the second to the caller.
//
// # Errors are not cached by default
//
// Caching a failure converts a transient fault into a sticky one for the whole
// TTL, and this is the single most common way a memo cache makes a system worse
// rather than better. By default a call returning a non-nil error is not stored,
// though it IS still coalesced, so a thundering herd against a failing
// dependency still produces one call rather than N. Set CacheErrors to override
// that deliberately.
//
// # What this is not
//
//   - Not distributed. It is per-process, with no network and no shared state.
//     Two replicas have two caches. That is a property, not an oversight: the
//     moment a cache is shared it needs invalidation, coherence and a failure
//     mode of its own.
//   - Not a semantic cache. Keys match exactly. There is no embedding, no
//     similarity threshold, and therefore no possibility of returning a
//     confidently wrong answer because two different requests looked alike.
//   - Not a persistence layer. Nothing survives a restart.
//
// # Determinism and testing
//
// Expiry reads a clock that can be injected (Options.Now), so tests advance time
// rather than sleeping. Everything else is deterministic.
package memo
