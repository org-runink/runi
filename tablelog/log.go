// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
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
}

func emptyState() *state { return &state{files: map[string]fileEntry{}} }

func (s *state) clone() *state {
	c := &state{version: s.version, files: make(map[string]fileEntry, len(s.files))}
	for k, v := range s.files {
		c.files[k] = v
	}
	return c
}

func (s *state) sortedFiles() []fileEntry {
	out := make([]fileEntry, 0, len(s.files))
	for _, f := range s.files {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// apply returns the state after e. It refuses entries that do not chain.
func (s *state) apply(e *logEntry) (*state, error) {
	if e.Version != s.version+1 || e.Parent != s.version {
		return nil, fmt.Errorf("%w: entry v%d (parent %d) does not follow v%d", ErrCorrupt, e.Version, e.Parent, s.version)
	}
	n := s.clone()
	n.version = e.Version
	for _, p := range e.Remove {
		if _, ok := n.files[p]; !ok {
			return nil, fmt.Errorf("%w: v%d removes %s, which is not live", ErrCorrupt, e.Version, p)
		}
		delete(n.files, p)
	}
	for _, f := range e.Add {
		if _, ok := n.files[f.Path]; ok || !strings.HasPrefix(f.Path, "data/") {
			return nil, fmt.Errorf("%w: v%d adds %s twice or outside data/", ErrCorrupt, e.Version, f.Path)
		}
		f.Version = e.Version
		n.files[f.Path] = f
	}
	return n, nil
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
		if s, err = s.apply(e); err != nil {
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
func (t *Table) commit(ctx context.Context, a action) (int64, error) {
	for attempt := 0; attempt < t.cfg.maxAttempts; attempt++ {
		if attempt > 0 {
			if err := t.backoff(ctx, attempt); err != nil {
				return 0, err
			}
		}
		for _, f := range a.add {
			if created, ok := dataFileCreated(f.Path); ok && t.cfg.now().Sub(created) > MaxCommitAge {
				return 0, fmt.Errorf("%w: %s written %s ago", ErrStaleCommit, f.Path, t.cfg.now().Sub(created))
			}
		}
		s, err := t.loadState(ctx, -1)
		if err != nil {
			return 0, err
		}
		for _, p := range a.remove {
			if _, ok := s.files[p]; !ok {
				return 0, fmt.Errorf("%w: %s already removed by v≤%d", ErrConflict, p, s.version)
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
			return 0, err
		}
		err = t.st.PutIfAbsent(ctx, t.logKey(e.Version), body)
		if errors.Is(err, ErrExists) {
			continue // lost the race: re-read the head and try the next version
		}
		if err != nil {
			return 0, fmt.Errorf("tablelog: commit v%d: %w", e.Version, err)
		}
		next, err := s.apply(e)
		if err != nil {
			return 0, err // cannot happen: e was built from s
		}
		t.remember(next)
		if k := t.cfg.checkpointEvery; k > 0 && next.version%k == 0 {
			cpErr := t.writeCheckpoint(ctx, next)
			t.mu.Lock()
			t.cpErr = cpErr
			t.mu.Unlock()
		}
		return next.version, nil
	}
	return 0, fmt.Errorf("%w after %d attempts", ErrRetriesExhausted, t.cfg.maxAttempts)
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
