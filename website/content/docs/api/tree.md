---
title: "runi/tree"
weight: 16
---

```go
import "github.com/org-runink/runi/tree"
```

Package tree attributes a quantity measured per node of a hierarchy to the
subtrees that cause it, and keeps that attribution current as single nodes
change.

Many quantities are measured per node of a hierarchy but asked about per
subtree: resource use or pressure per group in a hierarchy of process
groups, bytes per directory, cost per team in an organisation. "How much
does this group use, all of it included?" is a subtree sum; "which part of
it is responsible?" is the largest values below it. Recomputing either from
scratch is O(n) per question. A Tree keeps every node's subtree sum and
subtree maximum up to date as values and structure change, at a cost of
the node's depth per change, so the questions become O(1) (sum, maximum)
or O(subtree) (top k), and every answer is deterministic: children are
kept in key order and every tie goes to the smaller key.

Which to use:

  - Tree holds the hierarchy (Add, AddRoot, Move, Remove, RemoveSubtree)
    and the values (Set). Sum and Own are the subtree total and the node's
    own share; Max is the single largest own value below a node and where
    it is; TopK is the k heaviest descendants by own value or by subtree
    sum; Path and Depth say where a node sits.
  - Snapshot copies the structure, without values, and Diff compares two
    copies: which nodes appeared, disappeared, or moved to another parent.
    It is how to report what changed in a hierarchy between two polls.

Bytes per directory, and the directories that hold most of them:

t := tree.New[string]()
t.AddRoot("/")
t.Add("/var", "/")
t.Add("/var/log", "/var")
t.Set("/var/log", 7e9)
fmt.Println(t.Sum("/")) // 7e+09
top := t.TopK("/", 3, tree.BySum, nil)

A Tree is a forest: it may have several roots. A root is its own parent
wherever a parent is named: Parent of a root returns the root, Add(id, id)
adds one, Move(id, id) makes one, and Diff reports a node that became a
root as moved to itself.

Sums are kept with compensated summation, so a million updates to large
values do not round away the small values beside them; Set gives the
bound, and a test holds it. Set rejects NaN and ±Inf with ErrInvalid
instead of skipping them as this module's streaming summaries do: a value
here is state shared by every ancestor's sum, and a NaN added once could
never be subtracted out again. Every error wraps ErrInvalid, and the
specific ones (ErrUnknown, ErrExists, ErrCycle, ErrHasChildren) can each
be matched with errors.Is.

Set, Sum, Own, Max, Parent and Depth do not allocate, and neither do
Children, Path and TopK when the destination slice has room; a test holds
each of these to zero. A Tree is not safe for concurrent use; guard it.

# What this package does not do

It does not read hierarchies. Walking a filesystem, a process-group
hierarchy or an organisation chart, and deciding when, is the caller's
business; a Tree is told about each node and each value.

It keeps sums and maxima only. Both can be updated along one path to the
root; a median or a percentile of a subtree cannot, and needs a mergeable
quantile sketch per node, such as the KLL sketch (Karnin, Lang and
Liberty, 2016) or the t-digest (Dunning and Ertl, 2019), which this
package does not provide. A minimum is a maximum of negated values: keep a
second Tree. A mean is Sum divided by a count kept as a second Tree whose
every value is 1.

It is fast for bushy hierarchies, not for long chains. Every update costs
the depth of the node, which for directory trees and group hierarchies is
a handful; on a path of a hundred thousand nodes it is a hundred thousand.
Where depth is unbounded, link-cut trees (D. D. Sleator and R. E. Tarjan,
"A data structure for dynamic trees", J. Comput. Syst. Sci. 26(3), 1983)
or Euler-tour trees answer the same path and subtree aggregates in
O(log n) amortised per update, at a large constant and in far more code.

TopK walks the subtree it is asked about, O(size · log k). Asked of the
root of a million nodes many times a second, that is the cost; a caller
who needs it that often should ask about smaller subtrees, or maintain
the ranking it needs alongside.

It is not a time series. A value is the current measurement and replaces
the previous one. To attribute a rate, turn each node's cumulative counter
into one first with Counter from github.com/org-runink/runi/rate; to
smooth it, Decayed or EWMA from github.com/org-runink/runi/drift; and to
ask whether a subtree's sum has changed regime, feed Sum to a detector
there.

