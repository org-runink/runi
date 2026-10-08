// SPDX-License-Identifier: BSD-3-Clause

// Package tablelog is an append-only, versioned key/value table kept entirely on
// the object store — the Delta/Iceberg ("Snowflake/Databricks-style") pattern,
// scaled down to application metadata. There is no database process: a table is
// a set of immutable Avro data files plus a transaction log of JSON entries, and
// the only coordination primitive is the object store's atomic create
// (PutIfAbsent: an atomic create).
//
// # Layout
//
// Everything for one table lives under tables/<tenant>/<table>/:
//
//	data/<hex>.avro            immutable Avro OCF rows (key, op put|del, ts, payload, ver, ord)
//	_log/%020d.json            commit N: {version, parent, ts, writer, add, remove, op}
//	_checkpoint/%020d.avro     the full live file set as of version N
//	_checkpoint/_last          the newest checkpoint version — a HINT only
//
// A data file's name is 16 hex digits of its creation time (unix µs) followed by
// 32 random hex digits (crypto/rand). The time prefix lets Vacuum age an orphan
// without reading it and without trusting server mtimes, which many object
// stores do not expose.
//
// # Commit
//
// Optimistic concurrency: read the head (the _last hint, then a LIST of _log/),
// write the data file, then PutIfAbsent(_log/<head+1>). Losing that race yields
// ErrExists; the writer re-reads the head and conflict-checks. Appends never
// conflict. A compaction conflicts when a file it removes is no longer live; it
// then re-plans from the new head. Retries use jittered exponential backoff up
// to MaxAttempts. Versions are therefore dense: 1, 2, 3, … with no gaps.
//
// # Read
//
// State = checkpoint + replay of later log entries. Rows merge by key with
// last-writer-wins on (version, ordinal); a del row is a tombstone. Snapshot(v)
// reads the table as of any retained version (time travel).
//
// # Encryption is the Store's business, not this package's
//
// tablelog does not encrypt anything. It hands bytes to the [Store] and reads
// them back, so whether objects are sealed at rest is entirely a property of
// the implementation you supply.
//
// If you do seal them, bind the ciphertext to the object's KEY. Every key here
// already encodes tenant, table and path (tables/<tenant>/<table>/<path>), so
// binding to the key gives you the property worth having: a data file copied
// into another tenant's or table's prefix fails to open rather than being
// served as that table's data. A scheme that binds only to the bucket, or to
// nothing, will decrypt a misplaced file happily.
//
// # What it is not
//
// No joins, no multi-table transactions, no secondary indexes, no uniqueness
// beyond the key, no SQL. Workloads that need those stay on Litestream-replicated
// SQLite. See the package section of the store README.
package tablelog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the object-store surface tablelog needs. Implement it over S3,
// GCS, MinIO, a filesystem, or anything else with an atomic create; it is an
// interface so tests can inject faults too (a crash between the data write and
// the log write, say).
//
// PutIfAbsent must be an atomic create that reports a lost race as an error
// wrapping ErrExists. Everything the table correctness rests on is
// that one primitive.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	PutIfAbsent(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
}

var (
	// ErrExists is what a Store's PutIfAbsent must return (or wrap) when the
	// key already exists. It is the single primitive the table's correctness
	// rests on: two writers racing for the same log version, and exactly one
	// of them winning. A Store that reports a lost race any other way will
	// corrupt the table rather than retry, so implementations should wrap this
	// error rather than inventing their own.
	ErrExists = errors.New("tablelog: object already exists")

	// ErrConflict: a compaction's removed file was already removed by a
	// concurrent commit. Compact handles it by re-planning; it only surfaces
	// when re-planning also runs out of attempts.
	ErrConflict = errors.New("tablelog: commit conflict")
	// ErrRetriesExhausted: every attempt lost the race for the next log version.
	ErrRetriesExhausted = errors.New("tablelog: commit retries exhausted")
	// ErrNoVersion: the requested version has not been committed.
	ErrNoVersion = errors.New("tablelog: no such version")
	// ErrCorrupt: the log or a checkpoint is inconsistent (a gap in versions, an
	// entry that removes a file that is not live, a malformed entry).
	ErrCorrupt = errors.New("tablelog: corrupt table")
	// ErrStaleCommit: the commit's data files are older than MaxCommitAge, so a
	// concurrent Vacuum may already have treated them as orphans. The commit is
	// refused rather than risk a log entry that references a deleted file.
	ErrStaleCommit = errors.New("tablelog: commit too old to be vacuum-safe")
)

