---
title: "runi/memo"
weight: 8
---

```go
import "github.com/org-runink/runi/memo"
```

Package memo is an in-process memoization cache with single-flight
coalescing, TTL expiry and LRU eviction, using only the standard library.

It is built for the case where the same expensive call — an LLM completion, a
tool invocation, a rate-limited API request — is made repeatedly with
identical inputs, and the work can be skipped.

c := memo.New[string, Answer](memo.Options{Capacity: 4096, TTL: 10 * time.Minute})
ans, err := c.Do(ctx, key, func(ctx context.Context) (Answer, error) {
	return expensive(ctx, req)
})

# Why single-flight is part of the cache, not beside it

A plain cache helps the SECOND caller. Under concurrency the problem is
usually the first N callers arriving together on a cold key: each misses,
each runs the expensive call, and the cache turns an N-fold stampede into an
N-fold stampede that also writes N times. Do coalesces them — one execution,
every caller gets its result. The two mechanisms cover different populations
(sequential repeats, concurrent duplicates) and neither substitutes for the
other, so this package does both rather than leaving the second to the caller.

# Errors are not cached by default

Caching a failure converts a transient fault into a sticky one for the whole
TTL, and this is the single most common way a memo cache makes a system worse
rather than better. By default a call returning a non-nil error is not stored,
though it IS still coalesced, so a thundering herd against a failing
dependency still produces one call rather than N. Set CacheErrors to override
that deliberately.

# What this is not

  - Not distributed. It is per-process, with no network and no shared state.
    Two replicas have two caches. That is a property, not an oversight: the
    moment a cache is shared it needs invalidation, coherence and a failure
    mode of its own.
  - Not a semantic cache. Keys match exactly. There is no embedding, no
    similarity threshold, and therefore no possibility of returning a
    confidently wrong answer because two different requests looked alike.
  - Not a persistence layer. Nothing survives a restart.

# Determinism and testing

Expiry reads a clock that can be injected (Options.Now), so tests advance time
rather than sleeping. Everything else is deterministic.

# Measured against functools.lru_cache

Same experiment on both sides: N callers hit one cold key simultaneously, the
function takes 5 ms, and what is counted is how many times the function
actually ran.

                                  memo   lru_cache
cache hit                      19.6 ns     36.6 ns
cache miss                      725 ns       82 ns   lru_cache 8.8x faster
8 cold callers, function ran         1           8
64 cold callers, function ran        1          64
wall clock, 64 cold callers     5.75 ms    13.17 ms
TTL                                yes          no
single-flight                      yes          no

The miss is genuinely slower: lru_cache inserts into a dict keyed on the
argument tuple's hash, while this one canonicalises a structured key and
maintains an LRU list and expiry. The row that matters is the count. With 64
callers on a cold key lru_cache calls the expensive function 64 times and
this calls it once, so if that function is a model inference or a metered API
call the difference is not 8.8x on a nanosecond but 64x on the expensive
thing. That is not a flaw in lru_cache, which never promised single-flight.

Measured on an ASUS Ascent GX10, 20 cores, aarch64, Go 1.27.2, Python 3.12.3.

## Index

