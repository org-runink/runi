package tablelog

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"
)

// A log-structured table is a state machine: a sequence of commits and the
// view they add up to. Its properties are about every sequence, not the three
// an example writes down -- and the ones that matter are that replaying the
// log gives the same view it gave before, and that a reader at an old version
// still sees exactly what was there then.

// A model of the table kept alongside it: the last payload written for each
// key, with deleted keys absent.
type model map[string]string

func applyOp(m model, key, payload string, del bool) {
	if del {
		delete(m, key)
		return
	}
	m[key] = payload
}

// Whatever sequence of puts and deletes is committed, reading the table back
// gives exactly the rows a direct model of those operations holds.
func TestPropertyTheTableMatchesAModelOfItsCommits(t *testing.T) {
	ctx := context.Background()
	r := rand.New(rand.NewPCG(181, 182))
	for i := 0; i < 200; i++ {
		tb, _ := newTable(t)
		want := model{}
		for op := 0; op < 1+r.IntN(30); op++ {
			// A batch per commit, because a commit is the unit of atomicity
			// and a one-row batch would never exercise it.
			batch := 1 + r.IntN(4)
			if r.IntN(4) == 0 {
				keys := make([]string, batch)
				for j := range keys {
					keys[j] = fmt.Sprintf("k%d", r.IntN(12))
					applyOp(want, keys[j], "", true)
				}
				if _, err := tb.Delete(ctx, keys...); err != nil {
					t.Fatal(err)
				}
				continue
			}
			rows := make([]Row, batch)
			for j := range rows {
				k := fmt.Sprintf("k%d", r.IntN(12))
				v := fmt.Sprintf("v%d", r.IntN(1000))
				rows[j] = Row{Key: k, Payload: []byte(v)}
				applyOp(want, k, v, false)
			}
			if _, err := tb.Put(ctx, rows...); err != nil {
				t.Fatal(err)
			}
		}

		got, err := tb.Scan(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("table holds %d rows, the model holds %d", len(got), len(want))
		}
		for _, rec := range got {
			if string(rec.Payload) != want[rec.Key] {
				t.Fatalf("key %q is %q, model says %q", rec.Key, rec.Payload, want[rec.Key])
			}
		}
		// Get must agree with Scan, key by key, including on absence.
		for k, v := range want {
			rec, ok, err := tb.Get(ctx, k)
			if err != nil || !ok || string(rec.Payload) != v {
				t.Fatalf("Get(%q) = %q, %v, %v; want %q", k, rec.Payload, ok, err, v)
			}
		}
		if _, ok, err := tb.Get(ctx, "absent"); err != nil || ok {
			t.Fatalf("Get on an unwritten key reported %v, %v", ok, err)
		}
	}
}

// A snapshot at an old version is immutable: later commits cannot change what
// it shows. This is the whole point of keeping a log, and the one guarantee a
// consumer building a derived view depends on.
func TestPropertyOldSnapshotsNeverChange(t *testing.T) {
	ctx := context.Background()
	r := rand.New(rand.NewPCG(183, 184))
	for i := 0; i < 200; i++ {
		tb, _ := newTable(t)
		type checkpoint struct {
			version int64
			rows    map[string]string
		}
		var marks []checkpoint
		want := model{}

		for op := 0; op < 1+r.IntN(20); op++ {
			k := fmt.Sprintf("k%d", r.IntN(8))
			if r.IntN(4) == 0 {
				if _, err := tb.Delete(ctx, k); err != nil {
					t.Fatal(err)
				}
				applyOp(want, k, "", true)
			} else {
				v := fmt.Sprintf("v%d", r.IntN(1000))
				if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(v)}); err != nil {
					t.Fatal(err)
				}
				applyOp(want, k, v, false)
			}
			v, err := tb.Version(ctx)
			if err != nil {
				t.Fatal(err)
			}
			snap := map[string]string{}
			for k, v := range want {
				snap[k] = v
			}
			marks = append(marks, checkpoint{version: v, rows: snap})
		}

		// Every mark must still read back as it was, now that later commits
		// have landed on top of it.
		for _, mark := range marks {
			s, err := tb.Snapshot(ctx, mark.version)
			if err != nil {
				t.Fatalf("snapshot at %d: %v", mark.version, err)
			}
			got, err := s.Scan(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(mark.rows) {
				t.Fatalf("snapshot %d holds %d rows, held %d when taken",
					mark.version, len(got), len(mark.rows))
			}
			for _, rec := range got {
				if string(rec.Payload) != mark.rows[rec.Key] {
					t.Fatalf("snapshot %d: key %q is %q, was %q",
						mark.version, rec.Key, rec.Payload, mark.rows[rec.Key])
				}
			}
		}
	}
}

