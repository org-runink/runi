package tree

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// randomOp draws one operation over keys 0..keys−1. Values are small
// integers, so every sum is exact in float64 and the comparison with the
// model can be exact too; rounding is the business of the numerical tests.
// The value range is narrow on purpose: equal values, and so ties in Max and
// TopK, are common. Half of all moves pick the new parent inside the node's
// own subtree, which must always be refused.
//
// grow shifts the mix towards adding and away from removing, so a large key
// space fills up instead of hovering at a few hundred nodes. One draw in ten
// names a random key, present or not, so the error paths keep running.
func randomOp(r *rand.Rand, m *model, keys int, grow bool) op {
	var live []int
	for k := range keys {
		if m.has(k) {
			live = append(live, k)
		}
	}
	present := func() int {
		if len(live) == 0 || r.IntN(10) == 0 {
			return r.IntN(keys)
		}
		return live[r.IntN(len(live))]
	}
	// Shares, in percent, of add, set, move and remove; the rest is
	// RemoveSubtree.
	w := [4]int{30, 35, 17, 16}
	if grow {
		w = [4]int{60, 20, 15, 4}
	}
	switch x := r.IntN(100); {
	case x < w[0]:
		a := r.IntN(keys)
		for try := 0; grow && m.has(a) && try < 8; try++ {
			a = r.IntN(keys) // growing: prefer a key not yet used
		}
		if r.IntN(20) == 0 {
			return op{kind: opAdd, a: a, b: a} // a new root
		}
		return op{kind: opAdd, a: a, b: present()}
	case x < w[0]+w[1]:
		return op{kind: opSet, a: present(), v: float64(r.IntN(41) - 20)}
	case x < w[0]+w[1]+w[2]:
		a := present()
		if m.has(a) && r.IntN(2) == 0 {
			if sub := m.truth().subtree(a); len(sub) > 0 {
				return op{kind: opMove, a: a, b: sub[r.IntN(len(sub))]}
			}
		}
		if r.IntN(20) == 0 {
			return op{kind: opMove, a: a, b: a} // make it a root
		}
		return op{kind: opMove, a: a, b: present()}
	case x < w[0]+w[1]+w[2]+w[3]:
		return op{kind: opRemove, a: present()}
	default:
		return op{kind: opRemoveSubtree, a: present()}
	}
}

// After every step of random sequences, every answer the tree gives agrees
// with a recomputation from a plain parent map, the tree is a forest, and a
// move under a descendant has been refused (step checks the error of every
// operation against the model's prediction). Small key spaces churn the
// structure hard; the large one grows to a couple of thousand nodes.
func TestProperty(t *testing.T) {
	for _, c := range []struct {
		seed        uint64
		keys, steps int
	}{
		{1, 16, 20000},
		{2, 200, 10000},
		{3, 3000, 6000},
	} {
		c.steps /= raceScale
		r := rand.New(rand.NewPCG(c.seed, 97))
		tr, m := New[int](), newModel(c.keys)
		var peak, cycles int
		for i := range c.steps {
			o := randomOp(r, m, c.keys, i < 2*c.steps/3)
			if o.kind == opMove && o.a != o.b && m.has(o.a) && m.has(o.b) && m.under(o.b, o.a) {
				cycles++
			}
			step(t, tr, m, o)
			var topkOf []int
			if k := r.IntN(c.keys); m.has(k) {
				topkOf = []int{k}
			}
			check(t, tr, m, topkOf, []int{1, 1 + r.IntN(20)}, c.keys <= 200 || i%50 == 0)
			peak = max(peak, tr.Len())
		}
		if cycles < c.steps/50 {
			t.Errorf("seed %d: only %d moves under a descendant were tried", c.seed, cycles)
		}
		t.Logf("seed %d: %d keys, peak %d nodes, %d refused cycles", c.seed, c.keys, peak, cycles)
	}
}

// recorder stands in for *testing.T to see whether a check fails, without
// failing the test that runs it.
type recorder struct {
	testing.TB
	failed string
}

func (r *recorder) Helper()                   {}
func (r *recorder) Fatal(args ...any)         { r.failed = fmt.Sprint(args...); panic(r) }
func (r *recorder) Fatalf(f string, a ...any) { r.failed = fmt.Sprintf(f, a...); panic(r) }

func caught(t *testing.T, f func(tb testing.TB)) (msg string) {
	r := &recorder{TB: t}
	defer func() {
		if x := recover(); x != nil && x != r {
			panic(x)
		}
		msg = r.failed
	}()
	f(r)
	return ""
}

// The property and fuzz tests are only as good as check: a tree corrupted in
// any of the ways below must make it fail. Each corruption is made on a
// fresh, valid tree that check passes first.
func TestCheckCanFail(t *testing.T) {
	build := func() (*Tree[int], *model) {
		r := rand.New(rand.NewPCG(41, 42))
		tr, m := New[int](), newModel(64)
		for range 400 {
			step(t, tr, m, randomOp(r, m, 64, true))
		}
		if msg := caught(t, func(tb testing.TB) { check(tb, tr, m, nil, []int{3}, true) }); msg != "" {
			t.Fatalf("a valid tree fails the check: %s", msg)
		}
		return tr, m
	}
	inner := func(tr *Tree[int]) *node[int] {
		for i := range tr.nodes {
			if n := &tr.nodes[i]; n.parent >= 0 && len(n.kids) >= 2 {
				return n
			}
		}
		t.Fatal("no inner node with two children")
		return nil
	}
	for name, corrupt := range map[string]func(*Tree[int], *model){
		"a sum off by one":     func(tr *Tree[int], _ *model) { inner(tr).sum++ },
		"compensation lost":    func(tr *Tree[int], _ *model) { inner(tr).comp += 0.5 },
		"an own value changed": func(tr *Tree[int], _ *model) { inner(tr).own += 1 },
		"a maximum at the wrong node": func(tr *Tree[int], _ *model) {
			n := inner(tr)
			n.mxAt = n.kids[0] + n.kids[1] - n.mxAt
		},
		"children out of order": func(tr *Tree[int], _ *model) {
			n := inner(tr)
			n.kids[0], n.kids[1] = n.kids[1], n.kids[0]
		},
		"heap out of order": func(tr *Tree[int], _ *model) {
			for i := range tr.nodes {
				n := &tr.nodes[i]
				if n.parent != freeSlot && len(n.heap) >= 2 && tr.above(n.heap[0], n.heap[1]) {
					n.heap[0], n.heap[1] = n.heap[1], n.heap[0]
					tr.nodes[n.heap[0]].hpos, tr.nodes[n.heap[1]].hpos = 0, 1
					return
				}
			}
		},
		"the model moved a node the tree did not": func(tr *Tree[int], m *model) {
			for k := range m.parent {
				if m.has(k) && m.parent[k] != k {
					m.parent[k] = k
					return
				}
			}
		},
		"a node cut off from its root": func(tr *Tree[int], _ *model) {
			n := inner(tr)
			c := n.kids[0]
			n.kids = n.kids[1:]
			for j, k := range n.kids {
				tr.nodes[k].pos = j
			}
			n.heap = slices.DeleteFunc(n.heap, func(k int) bool { return k == c })
			for h, k := range n.heap {
				tr.nodes[k].hpos = h
			}
		},
	} {
		tr, m := build()
		corrupt(tr, m)
		if msg := caught(t, func(tb testing.TB) { check(tb, tr, m, nil, []int{3}, true) }); msg == "" {
			t.Errorf("%s: check passed", name)
		} else {
			t.Logf("%s: %s", name, msg)
		}
	}
}
