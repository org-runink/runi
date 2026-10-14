package tree

import (
	"errors"
	"math"
	"slices"
	"testing"
)

// handDrawn builds this tree, own values in brackets:
//
//	          a [1]
//	        /   |   \
//	    b [5] c [2]  d [−1]
//	    /   \          |
//	e [3]  f [5]     g [4]
//	                   |
//	                 h [0.5]
//
// Subtree sums: e 3, f 5, b 5+3+5 = 13, c 2, h 0.5, g 4.5, d −1+4.5 = 3.5,
// a 1+13+2+3.5 = 19.5. Subtree maxima: a 5, a tie between b and f that the
// smaller key, b, wins; d 4 at g.
func handDrawn(t *testing.T) *Tree[string] {
	t.Helper()
	tr := New[string]()
	must(t, tr.AddRoot("a"))
	// Children added out of key order: Children must still come out sorted.
	for _, e := range [][2]string{{"d", "a"}, {"b", "a"}, {"c", "a"}, {"f", "b"}, {"e", "b"}, {"g", "d"}, {"h", "g"}} {
		must(t, tr.Add(e[0], e[1]))
	}
	for id, v := range map[string]float64{"a": 1, "b": 5, "c": 2, "d": -1, "e": 3, "f": 5, "g": 4, "h": 0.5} {
		must(t, tr.Set(id, v))
	}
	return tr
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantSums(t *testing.T, tr *Tree[string], want map[string]float64) {
	t.Helper()
	for id, w := range want {
		if got := tr.Sum(id); got != w {
			t.Errorf("Sum(%s) = %v, want %v", id, got, w)
		}
	}
}

func wantMax(t *testing.T, tr *Tree[string], id string, v float64, at string) {
	t.Helper()
	if gv, gat, ok := tr.Max(id); !ok || gv != v || gat != at {
		t.Errorf("Max(%s) = %v at %s %v, want %v at %s", id, gv, gat, ok, v, at)
	}
}

func TestKnownAnswers(t *testing.T) {
	tr := handDrawn(t)
	if tr.Len() != 8 {
		t.Fatalf("Len %d", tr.Len())
	}
	wantSums(t, tr, map[string]float64{"a": 19.5, "b": 13, "c": 2, "d": 3.5, "e": 3, "f": 5, "g": 4.5, "h": 0.5})
	wantMax(t, tr, "a", 5, "b")
	wantMax(t, tr, "b", 5, "b")
	wantMax(t, tr, "d", 4, "g")
	wantMax(t, tr, "h", 0.5, "h")
	if got := tr.Children("a", nil); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Errorf("Children(a) = %v", got)
	}
	if got := tr.Path("h", []string{"x"}); !slices.Equal(got, []string{"x", "a", "d", "g", "h"}) {
		t.Errorf("Path(h) appended to [x] = %v", got)
	}
	if d, ok := tr.Depth("h"); d != 3 || !ok {
		t.Errorf("Depth(h) = %d %v", d, ok)
	}
	if p, ok := tr.Parent("a"); p != "a" || !ok {
		t.Errorf("Parent of the root = %q %v, want itself", p, ok)
	}
	if p, ok := tr.Parent("h"); p != "g" || !ok {
		t.Errorf("Parent(h) = %q %v", p, ok)
	}
	if got := tr.TopK("a", 3, ByOwn, nil); !slices.Equal(got, []Entry[string]{{"b", 5}, {"f", 5}, {"g", 4}}) {
		t.Errorf("TopK(a, 3, ByOwn) = %v", got)
	}
	if got := tr.TopK("a", 3, BySum, nil); !slices.Equal(got, []Entry[string]{{"b", 13}, {"f", 5}, {"g", 4.5}}) {
		t.Errorf("TopK(a, 3, BySum) = %v", got)
	}
	// k beyond the subtree: all seven descendants, a itself excluded.
	all := []Entry[string]{{"b", 13}, {"f", 5}, {"g", 4.5}, {"d", 3.5}, {"e", 3}, {"c", 2}, {"h", 0.5}}
	if got := tr.TopK("a", 10, BySum, nil); !slices.Equal(got, all) {
		t.Errorf("TopK(a, 10, BySum) = %v", got)
	}
	if got := tr.TopK("h", 3, BySum, nil); len(got) != 0 {
		t.Errorf("TopK of a leaf = %v, want nothing", got)
	}

	// Move g (with h) under c: c gains 4.5, d loses it, a is unchanged.
	must(t, tr.Move("g", "c"))
	wantSums(t, tr, map[string]float64{"a": 19.5, "c": 6.5, "d": -1, "g": 4.5})
	wantMax(t, tr, "d", -1, "d")
	wantMax(t, tr, "c", 4, "g")
	if got := tr.Path("h", nil); !slices.Equal(got, []string{"a", "c", "g", "h"}) {
		t.Errorf("Path(h) after the move = %v", got)
	}
	must(t, tr.Move("g", "c")) // to the parent it has: nothing happens
	wantSums(t, tr, map[string]float64{"a": 19.5, "c": 6.5})

	// Lowering the maximum: b drops to 0, f (5) takes over at b and at a.
	must(t, tr.Set("b", 0))
	wantMax(t, tr, "b", 5, "f")
	wantMax(t, tr, "a", 5, "f")
	must(t, tr.Set("f", 1))
	wantMax(t, tr, "a", 4, "g")
	wantSums(t, tr, map[string]float64{"a": 10.5, "b": 4})

	// d becomes a root of its own: a forest of two trees.
	must(t, tr.Move("d", "d"))
	if p, _ := tr.Parent("d"); p != "d" {
		t.Errorf("Parent(d) after Move(d, d) = %q", p)
	}
	wantSums(t, tr, map[string]float64{"a": 11.5, "d": -1})
	must(t, tr.Move("d", "d")) // already a root
	must(t, tr.Move("d", "h")) // and back in, deep
	wantSums(t, tr, map[string]float64{"a": 10.5, "g": 3.5, "h": -0.5})
	wantMax(t, tr, "h", 0.5, "h")

	// Remove leaves bottom-up; RemoveSubtree the rest of b.
	must(t, tr.Remove("d"))
	wantSums(t, tr, map[string]float64{"a": 11.5, "h": 0.5})
	if n, err := tr.RemoveSubtree("b"); n != 3 || err != nil {
		t.Fatalf("RemoveSubtree(b) = %d %v, want 3", n, err)
	}
	wantSums(t, tr, map[string]float64{"a": 7.5})
	if tr.Len() != 4 {
		t.Errorf("Len %d after removing 4 of 8", tr.Len())
	}
	if n, err := tr.RemoveSubtree("a"); n != 4 || err != nil || tr.Len() != 0 {
		t.Fatalf("RemoveSubtree(a) = %d %v, Len %d", n, err, tr.Len())
	}

	// Slots are reused, and a reused slot starts clean.
	must(t, tr.AddRoot("z"))
	must(t, tr.Add("y", "z"))
	wantSums(t, tr, map[string]float64{"z": 0, "y": 0})
	wantMax(t, tr, "z", 0, "y") // 0 and 0: the smaller key
	if got := tr.Children("z", nil); !slices.Equal(got, []string{"y"}) {
		t.Errorf("Children(z) in reused slots = %v", got)
	}
	if len(tr.nodes) != 8 {
		t.Errorf("%d slots after reuse, want the original 8", len(tr.nodes))
	}
}

