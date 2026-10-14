// SPDX-License-Identifier: BSD-3-Clause

package tree

import (
	"fmt"
	"math"
)

// Set sets id's own value to v and updates the subtree sum and maximum of
// every ancestor. It does not allocate.
//
// Sums are updated incrementally, not recomputed: v is added to, and the old
// value subtracted from, the sum of id and of each ancestor, O(depth). Done
// naively that loses precision for good: after a node goes from 0 to 1e16
// and back, a sibling's 1 has been rounded away from every ancestor's sum
// (1e16 + 1 is 1e16 in float64) and nothing ever brings it back. So each sum
// is kept as a pair, a running sum and a running compensation that collects
// exactly the low-order part each addition rounded off, which is Neumaier's
// improvement of Kahan's compensated summation (A. Neumaier, "Rundungsfehler-
// analyse einiger Verfahren zur Summation endlicher Summen", ZAMM 54, 1974;
// see N. J. Higham, Accuracy and Stability of Numerical Algorithms, 2nd ed.,
// SIAM 2002, §4.3). Sum returns sum + compensation.
//
// The error of a compensated sum of n terms x₁…xₙ with exact total S is at
// most u·|S| + γ²·Σ|xᵢ|, u = 2⁻⁵³ and γ = n·u/(1−n·u) (T. Ogita, S. M. Rump
// and S. Oishi, "Accurate sum and dot product", SIAM J. Sci. Comput. 26(6),
// 2005, Proposition 4.5, for the equivalent Sum2). Every Set below a node
// adds two terms to that node's sum. The first part is the rounding of the
// answer itself, the best any method can do. The second is the drift, and
// it grows with the cube of the number of updates below the node (the
// square from γ², once more from Σ|xᵢ|): for values about as large as the
// sum it is some tens of u·|S|, a relative 1e-14, after 10⁶ updates, and
// 1e-11 after 10⁷. That is a guarantee for the worst case, not the typical
// error. The test that holds the bound (10⁶ updates, values over fifteen
// orders of magnitude, random signs, up to 36 levels deep) also requires
// that no node's error exceeds u·|S|, which holds; a naive running sum on
// the same updates is off by 0.1 at the root and by more than 10¹⁵·u·|S| at
// a node whose sum nearly cancels. Rebuild recomputes every sum from the own
// values and drops the drift; a tree that must guarantee eleven digits
// through more than 10⁷ updates per node should call it that often.
//
// v must be finite and |v| ≤ 2⁹⁶⁰ (about 1e289), or Set fails with ErrInvalid
// and changes nothing. Unlike the streaming summaries elsewhere in this
// module, which skip a non-finite reading and carry on, a value here is
// state that every ancestor's sum shares: a NaN, once added, makes every sum
// above it NaN, and no later Set can subtract it out again (NaN − NaN is
// NaN); an infinity does the same through ∞ − ∞. Skipping silently would
// leave the previous value attributed as if it were current. Rejecting lets
// the caller decide what a missing reading means: often 0, sometimes the
// last good value. The bound keeps every sum, and every intermediate term,
// finite. Unknown id fails with ErrUnknown.
//
// The maximum is updated by walking up the same path, repairing each
// ancestor's heap of children (see Max): O(depth · log b) for branching b,
// and it stops as soon as an ancestor's maximum is unchanged, which for most
// updates is immediately.
func (t *Tree[K]) Set(id K, v float64) error {
	i, ok := t.index[id]
	if !ok {
		return unknown(id)
	}
	if !finite(v) || math.Abs(v) > maxValue {
		return fmt.Errorf("%w: value %v for %v, want finite and |v| ≤ 2^960", ErrInvalid, v, id)
	}
	v += 0 // −0 becomes +0, so equal values compare equal in every sense
	n := &t.nodes[i]
	old := n.own
	n.own = v
	t.addUp(i, v, -old)
	t.fixMax(i)
	return nil
}

// Own is id's own value, 0 if never set, NaN if id is not in the tree. O(1).
func (t *Tree[K]) Own(id K) float64 {
	i, ok := t.index[id]
	if !ok {
		return math.NaN()
	}
	return t.nodes[i].own
}

// Sum is the total of the own values in id's subtree, id included; NaN if
// id is not in the tree. O(1). See Set for its accuracy.
func (t *Tree[K]) Sum(id K) float64 {
	i, ok := t.index[id]
	if !ok {
		return math.NaN()
	}
	return t.nodes[i].sum + t.nodes[i].comp
}

