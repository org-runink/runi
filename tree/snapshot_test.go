package tree

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestDiffKnownAnswer(t *testing.T) {
	tr := handDrawn(t)
	a := tr.Snapshot()
	// From the hand-drawn tree: i joins under c, g (with h) moves to c, d
	// becomes a root of its own, e goes.
	must(t, tr.Add("i", "c"))
	must(t, tr.Move("g", "c"))
	must(t, tr.Move("d", "d"))
	must(t, tr.Remove("e"))
	b := tr.Snapshot()
	got := Diff(a, b)
	want := Changes[string]{
		Added:   []string{"i"},
		Removed: []string{"e"},
		Moved:   []Move[string]{{"d", "a", "d"}, {"g", "d", "c"}},
	}
	if !slices.Equal(got.Added, want.Added) || !slices.Equal(got.Removed, want.Removed) || !slices.Equal(got.Moved, want.Moved) {
		t.Fatalf("Diff = %+v\nwant   %+v", got, want)
	}
	// Backwards, every change reverses.
	back := Diff(b, a)
	if !slices.Equal(back.Added, want.Removed) || !slices.Equal(back.Removed, want.Added) ||
		!slices.Equal(back.Moved, []Move[string]{{"d", "d", "a"}, {"g", "c", "d"}}) {
		t.Fatalf("Diff backwards = %+v", back)
	}
	if d := Diff(a, a); d.Added != nil || d.Removed != nil || d.Moved != nil {
		t.Fatalf("Diff of a snapshot with itself = %+v", d)
	}
	if d := Diff(Snapshot[string]{}, Snapshot[string]{}); d.Added != nil || d.Removed != nil || d.Moved != nil {
		t.Fatalf("Diff of empty snapshots = %+v", d)
	}
	if d := Diff(Snapshot[string]{}, a); !slices.Equal(d.Added, []string{"a", "b", "c", "d", "e", "f", "g", "h"}) {
		t.Fatalf("Diff from empty = %+v", d)
	}

	if a.Len() != 8 || b.Len() != 8 {
		t.Errorf("Len %d %d", a.Len(), b.Len())
	}
	for _, c := range []struct {
		s      Snapshot[string]
		id, p  string
		wantOK bool
	}{{a, "a", "a", true}, {a, "g", "d", true}, {b, "g", "c", true}, {b, "d", "d", true}, {b, "e", "", false}, {a, "zz", "", false}} {
		if p, ok := c.s.Parent(c.id); p != c.p || ok != c.wantOK {
			t.Errorf("Parent(%s) = %q %v, want %q %v", c.id, p, ok, c.p, c.wantOK)
		}
	}
	var ids []string
	for id, p := range b.All() {
		ids = append(ids, id+"<"+p)
		if id == "f" {
			break // stopping early must stop the iteration
		}
	}
	if !slices.Equal(ids, []string{"a<a", "b<a", "c<a", "d<d", "f<b"}) {
		t.Errorf("All = %v", ids)
	}

	// The snapshot is a copy: changing the tree does not change it.
	must(t, tr.Move("i", "a"))
	if p, _ := b.Parent("i"); p != "c" {
		t.Errorf("snapshot changed with the tree: Parent(i) = %q", p)
	}
}

// apply carries out the procedure Changes documents, failing on any refusal.
func apply[K interface{ ~int | ~string }](t testing.TB, tr *Tree[K], c Changes[K], b Snapshot[K]) {
	t.Helper()
	for _, id := range c.Added {
		must(t, tr.AddRoot(id))
	}
	for _, m := range c.Moved {
		must(t, tr.Move(m.ID, m.ID))
	}
	for _, id := range c.Added {
		p, _ := b.Parent(id)
		must(t, tr.Move(id, p))
	}
	for _, m := range c.Moved {
		must(t, tr.Move(m.ID, m.To))
	}
	for _, id := range c.Removed {
		if _, ok := tr.Parent(id); ok {
			if _, err := tr.RemoveSubtree(id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// fromSnapshot builds a tree with s's structure, every node first a root and
// then moved under its parent: that order cannot be refused.
func fromSnapshot(t testing.TB, s Snapshot[int]) *Tree[int] {
	tr := New[int]()
	for id := range s.All() {
		must(t, tr.AddRoot(id))
	}
	for id, p := range s.All() {
		must(t, tr.Move(id, p))
	}
	return tr
}

// Applying Diff(a, b) to a tree shaped like a, by the documented procedure,
// is never refused and gives exactly b; and Diff is deterministic (the same
// pair gives the same Changes however the trees were built).
func TestDiffReconstructs(t *testing.T) {
	r := rand.New(rand.NewPCG(31, 32))
	// The cases the documented order exists for must actually occur: a node
	// becoming a root, a root moving under another node, and a node moving
	// out from under a parent that was removed.
	var toRoot, fromRoot, outOfRemoved int
	for round := range 200 / raceScale {
		keys := 8 + r.IntN(300)
		tr, m := New[int](), newModel(keys)
		for range keys * 2 {
			step(t, tr, m, randomOp(r, m, keys, true))
		}
		a := tr.Snapshot()
		for range r.IntN(keys) {
			step(t, tr, m, randomOp(r, m, keys, false))
		}
		b := tr.Snapshot()
		c := Diff(a, b)
		for _, l := range [][]int{c.Added, c.Removed} {
			if !slices.IsSorted(l) {
				t.Fatalf("round %d: not sorted: %v", round, l)
			}
		}
		if !slices.IsSortedFunc(c.Moved, func(x, y Move[int]) int { return x.ID - y.ID }) {
			t.Fatalf("round %d: Moved not sorted", round)
		}
		for _, mv := range c.Moved {
			if mv.To == mv.ID {
				toRoot++
			}
			if mv.From == mv.ID {
				fromRoot++
			}
			if _, ok := slices.BinarySearch(c.Removed, mv.From); ok {
				outOfRemoved++
			}
		}
		re := fromSnapshot(t, a)
		if !slices.Equal(re.Snapshot().edges, a.edges) {
			t.Fatalf("round %d: fromSnapshot did not rebuild a", round)
		}
		apply(t, re, c, b)
		if got := re.Snapshot(); !slices.Equal(got.edges, b.edges) {
			t.Fatalf("round %d: a + Diff(a, b) is not b:\n%v\n%v", round, got.edges, b.edges)
		}
		c2 := Diff(fromSnapshot(t, a).Snapshot(), re.Snapshot())
		if !slices.Equal(c.Added, c2.Added) || !slices.Equal(c.Removed, c2.Removed) || !slices.Equal(c.Moved, c2.Moved) {
			t.Fatalf("round %d: Diff not deterministic", round)
		}
	}
	if toRoot == 0 || fromRoot == 0 || outOfRemoved == 0 {
		t.Fatalf("cases not exercised: %d to a root, %d from a root, %d out of a removed parent", toRoot, fromRoot, outOfRemoved)
	}
	t.Logf("%d to a root, %d from a root, %d out of a removed parent", toRoot, fromRoot, outOfRemoved)
}
