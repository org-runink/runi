// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/org-runink/runi/avro"
)

const (
	opAppend  = "append"
	opCompact = "compact"
)

// fileEntry describes one live data file.
type fileEntry struct {
	Path   string `json:"path"`
	Rows   int64  `json:"rows"`
	MinKey string `json:"minKey"`
	MaxKey string `json:"maxKey"`
	Bytes  int64  `json:"bytes"`
	Dels   int64  `json:"dels,omitempty"`
	// Version is the commit that added the file. It is implied by the log entry
	// that carries the add, so it is not serialised there; checkpoints store it.
	Version int64 `json:"-"`
}

// logEntry is one _log/%020d.json commit.
type logEntry struct {
	Version int64       `json:"version"`
	Parent  int64       `json:"parent"`
	TS      int64       `json:"ts"` // unix µs, writer's clock
	Writer  string      `json:"writer"`
	Add     []fileEntry `json:"add"`
	Remove  []string    `json:"remove"`
	Op      string      `json:"op"`
}

// state is the live file set at one version. Once built it is never mutated —
// apply works on a clone — so it can be cached and shared by snapshots.
type state struct {
	version int64
	files   map[string]fileEntry

	// sortedFiles' answer, computed once. A state is immutable, so the order
	// cannot go stale; every read of the table asks for it, and sorting the
	// whole live file set per Scan, per Get and per Compact plan is work the
	// answer to which never changes.
	sortOnce sync.Once
	sorted   []fileEntry
}

func emptyState() *state { return &state{files: map[string]fileEntry{}} }

func (s *state) clone() *state {
	// A fresh zero sync.Once: the clone is about to be mutated by apply, so it
	// must compute its own order rather than inherit the original's.
	return &state{version: s.version, files: maps.Clone(s.files)}
}

// sortedFiles returns the live files in the order they were committed. The
// slice is shared with every other caller and memoised, so callers read it and
// never write to it.
//
// The order is the commit Version, not the path. A data file is named for the
// microsecond it was created, so sorting by path USUALLY gives commit order —
// and silently stops doing so the moment two files share a timestamp, because
// the rest of the name is random. That is not a theoretical window: it depends
// on the platform's clock granularity, which on Windows is between 0.5 ms and
// 15.6 ms, so a fast writer puts several commits inside one tick and they come
// back in random order. Version is assigned by the log and is exactly the
// sequence we mean, so it holds however quick the writer is and however coarse
// the clock. Path breaks ties within a single commit, which keeps the result
// deterministic rather than map-order.
func (s *state) sortedFiles() []fileEntry {
	s.sortOnce.Do(func() {
		out := make([]fileEntry, 0, len(s.files))
		for _, f := range s.files {
			out = append(out, f)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Version != out[j].Version {
				return out[i].Version < out[j].Version
			}
			return out[i].Path < out[j].Path
		})
		s.sorted = out
	})
	return s.sorted
}

// apply returns the state after e, leaving s untouched. It refuses entries that
// do not chain.
func (s *state) apply(e *logEntry) (*state, error) {
	n := s.clone()
	if err := n.applyHere(e); err != nil {
		return nil, err
	}
	return n, nil
}

// applyHere applies e to s IN PLACE. It is for a state no one else can see yet
// — a clone being caught up by a replay — because a state that someone has
// read is immutable by contract: a snapshot taken at an old version must never
// change, and a cached head must not change under a concurrent reader. A
// failed call leaves s half-applied and fit only to be discarded.
//
// Replaying in place is what keeps a cold read linear in the log it replays.
// Cloning the whole live file set per entry made catching up n commits cost
// n²/2 map inserts, which is exactly the shape of cost that is invisible on a
// short log and ruinous on a long one.
func (s *state) applyHere(e *logEntry) error {
	if e.Version != s.version+1 || e.Parent != s.version {
		return fmt.Errorf("%w: entry v%d (parent %d) does not follow v%d", ErrCorrupt, e.Version, e.Parent, s.version)
	}
	s.version = e.Version
	for _, p := range e.Remove {
		if _, ok := s.files[p]; !ok {
			return fmt.Errorf("%w: v%d removes %s, which is not live", ErrCorrupt, e.Version, p)
		}
		delete(s.files, p)
	}
	for _, f := range e.Add {
		if _, ok := s.files[f.Path]; ok || !strings.HasPrefix(f.Path, "data/") {
			return fmt.Errorf("%w: v%d adds %s twice or outside data/", ErrCorrupt, e.Version, f.Path)
		}
		f.Version = e.Version
		s.files[f.Path] = f
	}
	return nil
}

