// SPDX-License-Identifier: BSD-3-Clause

package tree

import (
	"cmp"
	"iter"
	"slices"
)

// Snapshot is an immutable copy of a tree's structure: which nodes there
// were and the parent of each, a root recorded as its own parent. It holds
// no values. Keep one, and Diff it against a later one to see what changed.
//
// It is stored as a key-sorted list of (node, parent) pairs, so Diff is a
// single merge of two sorted lists. The zero value is an empty snapshot.
// Safe to copy and to share between goroutines.
type Snapshot[K cmp.Ordered] struct {
	edges []edge[K]
}

type edge[K cmp.Ordered] struct{ id, parent K }

// Snapshot copies the structure. O(n log n) for the sort; it allocates the
// copy.
func (t *Tree[K]) Snapshot() Snapshot[K] {
	edges := make([]edge[K], 0, len(t.index))
	for i := range t.nodes {
		n := &t.nodes[i]
		switch n.parent {
		case freeSlot:
		case noParent:
			edges = append(edges, edge[K]{n.id, n.id})
		default:
			edges = append(edges, edge[K]{n.id, t.nodes[n.parent].id})
		}
	}
	slices.SortFunc(edges, func(a, b edge[K]) int { return cmp.Compare(a.id, b.id) })
	return Snapshot[K]{edges}
}

// Len is the number of nodes in the snapshot.
func (s Snapshot[K]) Len() int { return len(s.edges) }

// Parent is id's parent when the snapshot was taken, id itself for a root;
// ok is false when id was not in the tree. O(log n).
func (s Snapshot[K]) Parent(id K) (parent K, ok bool) {
	j, ok := slices.BinarySearchFunc(s.edges, id, func(e edge[K], id K) int { return cmp.Compare(e.id, id) })
	if !ok {
		return parent, false
	}
	return s.edges[j].parent, true
}

// All yields every node and its parent (itself for a root), in increasing
// key order.
func (s Snapshot[K]) All() iter.Seq2[K, K] {
	return func(yield func(id, parent K) bool) {
		for _, e := range s.edges {
			if !yield(e.id, e.parent) {
				return
			}
		}
	}
}

// Move is a node whose parent changed between two snapshots. A root's parent
// is written as the node itself, as Tree.Move accepts it, so From == ID means
// the node was a root and To == ID that it became one.
type Move[K cmp.Ordered] struct {
	ID, From, To K
}

// Changes is what Diff found, each list in increasing key order.
//
// To turn the tree a was taken from into the structure of b, in an order
// that never needs a node that is not there yet and never forms a cycle:
//
//  1. AddRoot every Added node.
//  2. Move every Moved node to a root: Move(m.ID, m.ID).
//  3. Move every Added and every Moved node to its parent in b. Each such
//     edge is an edge of b, and every node it could pass through is now
//     linked by edges of b too, so none of these moves can be refused.
//  4. Remove the Removed nodes: by now their children are Removed nodes
//     too, so RemoveSubtree on each one still present (or Remove, deepest
//     first) finishes the job.
type Changes[K cmp.Ordered] struct {
	Added, Removed []K
	Moved          []Move[K]
}

// Diff compares two snapshots: nodes only in b are Added, nodes only in a
// Removed, and nodes in both whose parent differs Moved — never Removed and
// Added, so a node that changed place keeps its identity. O(len(a)+len(b)),
// a single merge of the two sorted lists; the result is deterministic.
//
// Snapshots see states, not history: a node removed and added again under
// a different parent between a and b shows as Moved, and one removed and
// added again under the same parent does not show at all.
func Diff[K cmp.Ordered](a, b Snapshot[K]) Changes[K] {
	var c Changes[K]
	i, j := 0, 0
	for i < len(a.edges) || j < len(b.edges) {
		switch {
		case j == len(b.edges) || i < len(a.edges) && a.edges[i].id < b.edges[j].id:
			c.Removed = append(c.Removed, a.edges[i].id)
			i++
		case i == len(a.edges) || b.edges[j].id < a.edges[i].id:
			c.Added = append(c.Added, b.edges[j].id)
			j++
		default:
			if from, to := a.edges[i].parent, b.edges[j].parent; from != to {
				c.Moved = append(c.Moved, Move[K]{a.edges[i].id, from, to})
			}
			i++
			j++
		}
	}
	return c
}
