package tree

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"testing"
)

// model is the brute-force reference: a plain parent array over keys
// 0..len−1, absent = −2, a root its own parent, and own values. Everything a
// Tree maintains incrementally, the model recomputes from scratch.
type model struct {
	parent []int
	own    []float64
}

func newModel(keys int) *model {
	m := &model{parent: make([]int, keys), own: make([]float64, keys)}
	for i := range m.parent {
		m.parent[i] = -2
	}
	return m
}

func (m *model) has(k int) bool { return k >= 0 && k < len(m.parent) && m.parent[k] != -2 }

// under reports whether u is in the subtree of x (x included), by walking up
// the model's parents.
func (m *model) under(u, x int) bool {
	for {
		if u == x {
			return true
		}
		if m.parent[u] == u {
			return false
		}
		u = m.parent[u]
	}
}

// truth is everything recomputed from a model. Children are kept as one
// flat list with offsets (kids of u are flat[off[u]:off[u+1]]), which keeps
// the recomputation cheap enough to run after every step.
type truth struct {
	off, flat []int
	roots     []int
	sum       []float64
	max       []float64
	at        []int
	depth     []int
	n         int
}

func (tr *truth) kids(u int) []int { return tr.flat[tr.off[u]:tr.off[u+1]] }

func (m *model) truth() *truth {
	n := len(m.parent)
	tr := &truth{off: make([]int, n+1), sum: make([]float64, n), max: make([]float64, n), at: make([]int, n), depth: make([]int, n)}
	for k, p := range m.parent {
		switch {
		case p == -2:
		case p == k:
			tr.roots = append(tr.roots, k)
		default:
			tr.off[p+1]++
		}
	}
	for u := range n {
		tr.off[u+1] += tr.off[u]
	}
	tr.flat = make([]int, tr.off[n])
	fill := slices.Clone(tr.off[:n])
	for k, p := range m.parent { // ascending k: every child list comes out sorted
		if p >= 0 && p != k {
			tr.flat[fill[p]] = k
			fill[p]++
		}
	}
	// Preorder from the roots, then fold up in reverse.
	order := make([]int, 0, n)
	stack := make([]int, 0, n)
	for _, r := range tr.roots {
		stack = append(stack[:0], r)
		for len(stack) > 0 {
			u := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			order = append(order, u)
			for _, c := range tr.kids(u) {
				tr.depth[c] = tr.depth[u] + 1
				stack = append(stack, c)
			}
		}
	}
	tr.n = len(order)
	for j := len(order) - 1; j >= 0; j-- {
		u := order[j]
		tr.sum[u], tr.max[u], tr.at[u] = m.own[u], m.own[u], u
		for _, c := range tr.kids(u) {
			tr.sum[u] += tr.sum[c]
			if tr.max[c] > tr.max[u] || tr.max[c] == tr.max[u] && tr.at[c] < tr.at[u] {
				tr.max[u], tr.at[u] = tr.max[c], tr.at[c]
			}
		}
	}
	return tr
}

// subtree lists x's descendants, x excluded.
func (tr *truth) subtree(x int) []int {
	var out []int
	stack := slices.Clone(tr.kids(x))
	for len(stack) > 0 {
		u := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, u)
		stack = append(stack, tr.kids(u)...)
	}
	return out
}

// topK is TopK by brute force: sort the whole subtree.
func (m *model) topK(tr *truth, x, k int, by Metric) []Entry[int] {
	var all []Entry[int]
	for _, u := range tr.subtree(x) {
		v := m.own[u]
		if by == BySum {
			v = tr.sum[u]
		}
		all = append(all, Entry[int]{u, v})
	}
	slices.SortFunc(all, func(a, b Entry[int]) int {
		switch {
		case a.Value > b.Value:
			return -1
		case a.Value < b.Value:
			return 1
		}
		return a.ID - b.ID
	})
	return all[:min(k, len(all))]
}

// op is one step of a random or fuzzed sequence.
type op struct {
	kind int // opAdd … opRemoveSubtree
	a, b int
	v    float64
}

const (
	opAdd = iota
	opMove
	opRemove
	opSet
	opRemoveSubtree
	nOps
)

