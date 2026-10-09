package chain

import (
	"math/rand/v2"
	"testing"
)

// The example tests in this package each tamper with a chain one agreed way.
// These properties say the same thing for EVERY tamper of that shape, over
// thousands of generated chains: the point of a hash chain is that there is no
// edit it misses, and a fixed list of edits cannot establish that.

func randChain(r *rand.Rand, n int) []Link {
	links := make([]Link, n)
	prev := ""
	for i := range links {
		body := make([]byte, 1+r.IntN(40))
		for j := range body {
			body[j] = byte(r.IntN(256))
		}
		links[i] = Seal(Bound, prev, body)
		prev = links[i].Hash
	}
	return links
}

func clone(links []Link) []Link {
	out := make([]Link, len(links))
	for i, l := range links {
		body := make([]byte, len(l.Body))
		copy(body, l.Body)
		out[i] = Link{Prev: l.Prev, Hash: l.Hash, Body: body}
	}
	return out
}

// An honestly built chain always verifies, at every length, for every body.
func TestPropertyASealedChainAlwaysVerifies(t *testing.T) {
	r := rand.New(rand.NewPCG(21, 22))
	for i := 0; i < 2000; i++ {
		n := 1 + r.IntN(30)
		links := randChain(r, n)
		head, err := Verify(Bound, links)
		if err != nil {
			t.Fatalf("n=%d: honest chain failed: %v", n, err)
		}
		if head != links[n-1].Hash {
			t.Fatalf("n=%d: head %q is not the last hash %q", n, head, links[n-1].Hash)
		}
	}
}

// Flipping ANY single bit of ANY body is caught, and reported against the link
// that was edited rather than some later one.
func TestPropertyEverySingleBitFlipIsCaught(t *testing.T) {
	r := rand.New(rand.NewPCG(23, 24))
	for i := 0; i < 2000; i++ {
		n := 1 + r.IntN(12)
		links := randChain(r, n)
		bad := clone(links)
		at := r.IntN(n)
		pos := r.IntN(len(bad[at].Body))
		bad[at].Body[pos] ^= 1 << r.IntN(8)

		_, err := Verify(Bound, bad)
		if err == nil {
			t.Fatalf("n=%d: a flipped bit in link %d verified", n, at)
		}
		br, ok := err.(*Break)
		if !ok {
			t.Fatalf("want *Break, got %T", err)
		}
		if br.Index != at {
			t.Fatalf("flipped link %d, blamed link %d", at, br.Index)
		}
		if br.Fault != Altered {
			t.Fatalf("flipped a body, got fault %v, want Altered", br.Fault)
		}
	}
}

// Dropping any one link from the middle is caught. This is the property that
// matters for an audit log: a record that was removed must not be a record
// that was never there.
func TestPropertyEveryDeletionIsCaught(t *testing.T) {
	r := rand.New(rand.NewPCG(25, 26))
	for i := 0; i < 2000; i++ {
		n := 2 + r.IntN(20)
		links := randChain(r, n)
		at := r.IntN(n - 1) // dropping the LAST link is a legal truncation
		bad := append(clone(links[:at]), clone(links[at+1:])...)
		if _, err := Verify(Bound, bad); err == nil {
			t.Fatalf("n=%d: dropping link %d verified", n, at)
		}
	}
}

// Swapping any two distinct links is caught.
func TestPropertyEverySwapIsCaught(t *testing.T) {
	r := rand.New(rand.NewPCG(27, 28))
	for i := 0; i < 2000; i++ {
		n := 2 + r.IntN(20)
		links := randChain(r, n)
		a := r.IntN(n)
		b := r.IntN(n)
		if a == b {
			continue
		}
		bad := clone(links)
		bad[a], bad[b] = bad[b], bad[a]
		if _, err := Verify(Bound, bad); err == nil {
			t.Fatalf("n=%d: swapping %d and %d verified", n, a, b)
		}
	}
}

// Every PREFIX verifies. A chain read while it is still being appended to is
// not a broken chain, and a verifier that rejected one would make every
// concurrent reader a false alarm.
func TestPropertyEveryPrefixVerifies(t *testing.T) {
	r := rand.New(rand.NewPCG(29, 30))
	for i := 0; i < 1000; i++ {
		n := 1 + r.IntN(25)
		links := randChain(r, n)
		for k := 0; k <= n; k++ {
			if _, err := Verify(Bound, links[:k]); err != nil {
				t.Fatalf("n=%d: prefix of %d failed: %v", n, k, err)
			}
		}
	}
}

// VerifyFrom with the true root is the same answer as Verify, and VerifyFrom
// with ANY other root rejects the first link. A chain is only evidence if it
// is anchored.
func TestPropertyVerifyFromRejectsAnyOtherRoot(t *testing.T) {
	r := rand.New(rand.NewPCG(31, 32))
	for i := 0; i < 1000; i++ {
		n := 1 + r.IntN(15)
		links := randChain(r, n)
		if _, err := VerifyFrom(Bound, "", links); err != nil {
			t.Fatalf("the true root was rejected: %v", err)
		}
		wrong := Bound("", []byte{byte(r.IntN(256))})
		if _, err := VerifyFrom(Bound, wrong, links); err == nil {
			t.Fatalf("n=%d: a foreign root verified", n)
		}
	}
}
