// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"crypto/rand"
	"crypto/x509"
	"errors"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"
)

// signedLeaf has ca sign tmpl directly, bypassing the Issuer, so the peer
// check can be shown certificates the Issuer would never produce.
func signedLeaf(t *testing.T, ca testCA, tmpl *x509.Certificate) [][]*x509.Certificate {
	t.Helper()
	tmpl.SerialNumber = big.NewInt(99)
	tmpl.NotBefore = t0.Add(-time.Minute)
	tmpl.NotAfter = t0.Add(time.Hour)
	tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	key := newKey(t)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, key.Public(), ca.key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	chains, err := parse(t, der).Verify(x509.VerifyOptions{Roots: roots, CurrentTime: t0, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	if err != nil {
		t.Fatalf("the test certificate does not chain: %v", err)
	}
	return chains
}

func TestVerifyPeerIdentity(t *testing.T) {
	ca := defaultCA(t)
	uri, err := url.Parse("spiffe://example.test/workload/db")
	if err != nil {
		t.Fatal(err)
	}
	check := VerifyPeerIdentity([]string{"DB.example.test", "spiffe://example.test/workload/db", ":unreadable"})

	if err := check(nil, signedLeaf(t, ca, &x509.Certificate{DNSNames: []string{"db.example.test"}, URIs: []*url.URL{uri}})); err != nil {
		t.Errorf("every name allowed: %v", err)
	}
	refused := map[string]*x509.Certificate{
		"one name outside the list": {DNSNames: []string{"db.example.test", "api.example.test"}},
		"no names at all":           {},
		"an e-mail address":         {DNSNames: []string{"db.example.test"}, EmailAddresses: []string{"db@example.test"}},
		"an IP not on the list":     {IPAddresses: []net.IP{net.ParseIP(docIPv4)}},
	}
	for name, tmpl := range refused {
		if err := check(nil, signedLeaf(t, ca, tmpl)); !errors.Is(err, ErrPeerName) {
			t.Errorf("%s: err = %v, want ErrPeerName", name, err)
		}
	}
	for _, chains := range [][][]*x509.Certificate{nil, {}, {{}}} {
		if err := check(nil, chains); !errors.Is(err, ErrUnverified) {
			t.Errorf("chains %v: err = %v, want ErrUnverified", chains, err)
		}
	}
}
