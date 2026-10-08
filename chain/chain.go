// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Link is one sealed record: the caller's bytes, the hash of the link before
// it ("" for the first), and its own hash.
type Link struct {
	Prev string
	Hash string
	Body []byte
}

// A Hasher computes a link's hash from the previous hash and the body.
type Hasher func(prev string, body []byte) string

// domain separates chain's Bound hashes from any other sha256 over the same
// bytes.
const domain = "runi/chain v1\x00"

// Bound is hex(sha256(domain, prev, 0x00, body)): the previous hash is part of
// what is sealed. Use it for new chains.
func Bound(prev string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte(prev))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// BodyOnly is hex(sha256(body)). Use it only when body already carries the
// previous hash; see the package documentation.
func BodyOnly(_ string, body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Seal makes the link that follows prev.
func Seal(h Hasher, prev string, body []byte) Link {
	return Link{Prev: prev, Hash: h(prev, body), Body: body}
}

// Fault is how a chain broke.
type Fault int

const (
	// Altered: the body does not match the link's hash.
	Altered Fault = iota + 1
	// Reordered: the link names another link of this chain as the one
	// before it.
	Reordered
	// Replaced: the link names a hash that is not in this chain as the one
	// before it.
	Replaced
)

func (f Fault) String() string {
	switch f {
	case Altered:
		return "altered"
	case Reordered:
		return "reordered"
	case Replaced:
		return "replaced"
	}
	return fmt.Sprintf("Fault(%d)", int(f))
}

// Break is Verify's error: the first link that does not hold, counted from 0,
// and how.
type Break struct {
	Index int
	Fault Fault
}

func (b *Break) Error() string {
	switch b.Fault {
	case Altered:
		return fmt.Sprintf("chain: link %d was altered: its body does not match its hash", b.Index)
	case Reordered:
		return fmt.Sprintf("chain: link %d is out of order: the link it was sealed after is elsewhere in the chain", b.Index)
	default:
		return fmt.Sprintf("chain: link %d does not follow the link before it: the record it was sealed after was replaced or removed", b.Index)
	}
}

// Verify checks a whole chain, whose first link must follow "". It returns
// the head hash ("" for an empty chain) or a *Break at the first link that
// does not hold.
func Verify(h Hasher, links []Link) (string, error) {
	return VerifyFrom(h, "", links)
}

// VerifyFrom checks links that continue a chain whose last known hash is
// root: a segment, or a chain checked against a head kept elsewhere.
func VerifyFrom(h Hasher, root string, links []Link) (string, error) {
	prev := root
	for i, l := range links {
		if h(l.Prev, l.Body) != l.Hash {
			return "", &Break{Index: i, Fault: Altered}
		}
		if l.Prev != prev {
			return "", &Break{Index: i, Fault: misplaced(links, i, l.Prev)}
		}
		prev = l.Hash
	}
	return prev, nil
}

// misplaced tells a moved link from a swapped one: does the hash it was
// sealed after belong to some other link of this chain?
func misplaced(links []Link, at int, prev string) Fault {
	for j, o := range links {
		if j != at && o.Hash == prev {
			return Reordered
		}
	}
	return Replaced
}
