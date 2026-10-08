// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// build seals n synthetic records with h.
func build(h Hasher, n int) []Link {
	var out []Link
	prev := ""
	for i := 0; i < n; i++ {
		l := Seal(h, prev, []byte(fmt.Sprintf(`{"seq":%d,"note":"synthetic record %d"}`, i+1, i+1)))
		out = append(out, l)
		prev = l.Hash
	}
	return out
}

func faultOf(t *testing.T, err error) *Break {
	t.Helper()
	var b *Break
	if !errors.As(err, &b) {
		t.Fatalf("err = %v, want a *Break", err)
	}
	return b
}

func TestAnIntactChainVerifiesToItsHead(t *testing.T) {
	links := build(Bound, 5)
	head, err := Verify(Bound, links)
	if err != nil || head != links[4].Hash {
		t.Fatalf("head %q, err %v", head, err)
	}
	if head, err := Verify(Bound, nil); err != nil || head != "" {
		t.Fatalf("empty chain: %q, %v", head, err)
	}
}

func TestAnEditedBodyIsAltered(t *testing.T) {
	links := build(Bound, 4)
	links[2].Body = []byte(`{"seq":3,"note":"edited"}`)
	if b := faultOf(t, mustFail(Verify(Bound, links))); b.Index != 2 || b.Fault != Altered {
		t.Fatalf("%+v", b)
	}
}

func TestAnEditThatResealsItselfBreaksTheNextLink(t *testing.T) {
	links := build(Bound, 4)
	links[1] = Seal(Bound, links[1].Prev, []byte(`{"seq":2,"note":"edited and resealed"}`))
	if b := faultOf(t, mustFail(Verify(Bound, links))); b.Index != 2 || b.Fault != Replaced {
		t.Fatalf("%+v: the next link was sealed after the original, which is gone", b)
	}
}

func TestSwappedRecordsAreReordered(t *testing.T) {
	links := build(Bound, 5)
	links[1], links[2] = links[2], links[1]
	if b := faultOf(t, mustFail(Verify(Bound, links))); b.Index != 1 || b.Fault != Reordered {
		t.Fatalf("%+v", b)
	}
}

func TestARemovedRecordIsReplaced(t *testing.T) {
	links := build(Bound, 5)
	links = append(links[:2], links[3:]...)
	if b := faultOf(t, mustFail(Verify(Bound, links))); b.Index != 2 || b.Fault != Replaced {
		t.Fatalf("%+v", b)
	}
}

func TestAnInsertedForeignRecordIsReplaced(t *testing.T) {
	links := build(Bound, 3)
	foreign := Seal(Bound, "not a hash in this chain", []byte(`{"note":"foreign"}`))
	links = append(links[:1], append([]Link{foreign}, links[1:]...)...)
	if b := faultOf(t, mustFail(Verify(Bound, links))); b.Index != 1 || b.Fault != Replaced {
		t.Fatalf("%+v", b)
	}
}

func TestTheFirstLinkMustFollowTheRoot(t *testing.T) {
	links := build(Bound, 4)
	// links[1] was sealed after links[0], which is not in the slice: verified
	// as a whole chain from "", it does not follow the root.
	if b := faultOf(t, mustFail(Verify(Bound, links[1:]))); b.Index != 0 || b.Fault != Replaced {
		t.Fatalf("%+v", b)
	}
	head, err := VerifyFrom(Bound, links[0].Hash, links[1:])
	if err != nil || head != links[3].Hash {
		t.Fatalf("a segment verified from its root: %q, %v", head, err)
	}
}

func TestATruncatedHeadStillVerifies(t *testing.T) {
	// Documented limit: removing the newest records is invisible without a
	// head kept elsewhere.
	links := build(Bound, 5)
	head, err := Verify(Bound, links[:3])
	if err != nil {
		t.Fatal(err)
	}
	if head == links[4].Hash {
		t.Fatal("the truncated chain reports the original head")
	}
}

func TestBoundBindsThePreviousHash(t *testing.T) {
	body := []byte("same body")
	if Bound("a", body) == Bound("b", body) {
		t.Fatal("Bound ignores prev")
	}
	if BodyOnly("a", body) != BodyOnly("b", body) {
		t.Fatal("BodyOnly depends on prev")
	}
	if Bound("", body) == BodyOnly("", body) {
		t.Fatal("Bound is not domain-separated from a plain sha256")
	}
}

func TestBodyOnlyWorksWhenTheBodyCarriesThePrevHash(t *testing.T) {
	var links []Link
	prev := ""
	for i := 1; i <= 3; i++ {
		body := []byte(fmt.Sprintf(`{"seq":%d,"prev_hash":%q}`, i, prev))
		l := Seal(BodyOnly, prev, body)
		links = append(links, l)
		prev = l.Hash
	}
	if _, err := Verify(BodyOnly, links); err != nil {
		t.Fatal(err)
	}
	links[0], links[1] = links[1], links[0]
	if b := faultOf(t, mustFail(Verify(BodyOnly, links))); b.Fault != Reordered {
		t.Fatalf("%+v", b)
	}
}

func TestBreakAndFaultSayWhatHappened(t *testing.T) {
	for f, want := range map[Fault]string{Altered: "altered", Reordered: "out of order", Replaced: "replaced or removed"} {
		if msg := (&Break{Index: 3, Fault: f}).Error(); !strings.Contains(msg, want) || !strings.Contains(msg, "link 3") {
			t.Errorf("%v: %q", f, msg)
		}
	}
	for f, want := range map[Fault]string{Altered: "altered", Reordered: "reordered", Replaced: "replaced", Fault(9): "Fault(9)"} {
		if f.String() != want {
			t.Errorf("%d.String() = %q", int(f), f.String())
		}
	}
}

func mustFail(_ string, err error) error { return err }