// Max is the largest own value in id's subtree, id included, and the node
// that holds it; on a tie, the smaller key. ok is false, and v NaN, when id
// is not in the tree. O(1).
//
// Each node keeps its subtree maximum, and its children in a binary max-heap
// ordered by their subtree maxima (J. W. J. Williams, "Algorithm 232:
// Heapsort", Commun. ACM 7(6), 1964; Cormen et al., Introduction to
// Algorithms, ch. 6), so a node's maximum is the larger of its own value and
// the top of its heap. Rescanning the children instead would be simpler, and
// costs O(b) whenever the maximum decreases — under a parent with a hundred
// thousand children, every update to the busiest one. With the heap a change
// costs O(log b) at each level and stops at the first level where the maximum
// does not change. The heap lives in the node, takes one slot per child, and
// is rearranged in place, so updates do not allocate.
func (t *Tree[K]) Max(id K) (v float64, at K, ok bool) {
	i, ok := t.index[id]
	if !ok {
		return math.NaN(), at, false
	}
	n := &t.nodes[i]
	return n.mx, t.nodes[n.mxAt].id, true
}

// Rebuild recomputes every subtree sum from the own values, bottom-up, each
// as a fresh compensated sum, discarding whatever error the incremental
// updates accumulated. O(n); it allocates a list of the nodes in order. It
// is never needed for correctness; see Set for when it helps precision.
// Maxima are exact comparisons, never drift, and are left as they are.
func (t *Tree[K]) Rebuild() {
	order := make([]int, 0, len(t.index))
	for r := range t.nodes {
		if t.nodes[r].parent != noParent {
			continue
		}
		for u := r; u >= 0; u = t.next(u, r) {
			order = append(order, u)
		}
	}
	// In reverse preorder every node comes after all its descendants.
	for j := len(order) - 1; j >= 0; j-- {
		n := &t.nodes[order[j]]
		n.sum, n.comp = n.own, 0
		for _, k := range n.kids {
			n.add(t.nodes[k].sum)
			n.add(t.nodes[k].comp)
		}
	}
}

// add folds x into the node's compensated sum (Neumaier): the rounding error
// of sum + x is recovered exactly by the branch on magnitudes, and kept in
// comp.
func (n *node[K]) add(x float64) {
	s := n.sum + x
	if math.Abs(n.sum) >= math.Abs(x) {
		n.comp += (n.sum - s) + x
	} else {
		n.comp += (x - s) + n.sum
	}
	n.sum = s
}

// addUp adds x and then y to the sum of slot i and of every ancestor.
func (t *Tree[K]) addUp(i int, x, y float64) {
	for i >= 0 {
		n := &t.nodes[i]
		n.add(x)
		n.add(y)
		i = n.parent
	}
}

// ranks reports whether value v at slot a ranks above value w at slot b:
// larger value, then smaller key.
func (t *Tree[K]) ranks(v float64, a int, w float64, b int) bool {
	if v != w {
		return v > w
	}
	return t.nodes[a].id < t.nodes[b].id
}

// above orders slots in a heap: by subtree maximum, as ranks does.
func (t *Tree[K]) above(a, b int) bool {
	na, nb := &t.nodes[a], &t.nodes[b]
	return t.ranks(na.mx, na.mxAt, nb.mx, nb.mxAt)
}

// fixMax recomputes the maximum of slot u from its own value and the top of
// its heap, then repairs u's place in its parent's heap and carries on up,
// until a maximum comes out unchanged or the root is passed.
func (t *Tree[K]) fixMax(u int) {
	for {
		n := &t.nodes[u]
		v, at := n.own, u
		if len(n.heap) > 0 {
			if c := &t.nodes[n.heap[0]]; t.ranks(c.mx, c.mxAt, v, at) {
				v, at = c.mx, c.mxAt
			}
		}
		if v == n.mx && at == n.mxAt {
			return
		}
		n.mx, n.mxAt = v, at
		if n.parent < 0 {
			return
		}
		t.fixHeap(n.parent, n.hpos)
		u = n.parent
	}
}

// fixHeap restores the heap of slot p after the entry at index h changed.
func (t *Tree[K]) fixHeap(p, h int) {
	if !t.siftUp(p, h) {
		t.siftDown(p, h)
	}
}

func (t *Tree[K]) siftUp(p, h int) (moved bool) {
	heap := t.nodes[p].heap
	for h > 0 {
		up := (h - 1) / 2
		if !t.above(heap[h], heap[up]) {
			break
		}
		heap[h], heap[up] = heap[up], heap[h]
		t.nodes[heap[h]].hpos, t.nodes[heap[up]].hpos = h, up
		h, moved = up, true
	}
	return moved
}

func (t *Tree[K]) siftDown(p, h int) {
	heap := t.nodes[p].heap
	for {
		c := 2*h + 1
		if c >= len(heap) {
			return
		}
		if c+1 < len(heap) && t.above(heap[c+1], heap[c]) {
			c++
		}
		if !t.above(heap[c], heap[h]) {
			return
		}
		heap[h], heap[c] = heap[c], heap[h]
		t.nodes[heap[h]].hpos, t.nodes[heap[c]].hpos = h, c
		h = c
	}
}