// step applies o to the tree and the model and checks that the tree's error
// is exactly the one the model predicts.
func step(t testing.TB, tr *Tree[int], m *model, o op) {
	t.Helper()
	var err, want error
	switch o.kind {
	case opAdd:
		err = tr.Add(o.a, o.b)
		switch {
		case m.has(o.a):
			want = ErrExists
		case o.a != o.b && !m.has(o.b):
			want = ErrUnknown
		default:
			m.parent[o.a] = o.b
			m.own[o.a] = 0
		}
	case opMove:
		err = tr.Move(o.a, o.b)
		switch {
		case !m.has(o.a), o.a != o.b && !m.has(o.b):
			want = ErrUnknown
		case o.a != o.b && m.under(o.b, o.a):
			want = ErrCycle // must always be refused
		default:
			m.parent[o.a] = o.b
		}
	case opRemove:
		err = tr.Remove(o.a)
		switch {
		case !m.has(o.a):
			want = ErrUnknown
		case m.hasKids(o.a):
			want = ErrHasChildren
		default:
			m.parent[o.a] = -2
		}
	case opRemoveSubtree:
		n, e := tr.RemoveSubtree(o.a)
		err = e
		if !m.has(o.a) {
			want = ErrUnknown
			if n != 0 {
				t.Fatalf("RemoveSubtree(%d) of an unknown node removed %d", o.a, n)
			}
			break
		}
		doomed := append(m.truth().subtree(o.a), o.a)
		if n != len(doomed) {
			t.Fatalf("RemoveSubtree(%d) removed %d, want %d", o.a, n, len(doomed))
		}
		for _, u := range doomed {
			m.parent[u] = -2
		}
	case opSet:
		err = tr.Set(o.a, o.v)
		switch {
		case !m.has(o.a):
			want = ErrUnknown
		case math.IsNaN(o.v) || math.Abs(o.v) > maxValue:
			want = ErrInvalid
		default:
			m.own[o.a] = o.v + 0
		}
	}
	if want == nil && err != nil || want != nil && !errors.Is(err, want) {
		t.Fatalf("op %+v: error %v, want %v", o, err, want)
	}
	if err != nil && !errors.Is(err, ErrInvalid) {
		t.Fatalf("op %+v: error %v does not wrap ErrInvalid", o, err)
	}
}

func (m *model) hasKids(x int) bool {
	for k, p := range m.parent {
		if p == x && k != x {
			return true
		}
	}
	return false
}

// check compares the tree's answers with the model's recomputation: Parent,
// Sum, Own and Max for every node, and TopK for the nodes in topkOf (all
// inner nodes when nil and the tree is small). It checks the internal
// invariants (verify), which include that the tree is a forest. With full,
// it also checks Depth, Children and Path of every node through the API:
// every Path ends at a root, has one more key than the depth, and follows
// the model's parent links. Without, only on every 16th key, which keeps a
// tree of thousands of nodes checkable after every step.
func check(t testing.TB, tr *Tree[int], m *model, topkOf []int, ks []int, full bool) {
	t.Helper()
	verify(t, tr)
	truth := m.truth()
	if tr.Len() != truth.n {
		t.Fatalf("Len %d, model has %d nodes", tr.Len(), truth.n)
	}
	var buf []int
	off := len(m.parent) % 16
	for k := range m.parent {
		p, ok := tr.Parent(k)
		if !m.has(k) {
			if ok {
				t.Fatalf("Parent(%d) ok for an absent node", k)
			}
			continue
		}
		if !ok || p != m.parent[k] {
			t.Fatalf("Parent(%d) = %d %v, want %d", k, p, ok, m.parent[k])
		}
		if v, at, ok := tr.Max(k); !ok || v != truth.max[k] || at != truth.at[k] {
			t.Fatalf("Max(%d) = %v at %d %v, want %v at %d", k, v, at, ok, truth.max[k], truth.at[k])
		}
		if s := tr.Sum(k); s != truth.sum[k] {
			t.Fatalf("Sum(%d) = %v, want %v", k, s, truth.sum[k])
		}
		if o := tr.Own(k); o != m.own[k] || o == 0 && math.Signbit(o) {
			t.Fatalf("Own(%d) = %v, want %v", k, o, m.own[k])
		}
		if !full && k%16 != off {
			continue
		}
		if d, ok := tr.Depth(k); !ok || d != truth.depth[k] {
			t.Fatalf("Depth(%d) = %d %v, want %d", k, d, ok, truth.depth[k])
		}
		if buf = tr.Children(k, buf[:0]); !slices.Equal(buf, truth.kids(k)) {
			t.Fatalf("Children(%d) = %v, want %v", k, buf, truth.kids(k))
		}
		buf = tr.Path(k, buf[:0])
		if len(buf) != truth.depth[k]+1 || buf[len(buf)-1] != k || m.parent[buf[0]] != buf[0] {
			t.Fatalf("Path(%d) = %v: not a path from a root at depth %d", k, buf, truth.depth[k])
		}
		for j := 1; j < len(buf); j++ {
			if m.parent[buf[j]] != buf[j-1] {
				t.Fatalf("Path(%d) = %v: %d is not the parent of %d", k, buf, buf[j-1], buf[j])
			}
		}
	}
	if topkOf == nil && truth.n <= 64 {
		topkOf = truth.roots
		for k := range m.parent {
			if m.has(k) && len(truth.kids(k)) > 0 && m.parent[k] != k {
				topkOf = append(topkOf, k)
			}
		}
	}
	var got []Entry[int]
	for _, x := range topkOf {
		if !m.has(x) {
			continue
		}
		for _, k := range ks {
			for _, by := range []Metric{ByOwn, BySum} {
				got = tr.TopK(x, k, by, got[:0])
				if want := m.topK(truth, x, k, by); !slices.Equal(got, want) {
					t.Fatalf("TopK(%d, %d, %v) = %v, want %v", x, k, by, got, want)
				}
			}
		}
	}
}