// Compaction rewrites the files under a table and must not change what it
// shows -- not one row, at any version a consumer can still reach. A
// maintenance job that altered the data would be the worst kind of bug,
// because nothing in the application changed when it happened.
func TestPropertyCompactionPreservesEveryVisibleRow(t *testing.T) {
	ctx := context.Background()
	r := rand.New(rand.NewPCG(185, 186))
	for i := 0; i < 150; i++ {
		tb, _ := newTable(t, WithCompaction(1<<20, 0.0)) // compact eagerly
		want := model{}
		for op := 0; op < 1+r.IntN(25); op++ {
			k := fmt.Sprintf("k%d", r.IntN(10))
			if r.IntN(3) == 0 {
				if _, err := tb.Delete(ctx, k); err != nil {
					t.Fatal(err)
				}
				applyOp(want, k, "", true)
			} else {
				v := fmt.Sprintf("v%d", r.IntN(1000))
				if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(v)}); err != nil {
					t.Fatal(err)
				}
				applyOp(want, k, v, false)
			}
		}
		before, err := tb.Scan(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tb.Compact(ctx); err != nil {
			t.Fatalf("compact: %v", err)
		}
		after, err := tb.Scan(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		sortRecs(before)
		sortRecs(after)
		if len(before) != len(after) {
			t.Fatalf("compaction changed the row count: %d -> %d", len(before), len(after))
		}
		for j := range before {
			if before[j].Key != after[j].Key || string(before[j].Payload) != string(after[j].Payload) {
				t.Fatalf("compaction changed row %d: %+v -> %+v", j, before[j], after[j])
			}
		}
		for _, rec := range after {
			if string(rec.Payload) != want[rec.Key] {
				t.Fatalf("after compaction key %q is %q, model says %q", rec.Key, rec.Payload, want[rec.Key])
			}
		}
	}
}

func sortRecs(rs []Record) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].Key < rs[j].Key })
}

// A prefix scan returns exactly the rows with that prefix: no near-misses and
// nothing omitted. Prefix bugs are quiet, because the answer still looks like
// a plausible set of rows.
func TestPropertyPrefixScanIsExact(t *testing.T) {
	ctx := context.Background()
	r := rand.New(rand.NewPCG(187, 188))
	alphabet := []string{"a", "ab", "abc", "b", "ba", "", "a/b", "a/bc"}
	for i := 0; i < 300; i++ {
		tb, _ := newTable(t)
		want := model{}
		for op := 0; op < 1+r.IntN(20); op++ {
			k := alphabet[r.IntN(len(alphabet))] + fmt.Sprintf("%d", r.IntN(5))
			v := fmt.Sprintf("v%d", r.IntN(1000))
			if _, err := tb.Put(ctx, Row{Key: k, Payload: []byte(v)}); err != nil {
				t.Fatal(err)
			}
			want[k] = v
		}
		for _, prefix := range alphabet {
			got, err := tb.Scan(ctx, prefix)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, rec := range got {
				if len(rec.Key) < len(prefix) || rec.Key[:len(prefix)] != prefix {
					t.Fatalf("scan %q returned %q", prefix, rec.Key)
				}
				seen[rec.Key] = true
			}
			for k := range want {
				if len(k) >= len(prefix) && k[:len(prefix)] == prefix && !seen[k] {
					t.Fatalf("scan %q missed %q", prefix, k)
				}
			}
		}
	}
}

// Versions only ever go up, one commit at a time, and a write of no rows is
// refused rather than burning a version on nothing. A consumer that tracks
// its position by version relies on both.
func TestPropertyVersionsAdvanceMonotonically(t *testing.T) {
	ctx := context.Background()
	r := rand.New(rand.NewPCG(189, 190))
	for i := 0; i < 300; i++ {
		tb, _ := newTable(t)
		prev, err := tb.Version(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for op := 0; op < 1+r.IntN(20); op++ {
			empty := r.IntN(5) == 0
			var rows []Row
			if !empty {
				rows = []Row{{Key: fmt.Sprintf("k%d", r.IntN(6)), Payload: []byte("v")}}
			}
			_, err := tb.Put(ctx, rows...)
			if empty && err == nil {
				t.Fatal("a Put of no rows was accepted")
			}
			if !empty && err != nil {
				t.Fatal(err)
			}
			got, verr := tb.Version(ctx)
			if verr != nil {
				t.Fatal(verr)
			}
			switch {
			case empty && got != prev:
				t.Fatalf("a refused Put moved the version from %d to %d", prev, got)
			case !empty && got != prev+1:
				t.Fatalf("a Put moved the version from %d to %d", prev, got)
			}
			prev = got
		}
	}
}
