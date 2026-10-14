// SPDX-License-Identifier: BSD-3-Clause

package tree

import "cmp"

// Metric chooses the value TopK ranks nodes by.
type Metric int

const (
	// ByOwn ranks by a node's own value: "which single nodes are largest".
	ByOwn Metric = iota
	// BySum ranks by a node's subtree sum: "which subtrees account for most".
	BySum
)

// Entry is one result of TopK.
type Entry[K cmp.Ordered] struct {
	ID    K
	Value float64
}

// TopK appends to dst the k nodes of id's subtree with the largest value by
// metric, largest first, ties to the smaller key, and returns the extended
// slice. id itself is not a candidate: the question is which descendants
// account for most of id, and by BySum id would always come first with a
// value the caller already has from Sum. Fewer than k are appended when id
// has fewer descendants. It appends nothing when id is not in the tree, k ≤ 0,
// or metric is neither ByOwn nor BySum.
//
// Candidates come from every depth, so by BySum a node and its own parent
// can both appear: a child that accounts for most of its parent ranks just
// below it. That is the honest answer to "which subtrees are heaviest"; to
// rank only id's direct children, use Children and Sum.
//
// It walks the whole subtree, O(size · log k), keeping the best k so far in
// a bounded min-heap held in dst's own storage beyond its current length,
// then sorts that in place (heapsort). It does not allocate when dst has
// room for k more entries.
func (t *Tree[K]) TopK(id K, k int, metric Metric, dst []Entry[K]) []Entry[K] {
	r, ok := t.index[id]
	if !ok || k <= 0 || (metric != ByOwn && metric != BySum) {
		return dst
	}
	base := len(dst)
	for u := t.next(r, r); u >= 0; u = t.next(u, r) {
		n := &t.nodes[u]
		e := Entry[K]{n.id, n.own}
		if metric == BySum {
			e.Value = n.sum + n.comp
		}
		h := dst[base:]
		switch {
		case len(h) < k:
			dst = append(dst, e)
			entryUp(dst[base:], len(h))
		case worse(h[0], e):
			h[0] = e
			entryDown(h, 0)
		}
	}
	// Heapsort: move the worst to the end, repeatedly; the best ends first.
	h := dst[base:]
	for end := len(h) - 1; end > 0; end-- {
		h[0], h[end] = h[end], h[0]
		entryDown(h[:end], 0)
	}
	return dst
}

// worse orders entries for the min-heap: smaller value, then larger key.
func worse[K cmp.Ordered](a, b Entry[K]) bool {
	if a.Value != b.Value {
		return a.Value < b.Value
	}
	return a.ID > b.ID
}

func entryUp[K cmp.Ordered](h []Entry[K], i int) {
	for i > 0 {
		up := (i - 1) / 2
		if !worse(h[i], h[up]) {
			return
		}
		h[i], h[up] = h[up], h[i]
		i = up
	}
}

func entryDown[K cmp.Ordered](h []Entry[K], i int) {
	for {
		c := 2*i + 1
		if c >= len(h) {
			return
		}
		if c+1 < len(h) && worse(h[c+1], h[c]) {
			c++
		}
		if !worse(h[c], h[i]) {
			return
		}
		h[i], h[c] = h[c], h[i]
		i = c
	}
}
