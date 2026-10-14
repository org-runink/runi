// SPDX-License-Identifier: BSD-3-Clause

package tree

import (
	"cmp"
	"fmt"
	"slices"
)

// Tree is a forest of nodes keyed by K, each carrying a float64 value of its
// own, with the subtree sum and the subtree maximum of every node kept up to
// date as single nodes change.
//
// Every query is deterministic: children are kept in key order, and every tie
// (two equal maxima, two equal top-k values) goes to the smaller key. That is
// why K is cmp.Ordered rather than comparable.
//
// A root is its own parent. That is the usual parent-array representation of
// a forest (Cormen, Leiserson, Rivest and Stein, Introduction to Algorithms,
// 3rd ed., §21.3, "the root of each tree is its own parent"), and the package
// uses it everywhere a parent is named: Parent of a root returns the root,
// Add(id, id) adds a root, Move(id, id) makes id a root, and a Snapshot or a
// Move records a root's parent as the node itself. A loop that walks up by
// Parent stops when the parent equals the node; Path does that walk for you.
//
// Subtree sums use compensated summation (see Set), and subtree maxima a
// max-heap of children per node (see Max). Structure changes cost the depth
// of the node times log of the branching, plus a shift of the sibling list;
// the doc of each method says which. Indices, not pointers, link the nodes,
// and removed slots are reused, so a long-lived tree that adds and removes
// nodes at a steady rate does not grow.
//
// The zero value is an empty tree, ready to use; New returns the same. A
// Tree must not be copied after first use (a copy shares its storage), and is
// not safe for concurrent use.
type Tree[K cmp.Ordered] struct {
	index map[K]int
	nodes []node[K]
	free  []int // slots of removed nodes, reused by Add
}

type node[K cmp.Ordered] struct {
	id     K
	parent int   // slot of the parent; −1 for a root, −2 for a free slot
	pos    int   // index of this node in its parent's kids
	hpos   int   // index of this node in its parent's heap
	kids   []int // children, in key order
	heap   []int // the same children, a max-heap by subtree maximum

	own       float64
	sum, comp float64 // subtree sum is sum + comp (Neumaier)
	mx        float64 // largest own value in the subtree,
	mxAt      int     // held by this slot; ties to the smaller key
}

const (
	noParent = -1
	freeSlot = -2
)

// New returns an empty tree.
func New[K cmp.Ordered]() *Tree[K] { return &Tree[K]{} }

// Len is the number of nodes.
func (t *Tree[K]) Len() int { return len(t.index) }

// AddRoot adds id as a new root. It fails with ErrExists if id is already in
// the tree, and with ErrInvalid if id is a NaN (which no map lookup could
// ever find again). A new node's own value is 0.
func (t *Tree[K]) AddRoot(id K) error { return t.Add(id, id) }

// Add adds id as a new leaf under parent, or as a root when parent == id.
// It fails with ErrExists if id is already in the tree, ErrUnknown if parent
// is not, and ErrInvalid if id is a NaN. A new node's own value is 0, so the
// sums do not change; the maxima can (a 0 can beat negative values, or tie a
// 0 and win on key).
//
// Cost: O(b) to insert into the parent's key-ordered child list, b the
// number of siblings, plus O(depth · log b) to update the maxima above it.
func (t *Tree[K]) Add(id, parent K) error {
	if id != id {
		return fmt.Errorf("%w: NaN key", ErrInvalid)
	}
	if _, ok := t.index[id]; ok {
		return fmt.Errorf("%w: %v", ErrExists, id)
	}
	p := noParent
	if parent != id {
		var ok bool
		if p, ok = t.index[parent]; !ok {
			return fmt.Errorf("%w: parent %v of %v", ErrUnknown, parent, id)
		}
	}
	t.attach(t.alloc(id), p)
	return nil
}

