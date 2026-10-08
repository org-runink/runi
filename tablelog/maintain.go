// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CompactResult describes one Compact call. Version 0 means there was nothing
// worth compacting and no commit was made.
type CompactResult struct {
	Version int64
	Removed int // files merged away
	Added   int // files written (0 when every surviving row was a dropped tombstone)
	Rows    int // rows in the output
}

// Compact merges small and tombstone-heavy files into one file, keeping only
// the winning row per key.
//
// Tombstones are dropped only when the merge covers every live file: then any
// file outside it was committed later, so nothing older can be resurrected.
// A partial merge keeps them. Rows keep their original (version, ordinal), so a
// compaction that commits after a concurrent append never beats that append.
//
// On a conflict (a concurrent compaction already removed one of these files)
// the output is deleted and Compact re-plans from the new head, up to
// MaxAttempts times.
func (t *Table) Compact(ctx context.Context) (CompactResult, error) {
	var lastErr error
	for plan := 0; plan < t.cfg.maxAttempts; plan++ {
		if plan > 0 {
			if err := t.backoff(ctx, plan); err != nil {
				return CompactResult{}, err
			}
		}
		res, err := t.compactOnce(ctx)
		if errors.Is(err, ErrConflict) {
			lastErr = err
			continue
		}
		return res, err
	}
	return CompactResult{}, fmt.Errorf("tablelog: compact gave up after %d plans: %w", t.cfg.maxAttempts, lastErr)
}

func (t *Table) compactOnce(ctx context.Context) (CompactResult, error) {
	s, err := t.loadState(ctx, -1)
	if err != nil {
		return CompactResult{}, err
	}
	var cands []fileEntry
	heavy := false
	for _, f := range s.sortedFiles() {
		tombHeavy := f.Rows > 0 && float64(f.Dels)/float64(f.Rows) >= t.cfg.tombstoneRatio
		if f.Bytes < t.cfg.smallFileBytes || tombHeavy {
			cands = append(cands, f)
			heavy = heavy || tombHeavy
		}
	}
	if len(cands) == 0 || (len(cands) == 1 && !heavy) {
		return CompactResult{}, nil
	}
	whole := len(cands) == len(s.files)

	best, err := t.merge(ctx, cands, func(string) bool { return true })
	if err != nil {
		return CompactResult{}, err
	}
	out := make([]row, 0, len(best))
	for _, r := range best {
		if r.del && whole {
			continue
		}
		out = append(out, r) // r.ver is already resolved to a real version
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })

	a := action{op: opCompact}
	for _, f := range cands {
		a.remove = append(a.remove, f.Path)
	}
	if len(out) > 0 {
		fe, err := t.writeDataFile(ctx, out)
		if err != nil {
			return CompactResult{}, err
		}
		a.add = []fileEntry{fe}
	}
	v, err := t.commit(ctx, a)
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrRetriesExhausted) || errors.Is(err, ErrStaleCommit) {
			for _, f := range a.add { // provably unreferenced: no attempt won a version
				_ = t.st.Delete(ctx, t.root+f.Path)
			}
		}
		return CompactResult{}, err
	}
	return CompactResult{Version: v, Removed: len(a.remove), Added: len(a.add), Rows: len(out)}, nil
}

// VacuumResult lists what Vacuum deleted and what it left alone.
type VacuumResult struct {
	Deleted []string // table-relative paths
	Skipped []string // objects under data/ with names this package did not write
}

// Vacuum deletes data files that no retained version needs:
//
//   - files removed (by compaction) at a commit older than retention, and
//   - orphans — files no commit ever added, left by a writer that crashed or
//     lost its commit — created longer ago than retention.
//
// Live files, and files still reachable from a version within the window, are
// kept. Log entries and checkpoints are never deleted (they are small, and the
// log is what makes the table readable); time travel to a version whose
// removed files have been vacuumed fails on read.
//
// retention must be at least MinRetention, so an in-flight commit's freshly
// written files are never mistaken for orphans (commits older than MaxCommitAge
// refuse to publish).
func (t *Table) Vacuum(ctx context.Context, retention time.Duration) (VacuumResult, error) {
	var res VacuumResult
	if retention < MinRetention {
		return res, fmt.Errorf("tablelog: vacuum retention %s is below the %s minimum", retention, MinRetention)
	}
	now := t.cfg.now()
	cutoff := now.Add(-retention)

	// Full history: the removal times are only in the log entries.
	head, err := t.Version(ctx)
	if err != nil {
		return res, err
	}
	s := emptyState()
	added := map[string]bool{}
	removedAt := map[string]time.Time{}
	for v := int64(1); v <= head; v++ {
		e, err := t.readLog(ctx, v)
		if err != nil {
			return res, err
		}
		if s, err = s.apply(e); err != nil {
			return res, err
		}
		for _, f := range e.Add {
			added[f.Path] = true
		}
		for _, p := range e.Remove {
			removedAt[p] = time.UnixMicro(e.TS)
		}
	}

	keys, err := t.st.List(ctx, t.root+"data/")
	if err != nil {
		return res, fmt.Errorf("tablelog: list data/: %w", err)
	}
	for _, k := range keys {
		p := strings.TrimPrefix(k, t.root)
		if _, live := s.files[p]; live {
			continue
		}
		var old bool
		if added[p] {
			old = !removedAt[p].After(cutoff)
		} else {
			created, ok := dataFileCreated(p)
			if !ok {
				res.Skipped = append(res.Skipped, p)
				continue
			}
			old = !created.After(cutoff)
		}
		if !old {
			continue
		}
		if err := t.st.Delete(ctx, k); err != nil {
			return res, fmt.Errorf("tablelog: vacuum %s: %w", p, err)
		}
		res.Deleted = append(res.Deleted, p)
	}
	return res, nil
}