Diff compares structure, not values, and sees states, not history: a
node removed and added back between two snapshots is invisible to it.

## Index

- `type` [Changes](#changes)
- `type` [Entry](#entry)
- `type` [Metric](#metric)
- `type` [Move](#move)
- `type` [Snapshot](#snapshot)
- `type` [Tree](#tree)
- `func` [Diff](#diff)
- `const` [ErrInvalid](#errinvalid)
- `method` [Metric.String](#metricstring)
- `func` [New](#new)
- `method` [Snapshot.All](#snapshotall)
- `method` [Snapshot.Len](#snapshotlen)
- `method` [Snapshot.Parent](#snapshotparent)
- `method` [Tree.Add](#treeadd)
- `method` [Tree.AddRoot](#treeaddroot)
- `method` [Tree.Children](#treechildren)
- `method` [Tree.Depth](#treedepth)
- `method` [Tree.Len](#treelen)
- `method` [Tree.Max](#treemax)
- `method` [Tree.Move](#treemove)
- `method` [Tree.Own](#treeown)
- `method` [Tree.Parent](#treeparent)
- `method` [Tree.Path](#treepath)
- `method` [Tree.Rebuild](#treerebuild)
- `method` [Tree.Remove](#treeremove)
- `method` [Tree.RemoveSubtree](#treeremovesubtree)
- `method` [Tree.Set](#treeset)
- `method` [Tree.Snapshot](#treesnapshot)
- `method` [Tree.Sum](#treesum)
- `method` [Tree.TopK](#treetopk)

### Changes

```go
type Changes[K cmp.Ordered] struct
```

Changes is what Diff found, each list in increasing key order.

To turn the tree a was taken from into the structure of b, in an order
that never needs a node that is not there yet and never forms a cycle:

 1. AddRoot every Added node.
 2. Move every Moved node to a root: Move(m.ID, m.ID).
 3. Move every Added and every Moved node to its parent in b. Each such
    edge is an edge of b, and every node it could pass through is now
    linked by edges of b too, so none of these moves can be refused.
 4. Remove the Removed nodes: by now their children are Removed nodes
    too, so RemoveSubtree on each one still present (or Remove, deepest
    first) finishes the job.

<small>Defined in <code>tree/snapshot.go</code>.</small>

### Entry

```go
type Entry[K cmp.Ordered] struct
```

Entry is one result of TopK.

<small>Defined in <code>tree/topk.go</code>.</small>

### Metric

```go
type Metric int
```

Metric chooses the value TopK ranks nodes by.

<small>Defined in <code>tree/topk.go</code>.</small>

### Move

```go
type Move[K cmp.Ordered] struct
```

Move is a node whose parent changed between two snapshots. A root's parent
is written as the node itself, as Tree.Move accepts it, so From == ID means
the node was a root and To == ID that it became one.

<small>Defined in <code>tree/snapshot.go</code>.</small>

### Snapshot

```go
type Snapshot[K cmp.Ordered] struct
```

Snapshot is an immutable copy of a tree's structure: which nodes there
were and the parent of each, a root recorded as its own parent. It holds
no values. Keep one, and Diff it against a later one to see what changed.

It is stored as a key-sorted list of (node, parent) pairs, so Diff is a
single merge of two sorted lists. The zero value is an empty snapshot.
Safe to copy and to share between goroutines.

<small>Defined in <code>tree/snapshot.go</code>.</small>

### Tree

```go
type Tree[K cmp.Ordered] struct
```

Tree is a forest of nodes keyed by K, each carrying a float64 value of its
own, with the subtree sum and the subtree maximum of every node kept up to
date as single nodes change.

Every query is deterministic: children are kept in key order, and every tie
(two equal maxima, two equal top-k values) goes to the smaller key. That is
why K is cmp.Ordered rather than comparable.

A root is its own parent. That is the usual parent-array representation of
a forest (Cormen, Leiserson, Rivest and Stein, Introduction to Algorithms,
3rd ed., §21.3, "the root of each tree is its own parent"), and the package
uses it everywhere a parent is named: Parent of a root returns the root,
Add(id, id) adds a root, Move(id, id) makes id a root, and a Snapshot or a
Move records a root's parent as the node itself. A loop that walks up by
Parent stops when the parent equals the node; Path does that walk for you.

Subtree sums use compensated summation (see Set), and subtree maxima a
max-heap of children per node (see Max). Structure changes cost the depth
of the node times log of the branching, plus a shift of the sibling list;
the doc of each method says which. Indices, not pointers, link the nodes,
and removed slots are reused, so a long-lived tree that adds and removes
nodes at a steady rate does not grow.

The zero value is an empty tree, ready to use; New returns the same. A
Tree must not be copied after first use (a copy shares its storage), and is
not safe for concurrent use.

<small>Defined in <code>tree/tree.go</code>.</small>

### Diff

```go
func Diff[K cmp.Ordered](a, b Snapshot[K]) Changes[K]
```

Diff compares two snapshots: nodes only in b are Added, nodes only in a
Removed, and nodes in both whose parent differs Moved — never Removed and
Added, so a node that changed place keeps its identity. O(len(a)+len(b)),
a single merge of the two sorted lists; the result is deterministic.

Snapshots see states, not history: a node removed and added again under
a different parent between a and b shows as Moved, and one removed and
added again under the same parent does not show at all.

<small>Defined in <code>tree/snapshot.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("tree: invalid argument")
```

ErrInvalid is wrapped by every error this package returns: an argument the
tree cannot accept in its current state. The more specific sentinels below
wrap it too, so errors.Is(err, ErrInvalid) holds for all of them. Match
with errors.Is; the message text is not part of the API.

<small>Defined in <code>tree/errors.go</code>.</small>

### Metric.String

```go
func (m Metric) String() string
```

String is "own" or "sum".

<small>Defined in <code>tree/topk.go</code>.</small>

### New

```go
func New[K cmp.Ordered]() *Tree[K]
```

New returns an empty tree.

<small>Defined in <code>tree/tree.go</code>.</small>

### Snapshot.All

```go
func (s Snapshot[K]) All() iter.Seq2[K, K]
```

All yields every node and its parent (itself for a root), in increasing
key order.

<small>Defined in <code>tree/snapshot.go</code>.</small>

### Snapshot.Len

```go
func (s Snapshot[K]) Len() int
```

Len is the number of nodes in the snapshot.

<small>Defined in <code>tree/snapshot.go</code>.</small>

### Snapshot.Parent

```go
func (s Snapshot[K]) Parent(id K) (parent K, ok bool)
```

Parent is id's parent when the snapshot was taken, id itself for a root;
ok is false when id was not in the tree. O(log n).

<small>Defined in <code>tree/snapshot.go</code>.</small>

### Tree.Add

```go
func (t *Tree[K]) Add(id, parent K) error
```

Add adds id as a new leaf under parent, or as a root when parent == id.
It fails with ErrExists if id is already in the tree, ErrUnknown if parent
is not, and ErrInvalid if id is a NaN. A new node's own value is 0, so the
sums do not change; the maxima can (a 0 can beat negative values, or tie a
0 and win on key).

Cost: O(b) to insert into the parent's key-ordered child list, b the
number of siblings, plus O(depth · log b) to update the maxima above it.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.AddRoot

```go
func (t *Tree[K]) AddRoot(id K) error
```

AddRoot adds id as a new root. It fails with ErrExists if id is already in
the tree, and with ErrInvalid if id is a NaN (which no map lookup could
ever find again). A new node's own value is 0.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Children

```go
func (t *Tree[K]) Children(id K, dst []K) []K
```

Children appends id's children to dst in increasing key order and returns
the extended slice. It appends nothing when id is a leaf or not in the
tree. O(b); it does not allocate when dst has room for the children.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Depth

```go
func (t *Tree[K]) Depth(id K) (depth int, ok bool)
```

Depth is the number of edges from id up to its root: 0 for a root. ok is
false when id is not in the tree. O(depth): depths are not stored, because
keeping them would make Move cost the size of the subtree moved.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Len

```go
func (t *Tree[K]) Len() int
```

Len is the number of nodes.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Max

```go
func (t *Tree[K]) Max(id K) (v float64, at K, ok bool)
```

Max is the largest own value in id's subtree, id included, and the node
that holds it; on a tie, the smaller key. ok is false, and v NaN, when id
is not in the tree. O(1).

Each node keeps its subtree maximum, and its children in a binary max-heap
ordered by their subtree maxima (J. W. J. Williams, "Algorithm 232:
Heapsort", Commun. ACM 7(6), 1964; Cormen et al., Introduction to
Algorithms, ch. 6), so a node's maximum is the larger of its own value and
the top of its heap. Rescanning the children instead would be simpler, and
costs O(b) whenever the maximum decreases — under a parent with a hundred
thousand children, every update to the busiest one. With the heap a change
costs O(log b) at each level and stops at the first level where the maximum
does not change. The heap lives in the node, takes one slot per child, and
is rearranged in place, so updates do not allocate.

<small>Defined in <code>tree/values.go</code>.</small>

### Tree.Move

```go
func (t *Tree[K]) Move(id, newParent K) error
```

Move makes newParent the parent of id, carrying id's whole subtree with
it; Move(id, id) makes id a root. Moving a node to the parent it already
has does nothing.

A move under id itself or under any of its descendants would cut the
subtree off from every root into a cycle, and fails with ErrCycle. The
check walks up from newParent towards its root looking for id: O(depth of
newParent), no extra memory, and nothing else in the tree is touched.
Unknown id or newParent fails with ErrUnknown.

Cost: the check, plus for each of the old and new parent O(b) to update
its child list and O(depth · log b) to update sums and maxima on the path
to the root. The subtree's own sums and maxima are unchanged, so its size
does not matter.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Own

```go
func (t *Tree[K]) Own(id K) float64
```

Own is id's own value, 0 if never set, NaN if id is not in the tree. O(1).

<small>Defined in <code>tree/values.go</code>.</small>

### Tree.Parent

```go
func (t *Tree[K]) Parent(id K) (parent K, ok bool)
```

Parent returns id's parent, or id itself when id is a root. ok is false
only when id is not in the tree. O(1).

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Path

```go
func (t *Tree[K]) Path(id K, dst []K) []K
```

Path appends the keys from id's root down to id, both included, to dst and
returns the extended slice. It appends nothing when id is not in the tree.
O(depth); it does not allocate when dst has room for depth+1 keys.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Rebuild

```go
func (t *Tree[K]) Rebuild()
```

Rebuild recomputes every subtree sum from the own values, bottom-up, each
as a fresh compensated sum, discarding whatever error the incremental
updates accumulated. O(n); it allocates a list of the nodes in order. It
is never needed for correctness; see Set for when it helps precision.
Maxima are exact comparisons, never drift, and are left as they are.

<small>Defined in <code>tree/values.go</code>.</small>

### Tree.Remove

```go
func (t *Tree[K]) Remove(id K) error
```

Remove removes id, which must be a leaf; a node with children fails with
ErrHasChildren, an unknown one with ErrUnknown. Its own value leaves the
sums of its ancestors.

Only leaves, because the point of the tree is attribution: removing an
inner node would either drop its descendants' values from every ancestor's
sum without the caller having said so, or silently hand them to the
grandparent. Remove bottom-up, move the children elsewhere first, or say
explicitly that the whole subtree goes with RemoveSubtree.

Cost: O(b + depth · log b), as Add.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.RemoveSubtree

```go
func (t *Tree[K]) RemoveSubtree(id K) (removed int, err error)
```

RemoveSubtree removes id and all its descendants, and returns how many
nodes that was. Their values leave the sums of id's ancestors. Unknown id
fails with ErrUnknown and removes nothing.

Cost: O(subtree size) plus, for id's parent, the cost of Remove. It
allocates a list of the subtree's slots.

<small>Defined in <code>tree/tree.go</code>.</small>

### Tree.Set

```go
func (t *Tree[K]) Set(id K, v float64) error
```

Set sets id's own value to v and updates the subtree sum and maximum of
every ancestor. It does not allocate.

Sums are updated incrementally, not recomputed: v is added to, and the old
value subtracted from, the sum of id and of each ancestor, O(depth). Done
naively that loses precision for good: after a node goes from 0 to 1e16
and back, a sibling's 1 has been rounded away from every ancestor's sum
(1e16 + 1 is 1e16 in float64) and nothing ever brings it back. So each sum
is kept as a pair, a running sum and a running compensation that collects
exactly the low-order part each addition rounded off, which is Neumaier's
improvement of Kahan's compensated summation (A. Neumaier, "Rundungsfehler-
analyse einiger Verfahren zur Summation endlicher Summen", ZAMM 54, 1974;
see N. J. Higham, Accuracy and Stability of Numerical Algorithms, 2nd ed.,
SIAM 2002, §4.3). Sum returns sum + compensation.

The error of a compensated sum of n terms x₁…xₙ with exact total S is at
most u·|S| + γ²·Σ|xᵢ|, u = 2⁻⁵³ and γ = n·u/(1−n·u) (T. Ogita, S. M. Rump
and S. Oishi, "Accurate sum and dot product", SIAM J. Sci. Comput. 26(6),
2005, Proposition 4.5, for the equivalent Sum2). Every Set below a node
adds two terms to that node's sum. The first part is the rounding of the
answer itself, the best any method can do. The second is the drift, and
it grows with the cube of the number of updates below the node (the
square from γ², once more from Σ|xᵢ|): for values about as large as the
sum it is some tens of u·|S|, a relative 1e-14, after 10⁶ updates, and
1e-11 after 10⁷. That is a guarantee for the worst case, not the typical
error. The test that holds the bound (10⁶ updates, values over fifteen
orders of magnitude, random signs, up to 36 levels deep) also requires
that no node's error exceeds u·|S|, which holds; a naive running sum on
the same updates is off by 0.1 at the root and by more than 10¹⁵·u·|S| at
a node whose sum nearly cancels. Rebuild recomputes every sum from the own
values and drops the drift; a tree that must guarantee eleven digits
through more than 10⁷ updates per node should call it that often.

v must be finite and |v| ≤ 2⁹⁶⁰ (about 1e289), or Set fails with ErrInvalid
and changes nothing. Unlike the streaming summaries elsewhere in this
module, which skip a non-finite reading and carry on, a value here is
state that every ancestor's sum shares: a NaN, once added, makes every sum
above it NaN, and no later Set can subtract it out again (NaN − NaN is
NaN); an infinity does the same through ∞ − ∞. Skipping silently would
leave the previous value attributed as if it were current. Rejecting lets
the caller decide what a missing reading means: often 0, sometimes the
last good value. The bound keeps every sum, and every intermediate term,
finite. Unknown id fails with ErrUnknown.

The maximum is updated by walking up the same path, repairing each
ancestor's heap of children (see Max): O(depth · log b) for branching b,
and it stops as soon as an ancestor's maximum is unchanged, which for most
updates is immediately.

<small>Defined in <code>tree/values.go</code>.</small>

### Tree.Snapshot

```go
func (t *Tree[K]) Snapshot() Snapshot[K]
```

Snapshot copies the structure. O(n log n) for the sort; it allocates the
copy.

<small>Defined in <code>tree/snapshot.go</code>.</small>

### Tree.Sum

```go
func (t *Tree[K]) Sum(id K) float64
```

Sum is the total of the own values in id's subtree, id included; NaN if
id is not in the tree. O(1). See Set for its accuracy.

<small>Defined in <code>tree/values.go</code>.</small>

### Tree.TopK

```go
func (t *Tree[K]) TopK(id K, k int, metric Metric, dst []Entry[K]) []Entry[K]
```

TopK appends to dst the k nodes of id's subtree with the largest value by
metric, largest first, ties to the smaller key, and returns the extended
slice. id itself is not a candidate: the question is which descendants
account for most of id, and by BySum id would always come first with a
value the caller already has from Sum. Fewer than k are appended when id
has fewer descendants. It appends nothing when id is not in the tree, k ≤ 0,
or metric is neither ByOwn nor BySum.

Candidates come from every depth, so by BySum a node and its own parent
can both appear: a child that accounts for most of its parent ranks just
below it. That is the honest answer to "which subtrees are heaviest"; to
rank only id's direct children, use Children and Sum.

It walks the whole subtree, O(size · log k), keeping the best k so far in
a bounded min-heap held in dst's own storage beyond its current length,
then sorts that in place (heapsort). It does not allocate when dst has
room for k more entries.

<small>Defined in <code>tree/topk.go</code>.</small>
