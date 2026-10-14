// SPDX-License-Identifier: BSD-3-Clause

// Package tree attributes a quantity measured per node of a hierarchy to the
// subtrees that cause it, and keeps that attribution current as single nodes
// change.
//
// Many quantities are measured per node of a hierarchy but asked about per
// subtree: resource use or pressure per group in a hierarchy of process
// groups, bytes per directory, cost per team in an organisation. "How much
// does this group use, all of it included?" is a subtree sum; "which part of
// it is responsible?" is the largest values below it. Recomputing either from
// scratch is O(n) per question. A Tree keeps every node's subtree sum and
// subtree maximum up to date as values and structure change, at a cost of
// the node's depth per change, so the questions become O(1) (sum, maximum)
// or O(subtree) (top k), and every answer is deterministic: children are
// kept in key order and every tie goes to the smaller key.
//
// Which to use:
//
//   - Tree holds the hierarchy (Add, AddRoot, Move, Remove, RemoveSubtree)
//     and the values (Set). Sum and Own are the subtree total and the node's
//     own share; Max is the single largest own value below a node and where
//     it is; TopK is the k heaviest descendants by own value or by subtree
//     sum; Path and Depth say where a node sits.
//   - Snapshot copies the structure, without values, and Diff compares two
//     copies: which nodes appeared, disappeared, or moved to another parent.
//     It is how to report what changed in a hierarchy between two polls.
//
// Bytes per directory, and the directories that hold most of them:
//
//	t := tree.New[string]()
//	t.AddRoot("/")
//	t.Add("/var", "/")
//	t.Add("/var/log", "/var")
//	t.Set("/var/log", 7e9)
//	fmt.Println(t.Sum("/")) // 7e+09
//	top := t.TopK("/", 3, tree.BySum, nil)
//
// A Tree is a forest: it may have several roots. A root is its own parent
// wherever a parent is named: Parent of a root returns the root, Add(id, id)
// adds one, Move(id, id) makes one, and Diff reports a node that became a
// root as moved to itself.
//
// Sums are kept with compensated summation, so a million updates to large
// values do not round away the small values beside them; Set gives the
// bound, and a test holds it. Set rejects NaN and ±Inf with ErrInvalid
// instead of skipping them as this module's streaming summaries do: a value
// here is state shared by every ancestor's sum, and a NaN added once could
// never be subtracted out again. Every error wraps ErrInvalid, and the
// specific ones (ErrUnknown, ErrExists, ErrCycle, ErrHasChildren) can each
// be matched with errors.Is.
//
// Set, Sum, Own, Max, Parent and Depth do not allocate, and neither do
// Children, Path and TopK when the destination slice has room; a test holds
// each of these to zero. A Tree is not safe for concurrent use; guard it.
//
// # What this package does not do
//
// It does not read hierarchies. Walking a filesystem, a process-group
// hierarchy or an organisation chart, and deciding when, is the caller's
// business; a Tree is told about each node and each value.
//
// It keeps sums and maxima only. Both can be updated along one path to the
// root; a median or a percentile of a subtree cannot, and needs a mergeable
// quantile sketch per node, such as the KLL sketch (Karnin, Lang and
// Liberty, 2016) or the t-digest (Dunning and Ertl, 2019), which this
// package does not provide. A minimum is a maximum of negated values: keep a
// second Tree. A mean is Sum divided by a count kept as a second Tree whose
// every value is 1.
//
// It is fast for bushy hierarchies, not for long chains. Every update costs
// the depth of the node, which for directory trees and group hierarchies is
// a handful; on a path of a hundred thousand nodes it is a hundred thousand.
// Where depth is unbounded, link-cut trees (D. D. Sleator and R. E. Tarjan,
// "A data structure for dynamic trees", J. Comput. Syst. Sci. 26(3), 1983)
// or Euler-tour trees answer the same path and subtree aggregates in
// O(log n) amortised per update, at a large constant and in far more code.
//
// TopK walks the subtree it is asked about, O(size · log k). Asked of the
// root of a million nodes many times a second, that is the cost; a caller
// who needs it that often should ask about smaller subtrees, or maintain
// the ranking it needs alongside.
//
// It is not a time series. A value is the current measurement and replaces
// the previous one. To attribute a rate, turn each node's cumulative counter
// into one first with Counter from github.com/org-runink/runi/rate; to
// smooth it, Decayed or EWMA from github.com/org-runink/runi/drift; and to
// ask whether a subtree's sum has changed regime, feed Sum to a detector
// there.
//
// Diff compares structure, not values, and sees states, not history: a
// node removed and added back between two snapshots is invisible to it.
package tree
