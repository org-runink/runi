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
	sort.Slice(out, func(i, j int) bool { return changeLess(out[i], out[j]) })
	return out, nil
}

// changeLess orders a feed: by the commit that wrote the change, then by the
// row's position within that commit, then by key.
//
// The key comparison does not decide anything on data this package produces —
// Ord is unique within a commit, so two changes at the same version never tie —
// and it is here so the ordering is a TOTAL order rather than a partial one.
// sort.Slice is not stable, and a comparator that reports neither a<b nor b<a
// for distinct elements lets them come back in different orders on different
// runs, which in a change feed means a consumer replaying the same version
// twice can apply it two different ways. It is a named function so that
// property can be tested directly, instead of being asserted about in a
// comment that nothing checks.
func changeLess(a, b Change) bool {
	if a.Version != b.Version {
		return a.Version < b.Version
	}
	if a.Ord != b.Ord {
		return a.Ord < b.Ord
	}
	return a.Key < b.Key
}