// verify checks the tree's internal invariants directly, in O(n): every live
// slot is indexed under its key; every child list is in key order and its
// pos fields match; every heap holds the same children, in heap order, with
// matching hpos; every node's maximum is the larger of its own value and its
// heap's top; and a walk from the roots reaches every live node exactly once,
// which is what makes it a forest: no cycles, nothing cut off.
func verify[K cmp.Ordered](t testing.TB, tr *Tree[K]) {
	t.Helper()
	live := 0
	for i := range tr.nodes {
		n := &tr.nodes[i]
		if n.parent == freeSlot {
			continue
		}
		live++
		if j, ok := tr.index[n.id]; !ok || j != i {
			t.Fatalf("slot %d (%v) not indexed", i, n.id)
		}
		if len(n.heap) != len(n.kids) {
			t.Fatalf("%v: %d kids, %d in heap", n.id, len(n.kids), len(n.heap))
		}
		for j, k := range n.kids {
			c := &tr.nodes[k]
			if c.parent != i || c.pos != j || j > 0 && !(tr.nodes[n.kids[j-1]].id < c.id) {
				t.Fatalf("%v: child list broken at %d", n.id, j)
			}
			if n.heap[c.hpos] != k {
				t.Fatalf("%v: hpos of %v wrong", n.id, c.id)
			}
		}
		for h := 1; h < len(n.heap); h++ {
			if tr.above(n.heap[h], n.heap[(h-1)/2]) {
				t.Fatalf("%v: heap order broken at %d", n.id, h)
			}
		}
		v, at := n.own, i
		if len(n.heap) > 0 && tr.ranks(tr.nodes[n.heap[0]].mx, tr.nodes[n.heap[0]].mxAt, v, at) {
			v, at = tr.nodes[n.heap[0]].mx, tr.nodes[n.heap[0]].mxAt
		}
		if n.mx != v || n.mxAt != at {
			t.Fatalf("%v: max %v at %d, want %v at %d", n.id, n.mx, n.mxAt, v, at)
		}
	}
	if live != len(tr.index) {
		t.Fatalf("%d live slots, %d indexed", live, len(tr.index))
	}
	reached := 0
	for r := range tr.nodes {
		if tr.nodes[r].parent != noParent {
			continue
		}
		for u := r; u >= 0; u = tr.next(u, r) {
			if reached++; reached > live {
				t.Fatal("walk from the roots does not end: a cycle")
			}
		}
	}
	if reached != live {
		t.Fatalf("%d of %d nodes reachable from a root", reached, live)
	}
}