func (t *Table) logKey(v int64) string { return fmt.Sprintf("%s_log/%020d.json", t.root, v) }
func (t *Table) checkpointKey(v int64) string {
	return fmt.Sprintf("%s_checkpoint/%020d.avro", t.root, v)
}
func (t *Table) lastKey() string { return t.root + "_checkpoint/_last" }

// listVersions lists dir (under the table root) and returns the versions of
// objects named %020d<ext>, ascending. Other names are ignored.
func (t *Table) listVersions(ctx context.Context, dir, ext string) ([]int64, error) {
	keys, err := t.st.List(ctx, t.root+dir)
	if err != nil {
		return nil, fmt.Errorf("tablelog: list %s: %w", dir, err)
	}
	var out []int64
	for _, k := range keys {
		name, ok := strings.CutSuffix(strings.TrimPrefix(k, t.root+dir), ext)
		if !ok || len(name) != 20 {
			continue
		}
		v, err := strconv.ParseInt(name, 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (t *Table) readObject(ctx context.Context, key string) ([]byte, error) {
	rc, err := t.st.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (t *Table) readLog(ctx context.Context, v int64) (*logEntry, error) {
	body, err := t.readObject(ctx, t.logKey(v))
	if err != nil {
		return nil, fmt.Errorf("tablelog: read log v%d: %w", v, err)
	}
	var e logEntry
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("%w: log v%d: %v", ErrCorrupt, v, err)
	}
	if e.Version != v {
		return nil, fmt.Errorf("%w: log object v%d says version %d", ErrCorrupt, v, e.Version)
	}
	return &e, nil
}

// loadState returns the state at target (-1 = head).
//
// Head discovery is a LIST of _log/; the base it replays from is the newest of
// (this handle's cached head, the checkpoint the _last hint names — or, for time
// travel, the newest checkpoint ≤ target). Log entries are never deleted, so a
// missing or stale hint or checkpoint only costs replay, never correctness.
func (t *Table) loadState(ctx context.Context, target int64) (*state, error) {
	logs, err := t.listVersions(ctx, "_log/", ".json")
	if err != nil {
		return nil, err
	}
	var head int64
	if n := len(logs); n > 0 {
		head = logs[n-1]
	}
	if int64(len(logs)) != head {
		return nil, fmt.Errorf("%w: %d log entries but head is v%d (gap in the log)", ErrCorrupt, len(logs), head)
	}
	atHead := target < 0 || target == head
	if target < 0 {
		target = head
	}
	if target > head {
		return nil, fmt.Errorf("%w: v%d (head is v%d)", ErrNoVersion, target, head)
	}

	base := emptyState()
	t.mu.Lock()
	if c := t.cache; c != nil && c.version <= target {
		base = c
	}
	t.mu.Unlock()

	if cp := t.bestCheckpoint(ctx, target, atHead); cp > base.version {
		if s, err := t.readCheckpoint(ctx, cp); err == nil {
			base = s
		} // an unreadable checkpoint is skipped: replay from the older base is still correct
	}

	s := base
	for v := s.version + 1; v <= target; v++ {
		e, err := t.readLog(ctx, v)
		if err != nil {
			return nil, err
		}
		if s == base {
			s = base.clone() // base is shared; the replay's own copy is not
		}
		if err := s.applyHere(e); err != nil {
			return nil, err
		}
	}
	if atHead {
		t.remember(s)
	}
	return s, nil
}

func (t *Table) remember(s *state) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cache == nil || s.version > t.cache.version {
		t.cache = s
	}
}

// speculativeHead returns the state this handle may commit on top of without
// re-reading the log, or nil. It is set only by a commit this handle won: that
// is the one thing that proves the handle was at the head, and the proof is
// still good now unless someone else has committed since — which the atomic
// create, not this guess, is what detects.
func (t *Table) speculativeHead() *state {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.atHead {
		return nil
	}
	return t.cache
}

// wonVersion records that this handle published s, so the next commit may
// speculate on it.
func (t *Table) wonVersion(s *state) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cache == nil || s.version > t.cache.version {
		t.cache = s
	}
	t.atHead = true
}

// lostSpeculation records that the guessed version was already taken, so the
// next commit reads the head properly instead of guessing again.
func (t *Table) lostSpeculation() {
	t.mu.Lock()
	t.atHead = false
	t.mu.Unlock()
}

// bestCheckpoint picks the checkpoint to start from, 0 for none. At the head it
// trusts the _last hint if it is readable and ≤ target; otherwise (and for time
// travel) it lists _checkpoint/.
func (t *Table) bestCheckpoint(ctx context.Context, target int64, atHead bool) int64 {
	if atHead {
		if body, err := t.readObject(ctx, t.lastKey()); err == nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64); err == nil && v > 0 && v <= target {
				return v
			}
		}
	}
	cps, err := t.listVersions(ctx, "_checkpoint/", ".avro")
	if err != nil {
		return 0
	}
	var best int64
	for _, v := range cps {
		if v <= target {
			best = v
		}
	}
	return best
}