// Remove of a root that is a leaf, and of a leaf whose parent stays.
func TestRemoveLeaves(t *testing.T) {
	var tr Tree[int] // the zero value is ready
	must(t, tr.AddRoot(1))
	must(t, tr.Add(2, 1))
	must(t, tr.Set(2, 7))
	must(t, tr.Set(1, -7))
	must(t, tr.Remove(2))
	if tr.Sum(1) != -7 || tr.Len() != 1 {
		t.Fatalf("Sum %v Len %d", tr.Sum(1), tr.Len())
	}
	if v, at, _ := tr.Max(1); v != -7 || at != 1 {
		t.Fatalf("Max %v at %d", v, at)
	}
	must(t, tr.Remove(1))
	if tr.Len() != 0 {
		t.Fatal("not empty")
	}
}

// −0 is stored as +0, so a maximum of zeros never depends on which zero.
func TestNegativeZero(t *testing.T) {
	tr := New[int]()
	must(t, tr.AddRoot(1))
	must(t, tr.Set(1, math.Copysign(0, -1)))
	if math.Signbit(tr.Own(1)) || math.Signbit(tr.Sum(1)) {
		t.Fatal("−0 kept")
	}
}

// Every error path, each matched with errors.Is against its sentinel and
// against ErrInvalid; a failed call changes nothing.
func TestErrors(t *testing.T) {
	tr := handDrawn(t)
	before := tr.Snapshot()
	sum := tr.Sum("a")
	_, removeUnknown := tr.RemoveSubtree("nope")
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"AddRoot existing", tr.AddRoot("a"), ErrExists},
		{"Add existing", tr.Add("b", "c"), ErrExists},
		{"Add under unknown", tr.Add("x", "nope"), ErrUnknown},
		{"Move unknown", tr.Move("nope", "a"), ErrUnknown},
		{"Move under unknown", tr.Move("b", "nope"), ErrUnknown},
		{"Move under a child", tr.Move("d", "g"), ErrCycle},
		{"Move under a deep descendant", tr.Move("a", "h"), ErrCycle},
		{"Remove unknown", tr.Remove("nope"), ErrUnknown},
		{"Remove inner", tr.Remove("b"), ErrHasChildren},
		{"RemoveSubtree unknown", removeUnknown, ErrUnknown},
		{"Set unknown", tr.Set("nope", 1), ErrUnknown},
		{"Set NaN", tr.Set("e", math.NaN()), ErrInvalid},
		{"Set +Inf", tr.Set("e", math.Inf(1)), ErrInvalid},
		{"Set −Inf", tr.Set("e", math.Inf(-1)), ErrInvalid},
		{"Set too large", tr.Set("e", 0x1p961), ErrInvalid},
		{"Set too small", tr.Set("e", -math.MaxFloat64), ErrInvalid},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) || !errors.Is(c.err, ErrInvalid) {
			t.Errorf("%s: %v, want %v wrapping ErrInvalid", c.name, c.err, c.want)
		}
	}
	// The specific sentinels are distinct from each other.
	for _, a := range []error{ErrUnknown, ErrExists, ErrCycle, ErrHasChildren} {
		for _, b := range []error{ErrUnknown, ErrExists, ErrCycle, ErrHasChildren} {
			if a != b && errors.Is(a, b) {
				t.Errorf("%v matches %v", a, b)
			}
		}
	}
	if d := Diff(before, tr.Snapshot()); len(d.Added)+len(d.Removed)+len(d.Moved) != 0 || tr.Sum("a") != sum {
		t.Fatalf("failed calls changed the tree: %+v, sum %v", d, tr.Sum("a"))
	}
	if tr.Own("e") != 3 {
		t.Fatalf("a refused Set changed the value: %v", tr.Own("e"))
	}
	if err := tr.Set("e", 0x1p960); err != nil {
		t.Fatalf("the largest allowed value refused: %v", err)
	}

	// A NaN key could never be found again, so it is refused.
	f := New[float64]()
	if err := f.AddRoot(math.NaN()); !errors.Is(err, ErrInvalid) || f.Len() != 0 {
		t.Fatalf("NaN key: %v, Len %d", err, f.Len())
	}
}

