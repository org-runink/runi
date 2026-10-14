// SPDX-License-Identifier: BSD-3-Clause

package tree_test

import (
	"errors"
	"fmt"

	"github.com/org-runink/runi/tree"
)

// CPU time per group in a hierarchy of process groups, attributed to the
// subtrees that use it. Only leaves hold time of their own here, but inner
// nodes may too. TopK by BySum names the heaviest subtrees at any depth, so
// a group and the child that accounts for most of it both appear; by ByOwn,
// the single heaviest groups.
func Example() {
	t := tree.New[string]()
	if err := t.AddRoot("/"); err != nil {
		panic(err)
	}
	for _, g := range [][2]string{
		{"/system", "/"}, {"/system/logger", "/system"}, {"/system/sshd", "/system"},
		{"/user", "/"}, {"/user/build", "/user"}, {"/user/editor", "/user"},
		{"/user/build/compile", "/user/build"}, {"/user/build/link", "/user/build"},
	} {
		if err := t.Add(g[0], g[1]); err != nil {
			panic(err)
		}
	}
	for g, seconds := range map[string]float64{
		"/system/logger": 2, "/system/sshd": 1, "/user/editor": 6,
		"/user/build/compile": 40, "/user/build/link": 25,
	} {
		if err := t.Set(g, seconds); err != nil {
			panic(err)
		}
	}
	fmt.Println("total", t.Sum("/"), "user", t.Sum("/user"), "system", t.Sum("/system"))
	for _, e := range t.TopK("/", 3, tree.BySum, nil) {
		fmt.Printf("  %-20s %3.0f s\n", e.ID, e.Value)
	}
	v, at, _ := t.Max("/")
	fmt.Println("busiest single group:", at, v, "on path", t.Path(at, nil))

	// The link step finishes: its time goes, and the ranking follows.
	if err := t.Set("/user/build/link", 0); err != nil {
		panic(err)
	}
	fmt.Println("total", t.Sum("/"))
	for _, e := range t.TopK("/user", 2, tree.ByOwn, nil) {
		fmt.Printf("  %-20s %3.0f s\n", e.ID, e.Value)
	}
	// Output:
	// total 74 user 71 system 3
	//   /user                 71 s
	//   /user/build           65 s
	//   /user/build/compile   40 s
	// busiest single group: /user/build/compile 40 on path [/ /user /user/build /user/build/compile]
	// total 49
	//   /user/build/compile   40 s
	//   /user/editor           6 s
}

// What changed in a hierarchy between two polls: one group appeared, one
// moved to another parent (with its children), one became a root of its
// own, one went away. A moved group is reported as moved, not as removed
// and added again.
func ExampleDiff() {
	t := tree.New[string]()
	for _, e := range [][2]string{
		{"/", "/"}, // a root is its own parent
		{"a", "/"}, {"a1", "a"}, {"a2", "a"}, {"b", "/"}, {"b1", "b"},
	} {
		if err := t.Add(e[0], e[1]); err != nil {
			panic(err)
		}
	}
	before := t.Snapshot()

	_ = t.Add("c", "/")
	_ = t.Move("a2", "b")
	_ = t.Move("b1", "b1")
	_ = t.Remove("a1")
	err := t.Move("a", "a2") // a2 is now under b, not a: allowed
	fmt.Println("move a under a2:", err)
	err = t.Move("b", "a") // a is under a2, which is under b: a cycle
	fmt.Println("move b under a refused as a cycle:", errors.Is(err, tree.ErrCycle))

	c := tree.Diff(before, t.Snapshot())
	fmt.Println("added:  ", c.Added)
	fmt.Println("removed:", c.Removed)
	for _, m := range c.Moved {
		fmt.Printf("moved:   %s from %s to %s\n", m.ID, m.From, m.To)
	}
	// Output:
	// move a under a2: <nil>
	// move b under a refused as a cycle: true
	// added:   [c]
	// removed: [a1]
	// moved:   a from / to a2
	// moved:   a2 from a to b
	// moved:   b1 from b to b1
}