const checkpointSchema = `{"type":"record","name":"CheckpointFile","namespace":"org.runink.store.tablelog","fields":[` +
	`{"name":"path","type":"string"},{"name":"rows","type":"long"},` +
	`{"name":"minKey","type":"string"},{"name":"maxKey","type":"string"},` +
	`{"name":"bytes","type":"long"},{"name":"dels","type":"long"},{"name":"version","type":"long"}]}`

func marshalCP(e *avro.Encoder, f fileEntry) {
	e.String(f.Path)
	e.Long(f.Rows)
	e.String(f.MinKey)
	e.String(f.MaxKey)
	e.Long(f.Bytes)
	e.Long(f.Dels)
	e.Long(f.Version)
}

func unmarshalCP(d *avro.Decoder) (fileEntry, error) {
	var f fileEntry
	var err error
	if f.Path, err = d.String(); err != nil {
		return f, err
	}
	if f.Rows, err = d.Long(); err != nil {
		return f, err
	}
	if f.MinKey, err = d.String(); err != nil {
		return f, err
	}
	if f.MaxKey, err = d.String(); err != nil {
		return f, err
	}
	if f.Bytes, err = d.Long(); err != nil {
		return f, err
	}
	if f.Dels, err = d.Long(); err != nil {
		return f, err
	}
	f.Version, err = d.Long()
	return f, err
}

func (t *Table) readCheckpoint(ctx context.Context, v int64) (*state, error) {
	body, err := t.readObject(ctx, t.checkpointKey(v))
	if err != nil {
		return nil, err
	}
	_, files, err := avro.ReadOCF(bytes.NewReader(body), unmarshalCP)
	if err != nil {
		return nil, fmt.Errorf("%w: checkpoint v%d: %v", ErrCorrupt, v, err)
	}
	s := &state{version: v, files: make(map[string]fileEntry, len(files))}
	for _, f := range files {
		if f.Version < 1 || f.Version > v {
			return nil, fmt.Errorf("%w: checkpoint v%d lists %s at v%d", ErrCorrupt, v, f.Path, f.Version)
		}
		s.files[f.Path] = f
	}
	return s, nil
}

// writeCheckpoint stores s as _checkpoint/<v>.avro and advances the hint. A
// concurrent writer of the same checkpoint is fine (same content, first wins).
func (t *Table) writeCheckpoint(ctx context.Context, s *state) error {
	var buf bytes.Buffer
	if err := writeCPOCF(&buf, checkpointSchema, avro.CodecDeflate, s.sortedFiles(), marshalCP); err != nil {
		return err
	}
	if err := t.st.PutIfAbsent(ctx, t.checkpointKey(s.version), buf.Bytes()); err != nil && !errors.Is(err, ErrExists) {
		return err
	}
	// The hint is advisory: only move it forward. A racing writer can still
	// move it back; readers then replay a little more.
	if body, err := t.readObject(ctx, t.lastKey()); err == nil {
		if cur, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64); err == nil && cur >= s.version {
			return nil
		}
	}
	hint := []byte(strconv.FormatInt(s.version, 10))
	return t.st.Put(ctx, t.lastKey(), bytes.NewReader(hint), int64(len(hint)), "text/plain")
}

type action struct {
	op     string
	add    []fileEntry
	remove []string
}

