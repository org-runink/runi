---
title: "runi/barn"
weight: 7
---

```go
import "github.com/org-runink/runi/barn"
```

Package barn is an append-only, versioned key/value table kept entirely on
the object store — the Delta/Iceberg ("Snowflake/Databricks-style") pattern,
scaled down to application metadata. There is no database process: a table is
a set of immutable Avro data files plus a transaction log of JSON entries, and
the only coordination primitive is the object store's atomic create
(PutIfAbsent: an atomic create).

# Layout

Everything for one table lives under tables/<tenant>/<table>/:

data/<hex>.avro            immutable Avro OCF rows (key, op put|del, ts, payload, ver, ord)
_log/%020d.json            commit N: {version, parent, ts, writer, add, remove, op}
_checkpoint/%020d.avro     the full live file set as of version N
_checkpoint/_last          the newest checkpoint version — a HINT only

A data file's name is 16 hex digits of its creation time (unix µs) followed by
32 random hex digits (crypto/rand). The time prefix lets Vacuum age an orphan
without reading it and without trusting server mtimes, which many object
stores do not expose.

# Commit

Optimistic concurrency: read the head (the _last hint, then a LIST of _log/),
write the data file, then PutIfAbsent(_log/<head+1>). Losing that race yields
ErrExists; the writer re-reads the head and conflict-checks. Appends never
conflict. A compaction conflicts when a file it removes is no longer live; it
then re-plans from the new head. Retries use jittered exponential backoff, up
to the limit set by WithMaxAttempts (32 by default). Versions are therefore
dense: 1, 2, 3, … with no gaps.

# Read

State = checkpoint + replay of later log entries. Rows merge by key with
last-writer-wins on (version, ordinal); a del row is a tombstone. Snapshot(v)
reads the table as of any retained version (time travel).

# Encryption is the Store's business, not this package's

barn does not encrypt anything. It hands bytes to the [Store] and reads
them back, so whether objects are sealed at rest is entirely a property of
the implementation you supply.

If you do seal them, bind the ciphertext to the object's KEY. Every key here
already encodes tenant, table and path (tables/<tenant>/<table>/<path>), so
binding to the key gives you the property worth having: a data file copied
into another tenant's or table's prefix fails to open rather than being
served as that table's data. A scheme that binds only to the bucket, or to
nothing, will decrypt a misplaced file happily.

# What it is not

No joins, no multi-table transactions, no secondary indexes, no uniqueness
beyond the key, no SQL. Workloads that need those stay on Litestream-replicated
SQLite.

## Index

