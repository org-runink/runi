package tree

import (
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"testing"
)

// decode reads three bytes as one operation on keys 0..15: the kind, the
// node, and the other node or the value. Values are quarters from −32 to
// 31.75, so every sum is exact and the model's must match to the bit; one
// kind sets a value Set must refuse (NaN, ±Inf, beyond 2^960) or −0, and
// one calls Rebuild, which must change no answer.
func decode(b []byte) (o op, rebuild bool) {
	o = op{a: int(b[1]) % 16, b: int(b[2]) % 16}
	switch k := int(b[0]) % 7; k {
	case 5:
		o.kind = opSet
		o.v = [...]float64{math.NaN(), math.Inf(1), math.Inf(-1), 0x1p961, math.Copysign(0, -1)}[int(b[2])%5]
	case 6:
		rebuild = true
	default:
		o.kind = k
		o.v = float64(int8(b[2])) / 4
	}
	return o, rebuild
}

// Any sequence of operations leaves a tree whose every answer — Parent,
// Depth, Children, Path, Sum, Own, Max, TopK at every node with several k —
// agrees with a recomputation from a plain parent map, whose internal
// invariants hold (verify: a forest, heaps in order), and whose every error
// is the one the model predicts, so a move under a descendant is always
// refused. The structure halfway through and at the end is diffed, and the
// Changes applied to a copy of the first must give the second.
func FuzzTree(f *testing.F) {
	f.Add([]byte{0, 1, 1, 0, 2, 1, 0, 3, 2, 3, 3, 8, 1, 1, 3})                   // a chain, a value, a cycle refused
	f.Add([]byte{0, 0, 0, 0, 1, 0, 0, 2, 0, 3, 1, 4, 3, 2, 4, 2, 0, 0, 4, 0, 0}) // ties at 0 and 1, Remove of an inner node
	f.Add([]byte{0, 5, 5, 0, 6, 5, 5, 6, 0, 5, 6, 1, 5, 6, 4, 6, 0, 0, 1, 6, 6}) // −0, NaN, Rebuild, make a root
	f.Add([]byte{0, 9, 9, 0, 1, 9, 0, 2, 9, 1, 9, 2, 2, 2, 0, 1, 1, 1, 4, 9, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		// 200 operations reach every state 16 keys have; longer inputs
		// only slow the fuzzer, which then spends its time minimising.
		b = b[:min(len(b), 600)]
		tr, m := New[int](), newModel(16)
		var a Snapshot[int]
		half := len(b) / 6 * 3
		for i := 0; i+3 <= len(b); i += 3 {
			if i == half {
				a = tr.Snapshot()
			}
			o, rebuild := decode(b[i:])
			if rebuild {
				tr.Rebuild()
			} else {
				step(t, tr, m, o)
			}
			check(t, tr, m, nil, []int{1, 2, 3, 16}, true)
		}
		end := tr.Snapshot()
		re := fromSnapshot(t, a)
		apply(t, re, Diff(a, end), end)
		if !slices.Equal(re.Snapshot().edges, end.edges) {
			t.Fatalf("a + Diff(a, b) is not b")
		}
	})
}

// Any sequence of float64 bit patterns given to Set on a fixed tree — NaNs,
// infinities, subnormals, values near the 2^960 limit, wild cancellation —
// leaves every subtree sum finite and within the bound Set documents of the
// exact sum of the own values, recomputed in big.Float. A refused value
// changes nothing; an accepted one is stored exactly (−0 as +0).
func FuzzSet(f *testing.F) {
	f.Add(bytesOf(1e16, 1, -1e16, 1, 0, 3))
	f.Add(bytesOf(0x1p960, 7, 0x1p960, 6, -0x1p960, 5, 0x1p960, 4))
	f.Add(bytesOf(math.NaN(), 0, math.Inf(-1), 1, 0x1p961, 2, math.SmallestNonzeroFloat64, 3))
	f.Add(bytesOf(math.Copysign(0, -1), 7, 1e-300, 7, -1e300, 6, 1e300, 1))
	f.Fuzz(func(t *testing.T, b []byte) {
		// 0 ← 1 ← 2 ← 3, and 0 ← 4, 1 ← 5, 2 ← 6, 2 ← 7: depth 3.
		parent := []int{-1, 0, 1, 2, 0, 1, 2, 2}
		tr := New[int]()
		must(t, tr.AddRoot(0))
		for i := 1; i < len(parent); i++ {
			must(t, tr.Add(i, parent[i]))
		}
		l := newLedger(len(parent))
		xs := floats(b[:min(len(b), 16*256)])
		for i := 0; i+2 <= len(xs); i += 2 {
			// Each pair is (value, node): a node given as 0..7 is that node,
			// any other bit pattern picks one by its low bits.
			v, id := xs[i], int(math.Float64bits(xs[i+1])%8)
			if x := xs[i+1]; x >= 0 && x < 8 {
				id = int(x)
			}
			if !finite(v) || math.Abs(v) > maxValue {
				own, sum := tr.Own(id), tr.Sum(0)
				if err := tr.Set(id, v); !errors.Is(err, ErrInvalid) || tr.Own(id) != own || tr.Sum(0) != sum {
					t.Fatalf("Set(%d, %v) = %v, own %v → %v, root sum %v → %v", id, v, err, own, tr.Own(id), sum, tr.Sum(0))
				}
				continue
			}
			l.set(t, tr, parent, id, v)
			if got := tr.Own(id); got != v || got == 0 && math.Signbit(got) {
				t.Fatalf("Own(%d) = %v after Set %v", id, got, v)
			}
		}
		got := make([]float64, len(parent))
		for u := range got {
			if got[u] = tr.Sum(u); !finite(got[u]) {
				t.Fatalf("Sum(%d) = %v", u, got[u])
			}
		}
		exact, errGot, _ := exactSums(tr, parent, got, l.naive)
		for u := range got {
			if errGot[u] > l.bound(u, exact[u]) {
				t.Fatalf("node %d: error %g exceeds the bound %g (exact %g)", u, errGot[u], l.bound(u, exact[u]), exact[u])
			}
		}
	})
}

// floats decodes b as little-endian float64s, so the fuzzer can reach every
// bit pattern.
func floats(b []byte) []float64 {
	xs := make([]float64, len(b)/8)
	for i := range xs {
		xs[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[8*i:]))
	}
	return xs
}

func bytesOf(xs ...float64) []byte {
	b := make([]byte, 8*len(xs))
	for i, x := range xs {
		binary.LittleEndian.PutUint64(b[8*i:], math.Float64bits(x))
	}
	return b
}
