package tablelog

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// A data file is named for the microsecond it was created, so ordering the live
// set by path only reproduces commit order while every commit lands in its own
// microsecond. The rest of the name is random, so the moment two files share a
// timestamp their order is a coin toss.
//
// That is not hypothetical. It is decided by the platform's clock granularity:
// Linux gives a distinct microsecond per commit here, Windows ticks every
// 0.5-15.6 ms, so several commits land inside one tick and come back shuffled.
// It surfaced as a Windows-only CI failure in this package, and only after the
// commit path got fast enough to fit several commits into a single tick --
// the speed-up did not cause the bug, it just stopped hiding it.
//
// A frozen clock reproduces on every platform what a coarse clock does by
// accident on one. These tests pin the ordering to the log's commit Version,
// which is the sequence actually meant, rather than to the wall clock.

// frozenClock returns the same instant forever, so every data file in a test
// carries an identical timestamp and the path can no longer order them.
func frozenClock() func() time.Time {
	at := time.Unix(1_700_000_000, 0).UTC()
	return func() time.Time { return at }
}

func TestMergeKeepsCommitOrderWhenEveryFileSharesATimestampCLK(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t, WithClock(frozenClock()))

	// Twenty commits in ascending key order, all inside one "tick".
	for i := 0; i < 20; i++ {
		rows := make([]Row, 5)
		for j := range rows {
			rows[j] = Row{Key: fmt.Sprintf("k%03d", i*5+j), Payload: []byte("v")}
		}
		if _, err := tb.Put(ctx, rows...); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	snap, err := tb.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	files := snap.s.sortedFiles()
	if len(files) != 20 {
		t.Fatalf("got %d files, want 20", len(files))
	}

	// The live set must come back in commit order even though every path
	// carries the same timestamp.
	if !slices.IsSortedFunc(files, func(a, b fileEntry) int { return int(a.Version - b.Version) }) {
		got := make([]int64, len(files))
		for i, f := range files {
			got[i] = f.Version
		}
		t.Errorf("files are not in commit order with a frozen clock: versions %v", got)
	}

	winners, err := tb.merge(ctx, files, func(string) bool { return true })
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(winners) != 100 {
		t.Fatalf("merge returned %d winners, want 100", len(winners))
	}
	if !slices.IsSortedFunc(winners, func(a, b row) int { return strings.Compare(a.key, b.key) }) {
		t.Error("a table written in ascending key order did not merge back in key order " +
			"when every file shared a timestamp")
	}
}

// Last-writer-wins does NOT depend on the file order -- Get resolves recency
// itself -- and this test passed both before and after the ordering fix. It is
// here as a guard rather than a regression test: the obvious way to "simplify"
// the sort above is to decide that nothing depends on it, and this pins the
// invariant that would break first if the winner ever started being read off
// the end of a path-ordered list.
func TestNewestRowWinsWhenTimestampsCollideCLK(t *testing.T) {
	ctx := context.Background()
	tb, _ := newTable(t, WithClock(frozenClock()))

	for i := 0; i < 10; i++ {
		row := Row{Key: "same", Payload: []byte(fmt.Sprintf("write-%02d", i))}
		if _, err := tb.Put(ctx, row); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	got, ok, err := tb.Get(ctx, "same")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatal("key written ten times is not present")
	}
	if want := "write-09"; string(got.Payload) != want {
		t.Errorf("Get returned %q, want %q -- the newest write lost to an older one "+
			"because their files share a timestamp", got.Payload, want)
	}
}

// One commit can add more than one file -- a compaction does. Within a single
// commit Version cannot separate them, so the path breaks the tie. That keeps
// the answer deterministic instead of leaving it to map iteration order, which
// would otherwise differ between two reads of the same immutable state.
func TestFilesAddedByOneCommitAreOrderedByPathCLK(t *testing.T) {
	s := emptyState()
	e := &logEntry{
		Version: 1,
		Parent:  0,
		Add: []fileEntry{
			{Path: "data/cccc", Rows: 1},
			{Path: "data/aaaa", Rows: 1},
			{Path: "data/bbbb", Rows: 1},
		},
	}
	if err := s.applyHere(e); err != nil {
		t.Fatalf("applyHere: %v", err)
	}
	got := make([]string, 0, 3)
	for _, f := range s.sortedFiles() {
		if f.Version != 1 {
			t.Fatalf("file %s has version %d, want 1", f.Path, f.Version)
		}
		got = append(got, f.Path)
	}
	want := []string{"data/aaaa", "data/bbbb", "data/cccc"}
	if !slices.Equal(got, want) {
		t.Errorf("files from one commit came back %v, want %v", got, want)
	}
}