- `type` [Options](#options)
- `type` [Stats](#stats)
- `type` [Store](#store)
- `func` [Hash](#hash)
- `func` [New](#new)
- `func` [NormalizeSpace](#normalizespace)
- `method` [Stats.HitRate](#statshitrate)
- `method` [Store.Do](#storedo)
- `method` [Store.Get](#storeget)
- `method` [Store.Invalidate](#storeinvalidate)
- `method` [Store.Purge](#storepurge)
- `method` [Store.Set](#storeset)
- `method` [Store.Stats](#storestats)

### Options

```go
type Options struct
```

Options configures a Store. The zero value is usable: unlimited capacity, no
expiry, errors not cached.

<small>Defined in <code>memo/memo.go</code>.</small>

### Stats

```go
type Stats struct
```

Stats is a snapshot of a Store's counters.

<small>Defined in <code>memo/memo.go</code>.</small>

### Store

```go
type Store[K comparable, V any] struct
```

Store is a concurrency-safe memoization cache. The zero Store is not usable;
build one with New.

<small>Defined in <code>memo/memo.go</code>.</small>

### Hash

```go
func Hash(parts ...any) string
```

Hash builds a deterministic cache key from structured parts.

The hard part of an exact-match cache is not storage, it is deciding that two
requests are the same request. Hash makes that decision explicit and
repeatable:

  - map keys are SORTED before hashing, because Go randomises map iteration
    order and hashing a map directly would produce a different key every run
    for identical input — a cache that never hits and never tells you why;
  - values are TYPE-TAGGED, so Hash("1") and Hash(1) and Hash(true) differ.
    Untagged concatenation collides in ways that are very hard to find later;
  - lengths are written before variable-length data, so {"ab","c"} and
    {"a","bc"} do not collide.

It returns a hex SHA-256. Use it for the key type of a Store:

key := memo.Hash("model-v3", 0.2, map[string]any{"q": q, "lang": "en"})

# Supported types

Hash encodes these types directly: nil, string, []byte, bool, every int and
uint type, float32, float64, []string, []any, map[string]any and
map[string]string, nested to any depth through the last four. Signed
integers of every width hash alike, as do unsigned integers of every width
and float32 with the float64 it converts to. Every NaN hashes alike, and -0
hashes as 0.

For these types the output is stable: the same parts give the same key in
every process, on every platform, and in every v1 release of this module,
so a key may be persisted or shared between processes.

# Other types

Any other type is walked by reflection and hashed by its contents, never by
its address: a pointer by what it points to (a nil pointer differs from a
pointer to the zero value), a struct by all of its fields, exported or not,
slices, arrays and maps (keys of any type, sorted) element by element, and an
interface by the value it holds, tagged as if that value had been passed to
Hash directly. A type that implements encoding.TextMarshaler is hashed by its
text instead, so a time.Time is hashed as the instant it names and not its
location pointer or monotonic reading. The type's name and package path are
part of the key, so equal contents of two types differ, and a cyclic value is
hashed as a cycle rather than recursing forever. Channels, funcs and unsafe
pointers have no contents and are hashed by identity: such a key matches
only the same channel or func, and funcs are identified by their code, so
two closures of one function literal share a key. Leave them out of keys.

Keys of these types are deterministic across runs of one build, but they are
not covered by the stability promise above: renaming or moving a type, or
changing its fields, changes its keys, and so may a later release of this
module. Do not persist them beyond the life of a deployment.

A value of a supported type cycling through itself, such as a []any that
contains itself, is not detected and overflows the stack.

<small>Defined in <code>memo/key.go</code>.</small>

### New

```go
func New[K comparable, V any](opts Options) *Store[K, V]
```

New returns a Store.

<small>Defined in <code>memo/memo.go</code>.</small>

### NormalizeSpace

```go
func NormalizeSpace(s string) string
```

NormalizeSpace collapses every run of whitespace to a single space and trims
the ends.

Use it on free text before hashing when "the same request" should survive
reformatting — a prompt that gained a trailing newline, or was re-wrapped — and
do NOT use it where whitespace is significant, such as code or
whitespace-sensitive markup. It is offered separately rather than applied
inside Hash for exactly that reason: only the caller knows which case this is.

<small>Defined in <code>memo/key.go</code>.</small>

### Stats.HitRate

```go
func (s Stats) HitRate() float64
```

HitRate is hits as a fraction of all lookups, counting a coalesced wait as a
hit — it also avoided an execution. Returns 0 when there have been no
lookups.

<small>Defined in <code>memo/memo.go</code>.</small>

### Store.Do

```go
func (s *Store[K, V]) Do(ctx context.Context, key K, fn func(context.Context) (V, error)) (V, error)
```

Do returns the memoized value for key, running fn exactly once for
concurrent callers that miss.

On a hit the stored value is returned without calling fn. On a miss fn runs;
other callers arriving for the same key while it runs wait for that single
execution rather than starting their own.

If fn returns an error the result is not stored unless Options.CacheErrors is
set, but it IS still shared with the callers that coalesced onto it. ctx
cancels the wait for an in-flight execution; it does not cancel the execution
itself, because other callers may still be waiting on it.

<small>Defined in <code>memo/memo.go</code>.</small>

### Store.Get

```go
func (s *Store[K, V]) Get(key K) (v V, ok bool)
```

Get returns a stored value without running anything. ok is false on a miss or
an expired entry.

<small>Defined in <code>memo/memo.go</code>.</small>

### Store.Invalidate

```go
func (s *Store[K, V]) Invalidate(key K) bool
```

Invalidate removes key. It reports whether an entry was present.

<small>Defined in <code>memo/memo.go</code>.</small>

### Store.Purge

```go
func (s *Store[K, V]) Purge()
```

Purge removes every entry. In-flight executions are unaffected and their
results are still stored when they finish.

<small>Defined in <code>memo/memo.go</code>.</small>

### Store.Set

```go
func (s *Store[K, V]) Set(key K, v V)
```

Set stores a value directly, as if it had been computed now.

<small>Defined in <code>memo/memo.go</code>.</small>

### Store.Stats

```go
func (s *Store[K, V]) Stats() Stats
```

Stats returns a snapshot of the counters.

<small>Defined in <code>memo/memo.go</code>.</small>