const (
	// MinRetention is the smallest retention Vacuum accepts. It must exceed
	// MaxCommitAge, so an in-flight commit's data files are never vacuumed.
	MinRetention = 10 * time.Minute
	// MaxCommitAge is how long a commit may take from writing its data files to
	// winning its log version. Past it the commit fails with ErrStaleCommit.
	MaxCommitAge = 5 * time.Minute
)

// Row is one key/value put.
type Row struct {
	Key     string
	Payload []byte
}

// Record is one live row as read back.
type Record struct {
	Key     string
	Payload []byte
	Version int64     // the commit that wrote this value
	TS      time.Time // when the writer wrote it
}

// Option configures a Table.
type Option func(*config)

type config struct {
	maxAttempts     int
	checkpointEvery int64
	smallFileBytes  int64
	tombstoneRatio  float64
	now             func() time.Time
	backoffBase     time.Duration
	backoffMax      time.Duration
}

// WithMaxAttempts bounds how many times a commit retries after losing the race
// for the next version (default 32).
func WithMaxAttempts(n int) Option { return func(c *config) { c.maxAttempts = n } }

// WithCheckpointInterval writes a checkpoint every k versions (default 10; 0
// disables writing them — reading still uses any that exist).
func WithCheckpointInterval(k int64) Option { return func(c *config) { c.checkpointEvery = k } }

// WithCompaction sets which files Compact merges: files smaller than
// smallFileBytes, and files whose tombstone fraction is at least
// tombstoneRatio. Defaults: 1 MiB and 0.3.
func WithCompaction(smallFileBytes int64, tombstoneRatio float64) Option {
	return func(c *config) { c.smallFileBytes, c.tombstoneRatio = smallFileBytes, tombstoneRatio }
}

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(c *config) { c.now = now } }

// Table is a handle on one tenant's table. It is safe for concurrent use, and
// any number of Tables — in this process or others — may write the same table.
type Table struct {
	st     Store
	tenant string
	table  string
	root   string
	writer string
	cfg    config

	mu    sync.Mutex
	cache *state // newest head this handle has read; immutable once set
	cpErr error
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Open returns a handle on tables/<tenant>/<table>/. It does no I/O: an empty
// table is simply one with no log entries yet (version 0).
func Open(st Store, tenant, table string, opts ...Option) (*Table, error) {
	if st == nil {
		return nil, errors.New("tablelog: nil store")
	}
	for _, n := range []string{tenant, table} {
		if !nameRE.MatchString(n) {
			return nil, fmt.Errorf("tablelog: invalid tenant/table name %q (want %s)", n, nameRE)
		}
	}
	cfg := config{
		maxAttempts:     32,
		checkpointEvery: 10,
		smallFileBytes:  1 << 20,
		tombstoneRatio:  0.3,
		now:             time.Now,
		backoffBase:     2 * time.Millisecond,
		backoffMax:      250 * time.Millisecond,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.maxAttempts < 1 {
		cfg.maxAttempts = 1
	}
	return &Table{
		st: st, tenant: tenant, table: table,
		root:   "tables/" + tenant + "/" + table + "/",
		writer: randHex(8),
		cfg:    cfg,
	}, nil
}

// Put upserts rows in one commit and returns its version. Within one call a
// repeated key resolves to its last occurrence.
//
// A non-nil error does not always mean nothing was committed: if the log write
// itself fails ambiguously (a network error after the server applied it) the
// commit may have landed. Put never retries such an error, so it never commits
// twice.
func (t *Table) Put(ctx context.Context, rows ...Row) (int64, error) {
	if len(rows) == 0 {
		return 0, errors.New("tablelog: Put with no rows")
	}
	now := t.cfg.now().UnixMicro()
	rs := make([]row, len(rows))
	for i, r := range rows {
		if r.Key == "" {
			return 0, errors.New("tablelog: empty key")
		}
		rs[i] = row{key: r.Key, ts: now, payload: r.Payload, ord: int64(i)}
	}
	return t.append(ctx, rs)
}

// Delete writes tombstones for keys in one commit and returns its version.
// Deleting an absent key is not an error.
func (t *Table) Delete(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, errors.New("tablelog: Delete with no keys")
	}
	now := t.cfg.now().UnixMicro()
	rs := make([]row, len(keys))
	for i, k := range keys {
		if k == "" {
			return 0, errors.New("tablelog: empty key")
		}
		rs[i] = row{key: k, del: true, ts: now, ord: int64(i)}
	}
	return t.append(ctx, rs)
}

func (t *Table) append(ctx context.Context, rs []row) (int64, error) {
	fe, err := t.writeDataFile(ctx, rs)
	if err != nil {
		return 0, err
	}
	v, err := t.commit(ctx, action{op: opAppend, add: []fileEntry{fe}})
	if errors.Is(err, ErrRetriesExhausted) || errors.Is(err, ErrStaleCommit) {
		// Definitely not referenced by any log entry: every attempt lost with
		// ErrExists, or none was made. Anything else leaves an orphan for Vacuum.
		_ = t.st.Delete(ctx, t.root+fe.Path)
	}
	return v, err
}

// Get returns the live value of key at the head.
func (t *Table) Get(ctx context.Context, key string) (Record, bool, error) {
	s, err := t.Latest(ctx)
	if err != nil {
		return Record{}, false, err
	}
	return s.Get(ctx, key)
}

// Scan returns every live row whose key starts with prefix, sorted by key.
func (t *Table) Scan(ctx context.Context, prefix string) ([]Record, error) {
	s, err := t.Latest(ctx)
	if err != nil {
		return nil, err
	}
	return s.Scan(ctx, prefix)
}

// Version returns the head version (0 for an empty table).
func (t *Table) Version(ctx context.Context) (int64, error) {
	s, err := t.loadState(ctx, -1)
	if err != nil {
		return 0, err
	}
	return s.version, nil
}

// LastCheckpointError reports the most recent failure to write a checkpoint, or
// nil. A checkpoint failure never fails the commit that triggered it (the
// commit is already durable); it only makes later cold reads replay more log.
func (t *Table) LastCheckpointError() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cpErr
}