// commit publishes a as the next log version. It retries lost races with
// jittered exponential backoff; a compaction whose removed files are no longer
// live returns ErrConflict for the caller to re-plan.
//
// # Why the head is guessed first
//
// Reading the head is a LIST of the whole log, so a commit that always reads it
// costs more the more commits a table has already taken — the hundredth commit
// of a run paying for the ninety-nine before it. A handle that has just won a
// version does not need to ask: versions are dense, so if version+1 does not
// exist then version IS the head. Guessing it and letting PutIfAbsent decide
// turns the uncontended case into one atomic create.
//
// The guess cannot publish a wrong version. Winning the create for v+1 proves
// no one else holds v+1, and therefore (density again) that nothing was
// committed after v: the cached state is the head state, the conflict check
// below ran against the real file set, and the entry's parent is right. Losing
// it costs one create and falls back to reading the head properly — which on
// an object store is still cheaper than the LIST it replaced.
func (t *Table) commit(ctx context.Context, a action) (int64, error) {
	if s := t.speculativeHead(); s != nil {
		v, won, err := t.attemptCommit(ctx, a, s)
		if err != nil || won {
			return v, err
		}
		t.lostSpeculation()
	}
	for attempt := 0; attempt < t.cfg.maxAttempts; attempt++ {
		if attempt > 0 {
			if err := t.backoff(ctx, attempt); err != nil {
				return 0, err
			}
		}
		s, err := t.loadState(ctx, -1)
		if err != nil {
			return 0, err
		}
		v, won, err := t.attemptCommit(ctx, a, s)
		if err != nil || won {
			return v, err
		}
	}
	return 0, fmt.Errorf("%w after %d attempts", ErrRetriesExhausted, t.cfg.maxAttempts)
}

// attemptCommit tries to publish a as the version after s. won=false with a nil
// error means the version was already taken and the caller should re-read the
// head; everything else is final.
func (t *Table) attemptCommit(ctx context.Context, a action, s *state) (int64, bool, error) {
	for _, f := range a.add {
		if created, ok := dataFileCreated(f.Path); ok && t.cfg.now().Sub(created) > MaxCommitAge {
			return 0, false, fmt.Errorf("%w: %s written %s ago", ErrStaleCommit, f.Path, t.cfg.now().Sub(created))
		}
	}
	for _, p := range a.remove {
		if _, ok := s.files[p]; !ok {
			return 0, false, fmt.Errorf("%w: %s already removed by v≤%d", ErrConflict, p, s.version)
		}
	}
	e := &logEntry{
		Version: s.version + 1, Parent: s.version, TS: t.cfg.now().UnixMicro(),
		Writer: t.writer, Add: a.add, Remove: a.remove, Op: a.op,
	}
	if e.Add == nil {
		e.Add = []fileEntry{}
	}
	if e.Remove == nil {
		e.Remove = []string{}
	}
	body, err := marshalLogEntry(e)
	if err != nil {
		// Not reachable: logEntry is strings, ints and slices of those.
		// Checked rather than ignored because the day someone adds a field
		// that cannot be marshalled, this must fail the commit rather than
		// write an empty log object.
		return 0, false, err
	}
	err = t.st.PutIfAbsent(ctx, t.logKey(e.Version), body)
	if errors.Is(err, ErrExists) {
		return 0, false, nil // lost the race: the caller re-reads the head
	}
	if err != nil {
		return 0, false, fmt.Errorf("tablelog: commit v%d: %w", e.Version, err)
	}
	next, err := s.apply(e)
	if err != nil {
		return 0, false, err // cannot happen: e was built from s
	}
	t.wonVersion(next)
	if k := t.cfg.checkpointEvery; k > 0 && next.version%k == 0 {
		cpErr := t.writeCheckpoint(ctx, next)
		t.mu.Lock()
		t.cpErr = cpErr
		t.mu.Unlock()
	}
	return next.version, true, nil
}

func (t *Table) backoff(ctx context.Context, attempt int) error {
	d := t.cfg.backoffBase << min(attempt, 16)
	if d > t.cfg.backoffMax || d <= 0 {
		d = t.cfg.backoffMax
	}
	d = time.Duration(rand.Int64N(int64(d)) + 1) // full jitter
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// writeCPOCF is avro.WriteOCF behind a variable, for the same reason as
// writeRowsOCF: the failure is real but unreachable from a caller, and a
// checkpoint that cannot be encoded must not be reported as written.
var writeCPOCF = avro.WriteOCF[fileEntry]

// marshalLogEntry serialises a log entry. It is a variable so the failure can
// be exercised: logEntry holds only strings, ints and slices of those today, so
// nothing a caller does makes this fail, but the commit must refuse rather than
// write an empty log object if that ever changes. An error that is returned and
// never run is not known to work.
var marshalLogEntry = func(e *logEntry) ([]byte, error) { return json.Marshal(e) }
