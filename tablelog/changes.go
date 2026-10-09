// SPDX-License-Identifier: LicenseRef-Runink-Proprietary
// SPDX-FileCopyrightText: 2026 Runink

package tablelog

import (
	"context"
	"sort"
	"strings"
)

// Change is one row committed after a given version, as ChangesSince reads it.
type Change struct {
	Record
	// Ord is the row's position within its commit. (Version, Ord) is the row's
	// total order in the table.
	Ord int64
	// Deleted marks a tombstone written by Delete; Payload is then empty.
	Deleted bool
}

// ChangesSince returns the rows with the prefix committed after version
// `after` that this snapshot can still see, in commit order: by Version, then
// Ord. It is the incremental read for a consumer that keeps its own derived
// state (an index, a cache) and has already applied everything up to `after`.
//
// It reads only the live files that a commit after `after` added; a file added
// at or before `after` cannot hold a later row. Rows are NOT merged by key. In
// a table whose keys are each written once (an append-only log, store/twin)
// that is exactly every row committed in (after, Version()]. When a key is
// written more than once, a compaction that merged its rows keeps only the
// winner, so an overwritten intermediate row can be missing; the newest row of
// every key is always present. A whole-table compaction drops tombstones, so a
// Delete can be missing too when everything it covered was compacted away.
//
// Unlike Snapshot(v) for an old v, it never needs a vacuumed file: it reads
// only files live at this snapshot.
func (s *Snapshot) ChangesSince(ctx context.Context, prefix string, after int64) ([]Change, error) {
	if after >= s.s.version {
		return nil, nil
	}
	end, bounded := prefixEnd(prefix)
	var files []fileEntry
	for _, f := range s.s.sortedFiles() {
		if f.Version <= after {
			continue
		}
		if f.MaxKey < prefix || (bounded && f.MinKey >= end) {
			continue
		}
		files = append(files, f)
	}
	all, err := s.t.readFiles(ctx, files)
	if err != nil {
		return nil, err
	}
	var out []Change
	for _, rows := range all {
		for _, r := range rows {
			if r.ver <= after || !strings.HasPrefix(r.key, prefix) {
				continue
			}
			out = append(out, Change{Record: r.record(), Ord: r.ord, Deleted: r.del})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		if out[i].Ord != out[j].Ord {
			return out[i].Ord < out[j].Ord
		}
		// Not reachable today, and kept anyway so the comparator is total:
		// Ord is the row's position within its commit, so two changes at the
		// same version always have different Ords. A sort whose comparator is
		// only a partial order is unstable in a way that shows up as changes
		// arriving in a different order on different runs.
		return out[i].Key < out[j].Key
	})
	return out, nil
}
