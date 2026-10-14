// SPDX-License-Identifier: BSD-3-Clause

// Package tablelog is the name github.com/org-runink/runi/barn had until
// v0.8.0, kept for one release so code written against it keeps compiling
// while it moves.
//
// Every type here is an alias of the type of the same name in barn, so a
// *tablelog.Table IS a *barn.Table and the two can be passed between old and
// new code freely. The errors are barn's own values, so errors.Is matches
// across both import paths; their messages now read "barn: ...". Tables on
// disk are unaffected by the rename: barn opens tables written by tablelog.
//
// To move, replace the import path and the package name. Nothing else in the
// API changed.
//
// Deprecated: use github.com/org-runink/runi/barn. This package will be
// removed in the release after the one that introduced barn.
package tablelog

import (
	"time"

	"github.com/org-runink/runi/barn"
)

// The types, aliased so values are interchangeable with barn's.
type (
	// Deprecated: use barn.Change.
	Change = barn.Change
	// Deprecated: use barn.CompactResult.
	CompactResult = barn.CompactResult
	// Deprecated: use barn.MemStore.
	MemStore = barn.MemStore
	// Deprecated: use barn.Option.
	Option = barn.Option
	// Deprecated: use barn.Record.
	Record = barn.Record
	// Deprecated: use barn.Row.
	Row = barn.Row
	// Deprecated: use barn.Snapshot.
	Snapshot = barn.Snapshot
	// Deprecated: use barn.Store.
	Store = barn.Store
	// Deprecated: use barn.Table.
	Table = barn.Table
	// Deprecated: use barn.VacuumResult.
	VacuumResult = barn.VacuumResult
)

// The retention limits, unchanged.
const (
	// Deprecated: use barn.MinRetention.
	MinRetention = barn.MinRetention
	// Deprecated: use barn.MaxCommitAge.
	MaxCommitAge = barn.MaxCommitAge
)

// The errors: barn's own values, so errors.Is matches either name.
var (
	// Deprecated: use barn.ErrExists.
	ErrExists = barn.ErrExists
	// Deprecated: use barn.ErrConflict.
	ErrConflict = barn.ErrConflict
	// Deprecated: use barn.ErrRetriesExhausted.
	ErrRetriesExhausted = barn.ErrRetriesExhausted
	// Deprecated: use barn.ErrNoVersion.
	ErrNoVersion = barn.ErrNoVersion
	// Deprecated: use barn.ErrCorrupt.
	ErrCorrupt = barn.ErrCorrupt
	// Deprecated: use barn.ErrStaleCommit.
	ErrStaleCommit = barn.ErrStaleCommit
)

// Open is barn.Open.
//
// Deprecated: use barn.Open.
func Open(st Store, tenant, table string, opts ...Option) (*Table, error) {
	return barn.Open(st, tenant, table, opts...)
}

// NewMemStore is barn.NewMemStore.
//
// Deprecated: use barn.NewMemStore.
func NewMemStore() *MemStore { return barn.NewMemStore() }

// WithMaxAttempts is barn.WithMaxAttempts.
//
// Deprecated: use barn.WithMaxAttempts.
func WithMaxAttempts(n int) Option { return barn.WithMaxAttempts(n) }

// WithCheckpointInterval is barn.WithCheckpointInterval.
//
// Deprecated: use barn.WithCheckpointInterval.
func WithCheckpointInterval(k int64) Option { return barn.WithCheckpointInterval(k) }

// WithCompaction is barn.WithCompaction.
//
// Deprecated: use barn.WithCompaction.
func WithCompaction(smallFileBytes int64, tombstoneRatio float64) Option {
	return barn.WithCompaction(smallFileBytes, tombstoneRatio)
}

// WithClock is barn.WithClock.
//
// Deprecated: use barn.WithClock.
func WithClock(now func() time.Time) Option { return barn.WithClock(now) }
