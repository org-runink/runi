// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

// Every name, identity and certificate in these tests is invented. The DNS
// names live under example.test and the addresses are in the ranges reserved
// for documentation, built at run time rather than written out.

var t0 = time.Date(2031, 3, 14, 9, 26, 53, 589793238, time.UTC)

var (
	docIPv4 = net.IPv4(192, 0, 2, 10).String()
	docIPv6 = net.IP{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 7}.String()
)

// universe is every name a property test may ask for or allow.
var universe = []string{
	"api.example.test",
	"db.example.test",
	"cache.example.test",
	"*.example.test",
	docIPv4,
	docIPv6,
	"spiffe://example.test/workload/api",
	"spiffe://example.test/workload/db",
}

// fakeClock moves only when told to. armed receives once per After call, so a
// test can wait until the renewal loop is parked before moving time.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
	armed   chan struct{}
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock(t time.Time) *fakeClock {
	return &fakeClock{now: t, armed: make(chan struct{}, 4096)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- f.now
	} else {
		f.waiters = append(f.waiters, waiter{at: f.now.Add(d), ch: ch})
	}
	f.armed <- struct{}{}
	return ch
}

// Advance moves time forward and fires every timer that is now due, returning
// how many fired.
func (f *fakeClock) Advance(d time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	fired := 0
	var kept []waiter
	for _, w := range f.waiters {
		if w.at.After(f.now) {
			kept = append(kept, w)
			continue
		}
		w.ch <- f.now
		fired++
	}
	f.waiters = kept
	return fired
}

type testCA struct {
	key  *ecdsa.PrivateKey
	cert *x509.Certificate
}

// newCA makes a self-signed CA valid from notBefore to notAfter.
func newCA(t testing.TB, cn string, notBefore, notAfter time.Time) testCA {
	t.Helper()
	key := newKey(t)
	cert := selfSigned(t, key, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	})
	return testCA{key: key, cert: cert}
}

// defaultCA is valid for a year either side of t0.
func defaultCA(t testing.TB) testCA {
	return newCA(t, "Example Test CA", t0.Add(-365*24*time.Hour), t0.Add(365*24*time.Hour))
}

func selfSigned(t testing.TB, key *ecdsa.PrivateKey, tmpl *x509.Certificate) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func newKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// csrFor builds a request for names, signed by key.
func csrFor(t testing.TB, key crypto.Signer, names ...string) []byte {
	t.Helper()
	var l sanLists
	for _, n := range names {
		s, err := canonical(n)
		if err != nil {
			t.Fatal(err)
		}
		l.add(s)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:     pkix.Name{CommonName: "requested-subject.example.test"},
		DNSNames:    l.dns,
		IPAddresses: l.ips,
		URIs:        l.uris,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// allow is a Policy that grants names for ttl to everyone.
func allow(ttl time.Duration, names ...string) Policy {
	return PolicyFunc(func(Identity, *x509.CertificateRequest) ([]string, time.Duration, error) {
		return names, ttl, nil
	})
}

func newIssuer(t testing.TB, ca testCA, p Policy, opts Options) *Issuer {
	t.Helper()
	if opts.MaxTTL == 0 {
		opts.MaxTTL = 24 * time.Hour
	}
	i, err := NewIssuer(ca.key, ca.cert, p, opts)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func parse(t testing.TB, der []byte) *x509.Certificate {
	t.Helper()
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// failingSigner has the CA's public key but cannot sign.
type failingSigner struct{ pub crypto.PublicKey }

func (f failingSigner) Public() crypto.PublicKey { return f.pub }

func (f failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errSign
}

var errSign = errors.New("signer unavailable")

// oddSigner's public key is not a type the standard library knows.
type oddSigner struct{}

func (oddSigner) Public() crypto.PublicKey { return "not a key" }

func (oddSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errSign
}

// errReader fails every read.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }
