package tree

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

const unit = 0x1p-53 // u, the unit roundoff of float64

// ledger follows, beside a tree, what the accuracy bound of Set needs: for
// every node the number of terms its compensated sum has taken and the sum of
// their magnitudes. It also keeps a naive incremental sum, s += v − old, the
// obvious implementation, to show what compensation buys.
type ledger struct {
	terms []float64 // n, as a float64
	mag   []float64 // Σ|xᵢ|
	naive []float64
}

func newLedger(n int) *ledger {
	return &ledger{make([]float64, n), make([]float64, n), make([]float64, n)}
}

// set applies Set(id, v) to the tree and mirrors it in the ledger, given the
// parent of every node (−1 for the root).
func (l *ledger) set(t testing.TB, tr *Tree[int], parent []int, id int, v float64) {
	old := tr.Own(id)
	must(t, tr.Set(id, v))
	for u := id; u >= 0; u = parent[u] {
		l.terms[u] += 2
		l.mag[u] += math.Abs(v) + math.Abs(old)
		l.naive[u] += v - old
	}
}

// bound is the error bound Set documents for a node whose exact sum is s:
// u·|S| + γ²·Σ|xᵢ|, γ = n·u/(1−n·u) (Ogita, Rump and Oishi 2005, Prop. 4.5).
func (l *ledger) bound(u int, exact float64) float64 {
	g := l.terms[u] * unit / (1 - l.terms[u]*unit)
	return unit*math.Abs(exact) + g*g*l.mag[u]
}

// exactSums is the reference: every subtree sum of the current own values,
// computed in big.Float at a precision where the additions are exact (2200
// bits span every float64 from the smallest subnormal, 2⁻¹⁰⁷⁴, to beyond the
// 2⁹⁶⁰ Set allows, with room for the carries of thousands of terms),
// returned as the exact sum, rounded once to float64, and the exact errors
// of got and naive against it.
func exactSums(tr *Tree[int], parent []int, got, naive []float64) (exact, errGot, errNaive []float64) {
	n := len(parent)
	sums := make([]*big.Float, n)
	for i := range sums {
		sums[i] = new(big.Float).SetPrec(2200)
	}
	x := new(big.Float).SetPrec(2200)
	for id := range n {
		x.SetFloat64(tr.Own(id))
		for u := id; u >= 0; u = parent[u] {
			sums[u].Add(sums[u], x)
		}
	}
	exact, errGot, errNaive = make([]float64, n), make([]float64, n), make([]float64, n)
	d := new(big.Float).SetPrec(2200)
	for u := range n {
		exact[u], _ = sums[u].Float64()
		e, _ := d.Sub(x.SetFloat64(got[u]), sums[u]).Float64()
		errGot[u] = math.Abs(e)
		e, _ = d.Sub(x.SetFloat64(naive[u]), sums[u]).Float64()
		errNaive[u] = math.Abs(e)
	}
	return exact, errGot, errNaive
}

// randomForest returns a tree of n nodes keyed 0..n−1, node i under a random
// earlier node, and the parent array (−1 for root 0).
func randomForest(t testing.TB, r *rand.Rand, n int) (*Tree[int], []int) {
	tr := New[int]()
	parent := make([]int, n)
	parent[0] = -1
	must(t, tr.AddRoot(0))
	for i := 1; i < n; i++ {
		// Bias towards recent nodes so the tree is deep as well as wide.
		parent[i] = max(0, i-1-r.IntN(min(i, 4)))
		if r.IntN(4) == 0 {
			parent[i] = r.IntN(i)
		}
		must(t, tr.Add(i, parent[i]))
	}
	return tr, parent
}