// Move makes newParent the parent of id, carrying id's whole subtree with
// it; Move(id, id) makes id a root. Moving a node to the parent it already
// has does nothing.
//
// A move under id itself or under any of its descendants would cut the
// subtree off from every root into a cycle, and fails with ErrCycle. The
// check walks up from newParent towards its root looking for id: O(depth of
// newParent), no extra memory, and nothing else in the tree is touched.
// Unknown id or newParent fails with ErrUnknown.
//
// Cost: the check, plus for each of the old and new parent O(b) to update
// its child list and O(depth · log b) to update sums and maxima on the path
// to the root. The subtree's own sums and maxima are unchanged, so its size
// does not matter.
func (t *Tree[K]) Move(id, newParent K) error {
	i, ok := t.index[id]
	if !ok {
		return unknown(id)
	}
	q := noParent
	if newParent != id {
		if q, ok = t.index[newParent]; !ok {
			return fmt.Errorf("%w: new parent %v of %v", ErrUnknown, newParent, id)
		}
		for u := q; u >= 0; u = t.nodes[u].parent {
			if u == i {
				return fmt.Errorf("%w: %v under its descendant %v", ErrCycle, id, newParent)
			}
		}
	}
	if t.nodes[i].parent == q {
		return nil
	}
	n := &t.nodes[i]
	s, c := n.sum, n.comp
	if p := n.parent; p >= 0 {
		t.detach(i)
		t.addUp(p, -s, -c)
	}
	t.attach(i, q)
	if q >= 0 {
		t.addUp(q, s, c)
	}
	return nil
}

// Remove removes id, which must be a leaf; a node with children fails with
// ErrHasChildren, an unknown one with ErrUnknown. Its own value leaves the
// sums of its ancestors.
//
// Only leaves, because the point of the tree is attribution: removing an
// inner node would either drop its descendants' values from every ancestor's
// sum without the caller having said so, or silently hand them to the
// grandparent. Remove bottom-up, move the children elsewhere first, or say
// explicitly that the whole subtree goes with RemoveSubtree.
//
// Cost: O(b + depth · log b), as Add.
func (t *Tree[K]) Remove(id K) error {
	i, ok := t.index[id]
	if !ok {
		return unknown(id)
	}
	n := &t.nodes[i]
	if len(n.kids) > 0 {
		return fmt.Errorf("%w: %v has %d", ErrHasChildren, id, len(n.kids))
	}
	if p := n.parent; p >= 0 {
		own := n.own
		t.detach(i)
		t.addUp(p, -own, 0)
	}
	t.release(i)
	return nil
}

// RemoveSubtree removes id and all its descendants, and returns how many
// nodes that was. Their values leave the sums of id's ancestors. Unknown id
// fails with ErrUnknown and removes nothing.
//
// Cost: O(subtree size) plus, for id's parent, the cost of Remove. It
// allocates a list of the subtree's slots.
func (t *Tree[K]) RemoveSubtree(id K) (removed int, err error) {
	i, ok := t.index[id]
	if !ok {
		return 0, unknown(id)
	}
	if p := t.nodes[i].parent; p >= 0 {
		s, c := t.nodes[i].sum, t.nodes[i].comp
		t.detach(i)
		t.addUp(p, -s, -c)
	}
	var doomed []int
	for u := i; u >= 0; u = t.next(u, i) {
		doomed = append(doomed, u)
	}
	for _, u := range doomed {
		t.release(u)
	}
	return len(doomed), nil
}

// Parent returns id's parent, or id itself when id is a root. ok is false
// only when id is not in the tree. O(1).
func (t *Tree[K]) Parent(id K) (parent K, ok bool) {
	i, ok := t.index[id]
	if !ok {
		return parent, false
	}
	if p := t.nodes[i].parent; p >= 0 {
		return t.nodes[p].id, true
	}
	return id, true
}

// Depth is the number of edges from id up to its root: 0 for a root. ok is
// false when id is not in the tree. O(depth): depths are not stored, because
// keeping them would make Move cost the size of the subtree moved.
func (t *Tree[K]) Depth(id K) (depth int, ok bool) {
	i, ok := t.index[id]
	if !ok {
		return 0, false
	}
	for i = t.nodes[i].parent; i >= 0; i = t.nodes[i].parent {
		depth++
	}
	return depth, true
}