- `type` [Change](#change)
- `type` [CompactResult](#compactresult)
- `type` [MemStore](#memstore)
- `type` [Option](#option)
- `type` [Record](#record)
- `type` [Row](#row)
- `type` [Snapshot](#snapshot)
- `type` [Store](#store)
- `type` [Table](#table)
- `type` [VacuumResult](#vacuumresult)
- `method` [MemStore.Delete](#memstoredelete)
- `method` [MemStore.Get](#memstoreget)
- `method` [MemStore.List](#memstorelist)
- `method` [MemStore.Put](#memstoreput)
- `method` [MemStore.PutIfAbsent](#memstoreputifabsent)
- `func` [NewMemStore](#newmemstore)
- `func` [Open](#open)
- `method` [Snapshot.ChangesSince](#snapshotchangessince)
- `method` [Snapshot.Get](#snapshotget)
- `method` [Snapshot.Scan](#snapshotscan)
- `method` [Snapshot.Version](#snapshotversion)
- `method` [Table.Compact](#tablecompact)
- `method` [Table.Delete](#tabledelete)
- `method` [Table.Get](#tableget)
- `method` [Table.LastCheckpointError](#tablelastcheckpointerror)
- `method` [Table.Latest](#tablelatest)
- `method` [Table.Put](#tableput)
- `method` [Table.Scan](#tablescan)
- `method` [Table.Snapshot](#tablesnapshot)
- `method` [Table.Vacuum](#tablevacuum)
- `method` [Table.Version](#tableversion)
- `func` [WithCheckpointInterval](#withcheckpointinterval)
- `func` [WithClock](#withclock)
- `func` [WithCompaction](#withcompaction)
- `func` [WithMaxAttempts](#withmaxattempts)

### Change

```go
type Change struct
```

Change is one row committed after a given version, as ChangesSince reads it.

<small>Defined in <code>barn/changes.go</code>.</small>

### CompactResult

```go
type CompactResult struct
```

CompactResult describes one Compact call. Version 0 means there was nothing
worth compacting and no commit was made.

<small>Defined in <code>barn/maintain.go</code>.</small>

### MemStore

```go
type MemStore struct
```

MemStore is an in-memory [Store]: the reference implementation, and enough to
run a table in a test without any infrastructure.

It is NOT a toy. It implements the one guarantee the table's correctness
rests on — PutIfAbsent is an atomic create — and it is safe for concurrent
use, so the optimistic-concurrency paths can be exercised honestly rather
than simulated. What it is not is durable: everything is lost when it goes
out of scope.

Use it to try the package, to test code that writes tables, and as the
shortest correct example of what a real Store must do.

<small>Defined in <code>barn/memstore.go</code>.</small>

### Option

```go
type Option func(*config)
```

Option configures a Table.

<small>Defined in <code>barn/barn.go</code>.</small>

### Record

```go
type Record struct
```

Record is one live row as read back.

<small>Defined in <code>barn/barn.go</code>.</small>

### Row

```go
type Row struct
```

Row is one key/value put.

<small>Defined in <code>barn/barn.go</code>.</small>

### Snapshot

```go
type Snapshot struct
```

Snapshot is an immutable view of the table at one version. The zero value is
not usable; get one from Table.Latest or Table.Snapshot.

<small>Defined in <code>barn/barn.go</code>.</small>

### Store

```go
type Store interface
```

Store is the object-store surface barn needs. Implement it over S3,
GCS, MinIO, a filesystem, or anything else with an atomic create; it is an
interface so tests can inject faults too (a crash between the data write and
the log write, say). [MemStore] is a complete in-memory implementation.

Keys are slash-separated paths of the form tables/<tenant>/<table>/<path>.
A Store treats them as opaque strings: it must not clean, case-fold or
otherwise rewrite them, and a key read back from List must be accepted
unchanged by Get and Delete.

Two errors are part of the contract. PutIfAbsent reports a key that already
exists with an error wrapping [ErrExists], and Get reports a key that does
not exist with an error wrapping [ErrNotFound]. barn tells these cases apart
with errors.Is, so any other error, from either method, is treated as a
failure and returned to the caller rather than read as "taken" or "absent".

Every method must honour ctx, and all of them must be safe for concurrent
use: a table reads its data files in parallel, and any number of Tables, in
this process or others, may share one Store.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table

```go
type Table struct
```

Table is a handle on one tenant's table. It is safe for concurrent use, and
any number of Tables — in this process or others — may write the same table.
The zero value is not usable; use Open.

<small>Defined in <code>barn/barn.go</code>.</small>

### VacuumResult

```go
type VacuumResult struct
```

VacuumResult lists what Vacuum deleted and what it left alone.

<small>Defined in <code>barn/maintain.go</code>.</small>

### MemStore.Delete

```go
func (m *MemStore) Delete(ctx context.Context, key string) error
```

Delete removes key. Deleting something absent is not an error: Vacuum may
race another Vacuum, and both should succeed.

<small>Defined in <code>barn/memstore.go</code>.</small>

### MemStore.Get

```go
func (m *MemStore) Get(ctx context.Context, key string) (io.ReadCloser, error)
```

Get returns a reader over key, or an error wrapping ErrNotFound if there is
no such object.

<small>Defined in <code>barn/memstore.go</code>.</small>

### MemStore.List

```go
func (m *MemStore) List(ctx context.Context, prefix string) ([]string, error)
```

List returns the keys under prefix, sorted. The Store contract allows any
order; sorting makes MemStore's answers deterministic.

<small>Defined in <code>barn/memstore.go</code>.</small>

### MemStore.Put

```go
func (m *MemStore) Put(ctx context.Context, key string, data []byte) error
```

Put writes key, overwriting any existing object.

<small>Defined in <code>barn/memstore.go</code>.</small>

### MemStore.PutIfAbsent

```go
func (m *MemStore) PutIfAbsent(ctx context.Context, key string, data []byte) error
```

PutIfAbsent creates key only if it does not exist, and returns an error
wrapping ErrExists if it does. This is the atomic create the table's
correctness depends on: two writers racing for the same log version, exactly
one winning.

<small>Defined in <code>barn/memstore.go</code>.</small>

### NewMemStore

```go
func NewMemStore() *MemStore
```

NewMemStore returns an empty in-memory store. It is equivalent to new(MemStore):
the zero value is an empty store ready to use.

<small>Defined in <code>barn/memstore.go</code>.</small>

### Open

```go
func Open(st Store, tenant, table string, opts ...Option) (*Table, error)
```

Open returns a handle on tables/<tenant>/<table>/. It does no I/O: an empty
table is simply one with no log entries yet (version 0).

<small>Defined in <code>barn/barn.go</code>.</small>

### Snapshot.ChangesSince

```go
func (s *Snapshot) ChangesSince(ctx context.Context, prefix string, after int64) ([]Change, error)
```

ChangesSince returns the rows with the prefix committed after version
`after` that this snapshot can still see, in commit order: by Version, then
Ord. It is the incremental read for a consumer that keeps its own derived
state (an index, a cache) and has already applied everything up to `after`.

It reads only the live files that a commit after `after` added; a file added
at or before `after` cannot hold a later row. Rows are NOT merged by key. In
a table whose keys are each written once (an append-only log or event stream)
that is exactly every row committed in (after, Version()]. When a key is
written more than once, a compaction that merged its rows keeps only the
winner, so an overwritten intermediate row can be missing; the newest row of
every key is always present. A whole-table compaction drops tombstones, so a
Delete can be missing too when everything it covered was compacted away.

Unlike Snapshot(v) for an old v, it never needs a vacuumed file: it reads
only files live at this snapshot.

<small>Defined in <code>barn/changes.go</code>.</small>

### Snapshot.Get

```go
func (s *Snapshot) Get(ctx context.Context, key string) (Record, bool, error)
```

Get returns key's live value in this snapshot.

<small>Defined in <code>barn/barn.go</code>.</small>

### Snapshot.Scan

```go
func (s *Snapshot) Scan(ctx context.Context, prefix string) ([]Record, error)
```

Scan returns live rows with the prefix, sorted by key.

<small>Defined in <code>barn/barn.go</code>.</small>

### Snapshot.Version

```go
func (s *Snapshot) Version() int64
```

Version is the commit this snapshot reflects.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.Compact

```go
func (t *Table) Compact(ctx context.Context) (CompactResult, error)
```

Compact merges small and tombstone-heavy files into one file, keeping only
the winning row per key.

Tombstones are dropped only when the merge covers every live file: then any
file outside it was committed later, so nothing older can be resurrected.
A partial merge keeps them. Rows keep their original (version, ordinal), so a
compaction that commits after a concurrent append never beats that append.

On a conflict (a concurrent compaction already removed one of these files)
the output is deleted and Compact re-plans from the new head, up to the
limit set by WithMaxAttempts.

<small>Defined in <code>barn/maintain.go</code>.</small>

### Table.Delete

```go
func (t *Table) Delete(ctx context.Context, keys ...string) (int64, error)
```

Delete writes tombstones for keys in one commit and returns its version.
Deleting an absent key is not an error.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.Get

```go
func (t *Table) Get(ctx context.Context, key string) (Record, bool, error)
```

Get returns the live value of key at the head.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.LastCheckpointError

```go
func (t *Table) LastCheckpointError() error
```

LastCheckpointError reports the most recent failure to write a checkpoint, or
nil. A checkpoint failure never fails the commit that triggered it (the
commit is already durable); it only makes later cold reads replay more log.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.Latest

```go
func (t *Table) Latest(ctx context.Context) (*Snapshot, error)
```

Latest returns a snapshot of the head.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.Put

```go
func (t *Table) Put(ctx context.Context, rows ...Row) (int64, error)
```

Put upserts rows in one commit and returns its version. Within one call a
repeated key resolves to its last occurrence.

A non-nil error does not always mean nothing was committed: if the log write
itself fails ambiguously (a network error after the server applied it) the
commit may have landed. Put never retries such an error, so it never commits
twice.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.Scan

```go
func (t *Table) Scan(ctx context.Context, prefix string) ([]Record, error)
```

Scan returns every live row whose key starts with prefix, sorted by key.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.Snapshot

```go
func (t *Table) Snapshot(ctx context.Context, version int64) (*Snapshot, error)
```

Snapshot returns the table as of version (time travel). Version 0 is the
empty table. Versions whose files Vacuum has deleted fail on read.

<small>Defined in <code>barn/barn.go</code>.</small>

### Table.Vacuum

```go
func (t *Table) Vacuum(ctx context.Context, retention time.Duration) (VacuumResult, error)
```

Vacuum deletes data files that no retained version needs:

  - files removed (by compaction) at a commit older than retention, and
  - orphans — files no commit ever added, left by a writer that crashed or
    lost its commit — created longer ago than retention.

Live files, and files still reachable from a version within the window, are
kept. Log entries and checkpoints are never deleted (they are small, and the
log is what makes the table readable); time travel to a version whose
removed files have been vacuumed fails on read.

retention must be at least MinRetention, so an in-flight commit's freshly
written files are never mistaken for orphans (commits older than MaxCommitAge
refuse to publish).

<small>Defined in <code>barn/maintain.go</code>.</small>

### Table.Version

```go
func (t *Table) Version(ctx context.Context) (int64, error)
```

Version returns the head version (0 for an empty table).

<small>Defined in <code>barn/barn.go</code>.</small>

### WithCheckpointInterval

```go
func WithCheckpointInterval(k int64) Option
```

WithCheckpointInterval writes a checkpoint every k versions (default 10; 0
disables writing them — reading still uses any that exist).

<small>Defined in <code>barn/barn.go</code>.</small>

### WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock replaces time.Now (tests).

<small>Defined in <code>barn/barn.go</code>.</small>

### WithCompaction

```go
func WithCompaction(smallFileBytes int64, tombstoneRatio float64) Option
```

WithCompaction sets which files Compact merges: files smaller than
smallFileBytes, and files whose tombstone fraction is at least
tombstoneRatio. Defaults: 1 MiB and 0.3.

<small>Defined in <code>barn/barn.go</code>.</small>

### WithMaxAttempts

```go
func WithMaxAttempts(n int) Option
```

WithMaxAttempts bounds how many times a commit retries after losing the race
for the next version (default 32).

<small>Defined in <code>barn/barn.go</code>.</small>