// After 10⁶ random updates, every subtree sum is within the documented bound
// of the exact sum, recomputed in big.Float from the current own values.
// Values span fifteen orders of magnitude with random signs, so every
// update cancels some of what came before. The measured error is logged in
// units of u·|S|: under one means the incremental sum is as good as one
// correctly rounded addition at the end. The naive sum is measured too; it
// is not held to anything here (TestNaiveSumFails is where it must fail).
// Under the race detector the run is a quarter as long: the detector slows
// it tenfold and has nothing to find in one goroutine.
func TestSumDrift(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	const nodes, updates = 300, 1_000_000
	tr, parent := randomForest(t, r, nodes)
	l := newLedger(nodes)
	for range updates / raceScale {
		v := 0.0
		if r.IntN(10) != 0 {
			v = math.Pow(10, -3+15*r.Float64())
			if r.IntN(2) == 0 {
				v = -v
			}
		}
		l.set(t, tr, parent, r.IntN(nodes), v)
	}
	got := make([]float64, nodes)
	for u := range got {
		got[u] = tr.Sum(u)
	}
	exact, errGot, errNaive := exactSums(tr, parent, got, l.naive)
	var worst, worstNaive float64
	for u := range nodes {
		if errGot[u] > l.bound(u, exact[u]) {
			t.Errorf("node %d: error %g exceeds the bound %g (exact %g, %v terms)", u, errGot[u], l.bound(u, exact[u]), exact[u], l.terms[u])
		}
		if exact[u] != 0 {
			worst = max(worst, errGot[u]/(unit*math.Abs(exact[u])))
			worstNaive = max(worstNaive, errNaive[u]/(unit*math.Abs(exact[u])))
		}
	}
	// The doc claims more than the bound: the error measured here stays
	// below one rounding of the exact sum at every node.
	if worst >= 1 {
		t.Errorf("worst error %.3g·u|S|, the doc says below 1", worst)
	}
	deepest := 0
	for u := range nodes {
		d, _ := tr.Depth(u)
		deepest = max(deepest, d)
	}
	if deepest < 10 {
		t.Errorf("tree too shallow to mean much: depth %d", deepest)
	}
	t.Logf("after %d updates, depth %d, root over %v terms: compensated error ≤ %.3g·u|S|, naive ≤ %.3g·u|S| (root bound %.3g·u|S|)",
		updates/raceScale, deepest, l.terms[0], worst, worstNaive, l.bound(0, exact[0])/(unit*math.Abs(exact[0])))

	t.Logf("root: exact %.17g, compensated error %.3g, naive error %.3g", exact[0], errGot[0], errNaive[0])

	// Rebuild recomputes from the own values, bottom-up. Each node's sum is
	// then a compensated sum of its own value and its children's (sum, comp)
	// pairs, so its error is at most the same bound over those terms plus
	// the errors the children brought with them. The children of a node in
	// randomForest have larger keys, so a descending pass sees them first.
	tr.Rebuild()
	for u := range got {
		got[u] = tr.Sum(u)
	}
	exact, errGot, _ = exactSums(tr, parent, got, l.naive)
	eb := make([]float64, nodes)
	worst = 0
	for u := nodes - 1; u >= 0; u-- {
		n := &tr.nodes[tr.index[u]]
		mag, carried := math.Abs(n.own), 0.0
		for _, c := range n.kids {
			mag += math.Abs(tr.nodes[c].sum) + math.Abs(tr.nodes[c].comp)
			carried += eb[tr.nodes[c].id]
		}
		k := float64(1 + 2*len(n.kids))
		g := k * unit / (1 - k*unit)
		eb[u] = unit*math.Abs(exact[u]) + g*g*mag + carried
		if errGot[u] > eb[u] {
			t.Errorf("after Rebuild node %d: error %g exceeds %g", u, errGot[u], eb[u])
		}
		if exact[u] != 0 {
			worst = max(worst, errGot[u]/(unit*math.Abs(exact[u])))
		}
	}
	t.Logf("after Rebuild: error ≤ %.3g·u|S|", worst)
}

// The test that shows TestSumDrift can fail: on data built to cancel —
// a node set to a huge value while its siblings' small values are set, then
// set back to zero — a naive running sum s += v − old ends far outside the
// bound, while the compensated sum stays inside it. If the bound were loose
// enough to pass the naive sum, this test would say so.
func TestNaiveSumFails(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	const nodes = 50
	tr, parent := randomForest(t, r, nodes)
	l := newLedger(nodes)
	for range 20_000 {
		big := r.IntN(nodes)
		l.set(t, tr, parent, big, 1e16*(1+r.Float64()))
		for range 3 {
			l.set(t, tr, parent, r.IntN(nodes), r.Float64()) // lost in the noise of 1e16
		}
		l.set(t, tr, parent, big, 0)
	}
	got := make([]float64, nodes)
	for u := range got {
		got[u] = tr.Sum(u)
	}
	exact, errGot, errNaive := exactSums(tr, parent, got, l.naive)
	if errGot[0] > l.bound(0, exact[0]) {
		t.Errorf("compensated: error %g exceeds the bound %g", errGot[0], l.bound(0, exact[0]))
	}
	if errNaive[0] <= l.bound(0, exact[0]) {
		t.Fatalf("naive: error %g within the bound %g; the bound cannot tell good from bad", errNaive[0], l.bound(0, exact[0]))
	}
	t.Logf("root: exact %.6g, compensated error %.3g, naive error %.3g, bound %.3g", exact[0], errGot[0], errNaive[0], l.bound(0, exact[0]))
}