// Children appends id's children to dst in increasing key order and returns
// the extended slice. It appends nothing when id is a leaf or not in the
// tree. O(b); it does not allocate when dst has room for the children.
func (t *Tree[K]) Children(id K, dst []K) []K {
	i, ok := t.index[id]
	if !ok {
		return dst
	}
	for _, k := range t.nodes[i].kids {
		dst = append(dst, t.nodes[k].id)
	}
	return dst
}

// Path appends the keys from id's root down to id, both included, to dst and
// returns the extended slice. It appends nothing when id is not in the tree.
// O(depth); it does not allocate when dst has room for depth+1 keys.
func (t *Tree[K]) Path(id K, dst []K) []K {
	i, ok := t.index[id]
	if !ok {
		return dst
	}
	base := len(dst)
	for ; i >= 0; i = t.nodes[i].parent {
		dst = append(dst, t.nodes[i].id)
	}
	slices.Reverse(dst[base:])
	return dst
}

// alloc takes a slot for id, reusing a removed one when there is one (with
// its child-list storage), and indexes it. The slot is not yet linked.
func (t *Tree[K]) alloc(id K) int {
	var i int
	if n := len(t.free); n > 0 {
		i, t.free = t.free[n-1], t.free[:n-1]
		old := &t.nodes[i]
		*old = node[K]{id: id, kids: old.kids[:0], heap: old.heap[:0]}
	} else {
		i = len(t.nodes)
		t.nodes = append(t.nodes, node[K]{id: id})
	}
	t.nodes[i].mxAt = i
	if t.index == nil {
		t.index = make(map[K]int)
	}
	t.index[id] = i
	return i
}

// release frees slot i, which must already be unlinked from any parent that
// survives it.
func (t *Tree[K]) release(i int) {
	n := &t.nodes[i]
	delete(t.index, n.id)
	var zero K
	n.id, n.parent = zero, freeSlot // zero: let a string key be collected
	t.free = append(t.free, i)
}

// attach links slot i, currently a root, under p (or leaves it a root when
// p < 0) and updates the maxima above it. Sums are the caller's business.
func (t *Tree[K]) attach(i, p int) {
	n := &t.nodes[i]
	n.parent = p
	if p < 0 {
		return
	}
	pn := &t.nodes[p]
	at, _ := slices.BinarySearchFunc(pn.kids, n.id, func(k int, id K) int { return cmp.Compare(t.nodes[k].id, id) })
	pn.kids = slices.Insert(pn.kids, at, i)
	for j := at; j < len(pn.kids); j++ {
		t.nodes[pn.kids[j]].pos = j
	}
	n.hpos = len(pn.heap)
	pn.heap = append(pn.heap, i)
	t.siftUp(p, n.hpos)
	t.fixMax(p)
}

// detach unlinks slot i from its parent, leaving it a root, and updates the
// maxima above the old parent. Sums are the caller's business.
func (t *Tree[K]) detach(i int) {
	n := &t.nodes[i]
	p := n.parent
	pn := &t.nodes[p]
	pn.kids = slices.Delete(pn.kids, n.pos, n.pos+1)
	for j := n.pos; j < len(pn.kids); j++ {
		t.nodes[pn.kids[j]].pos = j
	}
	last := len(pn.heap) - 1
	h := n.hpos
	pn.heap[h] = pn.heap[last]
	t.nodes[pn.heap[h]].hpos = h
	pn.heap = pn.heap[:last]
	if h < last {
		t.fixHeap(p, h)
	}
	n.parent = noParent
	t.fixMax(p)
}

// next is the slot after u in a preorder walk of root's subtree, −1 at the
// end. A walk of the whole subtree is O(size) and needs no stack: pos says
// where each node sits among its siblings.
func (t *Tree[K]) next(u, root int) int {
	if n := &t.nodes[u]; len(n.kids) > 0 {
		return n.kids[0]
	}
	for u != root {
		n := &t.nodes[u]
		if p := &t.nodes[n.parent]; n.pos+1 < len(p.kids) {
			return p.kids[n.pos+1]
		}
		u = n.parent
	}
	return -1
}
