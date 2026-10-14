// SPDX-License-Identifier: BSD-3-Clause

package sustain

import (
	"strconv"
	"time"
)

// Edge is one boundary between two adjacent levels of a Band, with its own
// deadband: a reading at or above Up carries the level up past the edge, and
// only a reading at or below Down carries it back. Down < Up.
type Edge struct {
	Up, Down float64
}

// Band classifies each reading into one of an ordered set of levels — below,
// inside and above a band; normal, warning and critical — with hysteresis at
// every boundary.
//
// The levels are numbered 0 to len(edges), lowest values first, and edge k
// separates level k from level k+1. Each edge is a Schmitt trigger with its
// own pair of levels, so a reading that sits near a boundary does not flip
// between the levels on either side of it: noise whose peak-to-peak amplitude
// is less than an edge's Up − Down cannot carry the level across that edge
// both ways. A reading may move the level several steps at once; a reading
// strictly between an edge's Down and Up leaves that edge where it was.
//
// For a band where the middle is normal, give two edges: level 0 is below,
// 1 inside, 2 above. For severities that rise with the value, level 0 is
// normal and each edge is a more severe alarm; for severities that rise as
// the value falls, number them from the top (severity = len(edges) − level).
//
// The edges must be in order: both Up and Down strictly increasing from one
// edge to the next. Then the level is always the number of edges whose
// trigger is on, as if each edge were a separate Hysteresis, and the tests
// hold Band to exactly that.
//
// Dwell is an optional minimum time at a level after a change, as for
// Hysteresis: a reading less than dwell after the last change cannot move the
// level, is counted in Held, and is not remembered. It applies only after a
// change; the first move from level 0 is never delayed. With a dwell, the
// level is no longer the count of separate triggers, since one change holds
// back every edge.
//
// The level starts at 0. NaN and ±Inf readings are skipped, and so are
// readings whose time is not after the last accepted reading; both are
// counted in Skipped and leave no other trace.
//
// Method: the multi-level alarm with a deadband on each setpoint of
// ANSI/ISA-18.2-2016, Management of Alarm Systems for the Process Industries;
// each edge is the trigger of O. H. Schmitt, "A thermionic trigger", Journal
// of Scientific Instruments 15(1), 1938.
//
// NewBand copies the edges; Update never changes them, so a copy of a Band
// shares them safely and is independent of the original. The zero value
// has no edges and is always at level 0; build one with NewBand. Not safe for
// concurrent use.
type Band struct {
	edges []Edge
	dwell time.Duration
	level int
	held  int64
	c     clock
}

// NewBand returns a band at level 0 with the given edges, at least one, every
// value finite, Down < Up within each edge and both strictly increasing from
// edge to edge. dwell ≥ 0 is the minimum time at a level after a change; 0
// means none.
func NewBand(dwell time.Duration, edges ...Edge) (Band, error) {
	if dwell < 0 {
		return Band{}, invalid("dwell", dwell, ">= 0")
	}
	if len(edges) == 0 {
		return Band{}, invalid("len(edges)", 0, ">= 1")
	}
	for i, e := range edges {
		name := "edges[" + strconv.Itoa(i) + "]"
		switch {
		case !finite(e.Up) || !finite(e.Down):
			return Band{}, invalid(name, e, "finite Up and Down")
		case !(e.Down < e.Up):
			return Band{}, invalid(name, e, "Down < Up")
		case i > 0 && !(edges[i-1].Up < e.Up && edges[i-1].Down < e.Down):
			return Band{}, invalid(name, e, "Up and Down above the previous edge's")
		}
	}
	return Band{edges: append([]Edge(nil), edges...), dwell: dwell}, nil
}

// Update judges reading x taken at time at and returns the level after it.
// It does not allocate.
func (b *Band) Update(x float64, at time.Time) int {
	if !finite(x) {
		b.c.skipped++
		return b.level
	}
	if !b.c.accept(at) {
		return b.level
	}
	k := b.level
	for k < len(b.edges) && x >= b.edges[k].Up {
		k++
	}
	for k > 0 && x <= b.edges[k-1].Down {
		k--
	}
	if k == b.level {
		return k
	}
	if !b.c.settled(at, b.dwell) {
		b.held++
		return b.level
	}
	b.level = k
	b.c.change(at)
	return k
}

// Level is the current level, 0 to Levels() − 1.
func (b *Band) Level() int { return b.level }

// Levels is the number of levels: one more than the number of edges.
func (b *Band) Levels() int { return len(b.edges) + 1 }

// Since is the time of the reading that entered the current level: the first
// reading's time before any change, the zero time before any reading.
func (b *Band) Since() time.Time { return b.c.since }

// InState is how long the current level has lasted as of at, zero before the
// first reading and for an at before Since.
func (b *Band) InState(at time.Time) time.Duration { return b.c.inState(at) }

// Transitions is the number of changes of level; a move of several levels in
// one reading counts once.
func (b *Band) Transitions() int64 { return b.c.changes }

// Held is the number of readings that would have changed the level but came
// within the dwell.
func (b *Band) Held() int64 { return b.held }

// Skipped is the number of readings refused: non-finite, or not after the
// last accepted reading.
func (b *Band) Skipped() int64 { return b.c.skipped }

// Reset returns to level 0 before any reading and zeroes every count, keeping
// the edges and the dwell.
func (b *Band) Reset() { *b = Band{edges: b.edges, dwell: b.dwell} }