// Queries about nodes that are not there, and TopK arguments it cannot use,
// answer with nothing rather than failing.
func TestAbsent(t *testing.T) {
	tr := handDrawn(t)
	if !math.IsNaN(tr.Own("nope")) || !math.IsNaN(tr.Sum("nope")) {
		t.Error("Own or Sum of an unknown node is not NaN")
	}
	if v, at, ok := tr.Max("nope"); ok || !math.IsNaN(v) || at != "" {
		t.Errorf("Max of unknown = %v %q %v", v, at, ok)
	}
	if _, ok := tr.Parent("nope"); ok {
		t.Error("Parent of unknown ok")
	}
	if _, ok := tr.Depth("nope"); ok {
		t.Error("Depth of unknown ok")
	}
	dst := []string{"keep"}
	if got := tr.Children("nope", dst); !slices.Equal(got, dst) {
		t.Errorf("Children of unknown = %v", got)
	}
	if got := tr.Path("nope", dst); !slices.Equal(got, dst) {
		t.Errorf("Path of unknown = %v", got)
	}
	keep := []Entry[string]{{"keep", 1}}
	for _, c := range []struct {
		id string
		k  int
		by Metric
	}{{"nope", 3, ByOwn}, {"a", 0, ByOwn}, {"a", -1, BySum}, {"a", 3, Metric(2)}, {"a", 3, Metric(-1)}} {
		if got := tr.TopK(c.id, c.k, c.by, keep); !slices.Equal(got, keep) {
			t.Errorf("TopK(%q, %d, %d) = %v", c.id, c.k, c.by, got)
		}
	}
}

func TestRebuild(t *testing.T) {
	tr := handDrawn(t)
	must(t, tr.AddRoot("r2"))
	must(t, tr.Add("r2c", "r2"))
	must(t, tr.Set("r2c", 2))
	must(t, tr.Remove("e")) // leaves a free slot for Rebuild to skip
	tr.Rebuild()
	wantSums(t, tr, map[string]float64{"a": 16.5, "b": 10, "d": 3.5, "r2": 2, "r2c": 2})
	for i := range tr.nodes {
		if tr.nodes[i].parent != freeSlot && tr.nodes[i].comp != 0 {
			t.Errorf("compensation %v after Rebuild of exact values", tr.nodes[i].comp)
		}
	}
}
