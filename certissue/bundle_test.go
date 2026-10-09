// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"context"
	"crypto/x509"
	"testing"
	"time"
)

func verifies(leaf *x509.Certificate, pool *x509.CertPool) bool {
	_, err := leaf.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: t0, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return err == nil
}

func TestBundle(t *testing.T) {
	oldCA := newCA(t, "Example Test CA 1", t0.Add(-time.Hour), t0.Add(48*time.Hour))
	nextCA := newCA(t, "Example Test CA 2", t0.Add(-time.Hour), t0.Add(48*time.Hour))
	leafUnder := func(ca testCA) *x509.Certificate {
		iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(t0)})
		der, _, err := iss.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return parse(t, der)
	}
	oldLeaf, newLeaf := leafUnder(oldCA), leafUnder(nextCA)

	var b Bundle // the zero value is ready
	if verifies(oldLeaf, b.Pool()) {
		t.Fatal("an empty bundle trusted something")
	}
	b.Add(nil)
	b.Retire(nil)
	b.Retire(oldCA.cert) // not there: no effect
	b.Add(oldCA.cert)
	b.Add(oldCA.cert) // twice: still one
	if len(b.cas) != 1 {
		t.Fatalf("bundle holds %d certificates after adding one twice", len(b.cas))
	}
	before := b.Pool()

	b.Add(nextCA.cert)
	both := b.Pool()
	if !verifies(oldLeaf, both) || !verifies(newLeaf, both) {
		t.Fatal("during rotation a leaf under either CA must verify")
	}
	if verifies(newLeaf, before) {
		t.Fatal("a pool changed after it was returned")
	}

	b.Retire(oldCA.cert)
	after := b.Pool()
	if verifies(oldLeaf, after) {
		t.Fatal("a leaf under a retired CA still verifies")
	}
	if !verifies(newLeaf, after) {
		t.Fatal("retiring the old CA broke the new one")
	}
	if !verifies(oldLeaf, both) {
		t.Fatal("retiring reached into a pool already returned")
	}
}