// Snapshot is an immutable view of the table at one version.
type Snapshot struct {
	t *Table
	s *state
}

// Latest returns a snapshot of the head.
func (t *Table) Latest(ctx context.Context) (*Snapshot, error) {
	s, err := t.loadState(ctx, -1)
	if err != nil {
		return nil, err
	}
	return &Snapshot{t: t, s: s}, nil
}

// Snapshot returns the table as of version (time travel). Version 0 is the
// empty table. Versions whose files Vacuum has deleted fail on read.
func (t *Table) Snapshot(ctx context.Context, version int64) (*Snapshot, error) {
	if version < 0 {
		return nil, fmt.Errorf("%w: %d", ErrNoVersion, version)
	}
	s, err := t.loadState(ctx, version)
	if err != nil {
		return nil, err
	}
	return &Snapshot{t: t, s: s}, nil
}

// Version is the commit this snapshot reflects.
func (s *Snapshot) Version() int64 { return s.s.version }

// Get returns key's live value in this snapshot.
func (s *Snapshot) Get(ctx context.Context, key string) (Record, bool, error) {
	var files []fileEntry
	for _, f := range s.s.sortedFiles() {
		if f.MinKey <= key && key <= f.MaxKey {
			files = append(files, f)
		}
	}
	best, err := s.t.merge(ctx, files, func(k string) bool { return k == key })
	if err != nil {
		return Record{}, false, err
	}
	r, ok := best[key]
	if !ok || r.del {
		return Record{}, false, nil
	}
	return r.record(), true, nil
}

// Scan returns live rows with the prefix, sorted by key.
func (s *Snapshot) Scan(ctx context.Context, prefix string) ([]Record, error) {
	end, bounded := prefixEnd(prefix)
	var files []fileEntry
	for _, f := range s.s.sortedFiles() {
		if f.MaxKey < prefix || (bounded && f.MinKey >= end) {
			continue
		}
		files = append(files, f)
	}
	best, err := s.t.merge(ctx, files, func(k string) bool { return strings.HasPrefix(k, prefix) })
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(best))
	for _, r := range best {
		if !r.del {
			out = append(out, r.record())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// merge reads files and keeps, per matching key, the row with the greatest
// (version, ordinal). Tombstones are kept in the result so callers can tell
// "deleted" from "never written"; they filter them.
func (t *Table) merge(ctx context.Context, files []fileEntry, match func(string) bool) (map[string]row, error) {
	all, err := t.readFiles(ctx, files)
	if err != nil {
		return nil, err
	}
	best := map[string]row{}
	for _, rows := range all {
		for _, r := range rows {
			if !match(r.key) {
				continue
			}
			if cur, ok := best[r.key]; !ok || r.newer(cur) {
				best[r.key] = r
			}
		}
	}
	return best, nil
}

// prefixEnd returns the smallest string greater than every string with prefix
// p, and false when there is none (p empty or all 0xff).
func prefixEnd(p string) (string, bool) {
	b := []byte(p)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return string(b[:i+1]), true
		}
	}
	return "", false
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("tablelog: crypto/rand: " + err.Error()) // never fails on supported platforms
	}
	return hex.EncodeToString(b)
}
