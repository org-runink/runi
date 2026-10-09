// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestIssueHappyPath(t *testing.T) {
	ca := defaultCA(t)
	clock := newFakeClock(t0)
	names := []string{"api.example.test", docIPv4, "spiffe://example.test/workload/api"}
	iss := newIssuer(t, ca, allow(time.Hour, names...), Options{Clock: clock})

	der, chain, err := iss.Issue(context.Background(), Request{
		CSR:      csrFor(t, newKey(t), names...),
		Identity: Identity{Subject: "api"},
		TTL:      2 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	leaf := parse(t, der)
	if got := presented(leaf); !slices.Equal(got, []string{"dns:api.example.test", "ip:" + docIPv4, "uri:spiffe://example.test/workload/api"}) {
		t.Errorf("names = %v", got)
	}
	if leaf.Subject.String() != "" {
		t.Errorf("subject = %q, want empty: the request's subject is not copied", leaf.Subject)
	}
	if leaf.IsCA {
		t.Error("leaf is a CA")
	}
	if !leaf.NotBefore.Equal(t0.Truncate(time.Second)) || !leaf.NotAfter.Equal(t0.Truncate(time.Second).Add(time.Hour)) {
		t.Errorf("validity %v to %v", leaf.NotBefore, leaf.NotAfter)
	}
	if !slices.Equal(leaf.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}) {
		t.Errorf("ext key usage = %v", leaf.ExtKeyUsage)
	}
	if len(chain) != 1 || !bytes.Equal(chain[0], ca.cert.Raw) {
		t.Fatal("chain is not the CA certificate")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: t0, DNSName: "api.example.test"}); err != nil {
		t.Errorf("leaf does not verify under its CA: %v", err)
	}
}

func TestNewIssuerRefuses(t *testing.T) {
	ca := defaultCA(t)
	p := allow(time.Hour, "api.example.test")
	notCA := newCA(t, "Example Test Leaf", t0.Add(-time.Hour), t0.Add(time.Hour))
	notCA.cert = selfSigned(t, notCA.key, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		NotBefore:    t0.Add(-time.Hour),
		NotAfter:     t0.Add(time.Hour),
		DNSNames:     []string{"leaf.example.test"},
	})
	noCertSign := newCA(t, "Example Test CA", t0.Add(-time.Hour), t0.Add(time.Hour))
	noCertSign.cert = selfSigned(t, noCertSign.key, &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		NotBefore:             t0.Add(-time.Hour),
		NotAfter:              t0.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
	})
	day := Options{MaxTTL: 24 * time.Hour}
	cases := []struct {
		name   string
		signer crypto.Signer
		ca     *x509.Certificate
		p      Policy
		opts   Options
	}{
		{"nil signer", nil, ca.cert, p, day},
		{"nil CA", ca.key, nil, p, day},
		{"nil policy", ca.key, ca.cert, nil, day},
		{"no MaxTTL", ca.key, ca.cert, p, Options{}},
		{"sub-second MaxTTL", ca.key, ca.cert, p, Options{MaxTTL: 999 * time.Millisecond}},
		{"unparsed CA", ca.key, &x509.Certificate{IsCA: true}, p, day},
		{"not a CA", notCA.key, notCA.cert, p, day},
		{"CA without cert signing", noCertSign.key, noCertSign.cert, p, day},
		{"signer for another key", newKey(t), ca.cert, p, day},
		{"signer of an unknown key type", oddSigner{}, ca.cert, p, day},
	}
	for _, tc := range cases {
		if _, err := NewIssuer(tc.signer, tc.ca, tc.p, tc.opts); err == nil {
			t.Errorf("%s: NewIssuer accepted it", tc.name)
		}
	}
}

// The Issuer keeps its own copy of the CA: changing the caller's certificate
// afterwards changes nothing.
func TestNewIssuerCopiesTheCA(t *testing.T) {
	ca := defaultCA(t)
	iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(t0)})
	ca.cert.NotAfter = t0.Add(-time.Hour)
	ca.cert.IsCA = false
	if _, _, err := iss.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour}); err != nil {
		t.Fatalf("a change to the caller's CA value reached the issuer: %v", err)
	}
}

func TestIssueHonoursCancellation(t *testing.T) {
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(t0)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := iss.Issue(ctx, Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestIssueRefusesGarbage(t *testing.T) {
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(t0)})
	for _, csr := range [][]byte{nil, []byte("not a certificate request"), {0x30, 0x03, 0x02, 0x01, 0x01}} {
		if _, _, err := iss.Issue(context.Background(), Request{CSR: csr, TTL: time.Hour}); !errors.Is(err, ErrInvalidCSR) {
			t.Errorf("csr %x: err = %v, want ErrInvalidCSR", csr, err)
		}
	}
}

func TestIssueKeyTypes(t *testing.T) {
	ca := defaultCA(t)
	p := allow(time.Hour, "api.example.test")
	p521, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, ed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	def := newIssuer(t, ca, p, Options{Clock: newFakeClock(t0)})
	if _, _, err := def.Issue(context.Background(), Request{CSR: csrFor(t, p521, "api.example.test"), TTL: time.Hour}); !errors.Is(err, ErrKeyType) {
		t.Errorf("P-521 under the defaults: err = %v, want ErrKeyType", err)
	}
	if _, _, err := def.Issue(context.Background(), Request{CSR: csrFor(t, ed, "api.example.test"), TTL: time.Hour}); err != nil {
		t.Errorf("Ed25519 under the defaults: %v", err)
	}
	only521 := newIssuer(t, ca, p, Options{Clock: newFakeClock(t0), KeyTypes: []KeyType{ECDSAP521}})
	if _, _, err := only521.Issue(context.Background(), Request{CSR: csrFor(t, p521, "api.example.test"), TTL: time.Hour}); err != nil {
		t.Errorf("P-521 when allowed: %v", err)
	}
	if _, _, err := only521.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour}); !errors.Is(err, ErrKeyType) {
		t.Errorf("P-256 when only P-521 is allowed: err = %v, want ErrKeyType", err)
	}
}

func TestKeyTypeOf(t *testing.T) {
	bits := func(n int) *rsa.PublicKey {
		return &rsa.PublicKey{N: new(big.Int).Lsh(big.NewInt(1), uint(n-1)), E: 65537}
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p224, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		key  interface{}
		want KeyType
	}{
		{&newKey(t).PublicKey, ECDSAP256},
		{&p384.PublicKey, ECDSAP384},
		{&p224.PublicKey, ""},
		{pub, Ed25519},
		{bits(2048), RSA2048},
		{bits(3072), RSA3072},
		{bits(4096), RSA4096},
		{bits(1024), ""},
		{"not a key", ""},
	}
	for i, tc := range cases {
		if got := keyTypeOf(tc.key); got != tc.want {
			t.Errorf("case %d: %q, want %q", i, got, tc.want)
		}
	}
}

var errNotYou = errors.New("not this identity")

func TestIssuePolicyRefusal(t *testing.T) {
	p := PolicyFunc(func(id Identity, _ *x509.CertificateRequest) ([]string, time.Duration, error) {
		if id.Subject != "api" {
			return nil, 0, errNotYou
		}
		return []string{"api.example.test"}, time.Hour, nil
	})
	iss := newIssuer(t, defaultCA(t), p, Options{Clock: newFakeClock(t0)})
	csr := csrFor(t, newKey(t), "api.example.test")
	if _, _, err := iss.Issue(context.Background(), Request{CSR: csr, Identity: Identity{Subject: "db"}, TTL: time.Hour}); !errors.Is(err, errNotYou) {
		t.Errorf("err = %v, want the policy's error", err)
	}
	if _, _, err := iss.Issue(context.Background(), Request{CSR: csr, Identity: Identity{Subject: "api"}, TTL: time.Hour}); err != nil {
		t.Errorf("allowed identity: %v", err)
	}
}

func TestIssueDeniedWhenNothingOverlaps(t *testing.T) {
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, "db.example.test"), Options{Clock: newFakeClock(t0)})
	if _, _, err := iss.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour}); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
}

// A request's subject and e-mail addresses are never issued, even when Policy
// returns a string spelled like one of them.
func TestIssueIgnoresSubjectAndEmail(t *testing.T) {
	key := newKey(t)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:        pkix.Name{CommonName: "requested-subject.example.test"},
		DNSNames:       []string{"API.Example.Test"},
		EmailAddresses: []string{"someone@example.test"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, "api.example.test", "someone@example.test", "requested-subject.example.test"), Options{Clock: newFakeClock(t0)})
	leafDER, _, err := iss.Issue(context.Background(), Request{CSR: der, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	leaf := parse(t, leafDER)
	if !slices.Equal(leaf.DNSNames, []string{"api.example.test"}) || len(leaf.EmailAddresses) != 0 || leaf.Subject.CommonName != "" {
		t.Fatalf("leaf carries DNS %v, e-mail %v, CN %q", leaf.DNSNames, leaf.EmailAddresses, leaf.Subject.CommonName)
	}
}

func TestIssueLifetimeRefusals(t *testing.T) {
	csr := csrFor(t, newKey(t), "api.example.test")
	cases := []struct {
		name      string
		requested time.Duration
		policy    time.Duration
	}{
		{"nothing requested", 0, time.Hour},
		{"negative request", -time.Hour, time.Hour},
		{"under a second", 999 * time.Millisecond, time.Hour},
		{"policy gives none", time.Hour, 0},
	}
	for _, tc := range cases {
		iss := newIssuer(t, defaultCA(t), allow(tc.policy, "api.example.test"), Options{Clock: newFakeClock(t0)})
		if _, _, err := iss.Issue(context.Background(), Request{CSR: csr, TTL: tc.requested}); !errors.Is(err, ErrTTL) {
			t.Errorf("%s: err = %v, want ErrTTL", tc.name, err)
		}
	}
}

func TestIssueOutsideCAValidity(t *testing.T) {
	ca := defaultCA(t)
	csr := csrFor(t, newKey(t), "api.example.test")
	for _, at := range []time.Time{ca.cert.NotBefore.Add(-time.Second), ca.cert.NotAfter, ca.cert.NotAfter.Add(time.Hour)} {
		iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(at)})
		if _, _, err := iss.Issue(context.Background(), Request{CSR: csr, TTL: time.Hour}); !errors.Is(err, ErrCAValidity) {
			t.Errorf("at %v: err = %v, want ErrCAValidity", at, err)
		}
	}
}

func TestIssueNeverOutlivesTheCA(t *testing.T) {
	ca := newCA(t, "Example Test CA", t0.Add(-time.Hour), t0.Add(30*time.Minute))
	iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(t0)})
	der, _, err := iss.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if got := parse(t, der).NotAfter; !got.Equal(ca.cert.NotAfter) {
		t.Fatalf("NotAfter = %v, want the CA's %v", got, ca.cert.NotAfter)
	}
}

func TestIssueSerials(t *testing.T) {
	ca := defaultCA(t)
	csr := csrFor(t, newKey(t), "api.example.test")
	errSerial := errors.New("serial source down")
	bad := map[string]func() (*big.Int, error){
		"error":    func() (*big.Int, error) { return nil, errSerial },
		"nil":      func() (*big.Int, error) { return nil, nil },
		"zero":     func() (*big.Int, error) { return big.NewInt(0), nil },
		"negative": func() (*big.Int, error) { return big.NewInt(-5), nil },
	}
	for name, serial := range bad {
		iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(t0), Serial: serial})
		if _, _, err := iss.Issue(context.Background(), Request{CSR: csr, TTL: time.Hour}); err == nil {
			t.Errorf("%s serial: issued anyway", name)
		}
	}
	iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{
		Clock:  newFakeClock(t0),
		Serial: func() (*big.Int, error) { return big.NewInt(4242), nil },
	})
	der, _, err := iss.Issue(context.Background(), Request{CSR: csr, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if got := parse(t, der).SerialNumber; got.Int64() != 4242 {
		t.Fatalf("serial = %v, want the source's 4242", got)
	}
}

func TestDefaultSerial(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		n, err := randomSerial()
		if err != nil {
			t.Fatal(err)
		}
		if n.Sign() <= 0 || n.Cmp(serialLimit) > 0 {
			t.Fatalf("serial %v out of range", n)
		}
		if seen[n.String()] {
			t.Fatalf("serial %v repeated", n)
		}
		seen[n.String()] = true
	}
	if _, err := serialFrom(errReader{}); err == nil {
		t.Fatal("a failed read produced a serial")
	}
}

func TestIssueSigningFailure(t *testing.T) {
	ca := defaultCA(t)
	iss, err := NewIssuer(failingSigner{pub: ca.key.Public()}, ca.cert, allow(time.Hour, "api.example.test"), Options{MaxTTL: time.Hour, Clock: newFakeClock(t0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := iss.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour}); !errors.Is(err, errSign) {
		t.Fatalf("err = %v, want the signer's error", err)
	}
}

// No formatting verb prints anything but a fixed string, so an Issuer in a
// log line cannot leak the key.
func TestIssuerFormatWithholdsTheKey(t *testing.T) {
	ca := defaultCA(t)
	iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{})
	secrets := []string{ca.key.D.String(), ca.key.D.Text(16), fmt.Sprintf("%x", ca.key.D.Bytes())}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%T %v"} {
		for _, operand := range []interface{}{iss, *iss} {
			out := fmt.Sprintf(verb, operand)
			if verb == "%T %v" {
				out = strings.TrimPrefix(strings.TrimPrefix(out, "*certissue.Issuer "), "certissue.Issuer ")
			}
			if out != "certissue.Issuer{key: withheld}" {
				t.Errorf("%s of %T = %q", verb, operand, out)
			}
			for _, s := range secrets {
				if strings.Contains(out, s) {
					t.Fatalf("%s printed the key", verb)
				}
			}
		}
	}
}

func TestCanonical(t *testing.T) {
	cases := map[string]string{
		"API.Example.Test":                   "dns:api.example.test",
		"*.example.test":                     "dns:*.example.test",
		docIPv4:                              "ip:" + docIPv4,
		docIPv6:                              "ip:" + docIPv6,
		"spiffe://example.test/workload/api": "uri:spiffe://example.test/workload/api",
	}
	for in, want := range cases {
		s, err := canonical(in)
		if err != nil || s.key != want {
			t.Errorf("canonical(%q) = %q, %v; want %q", in, s.key, err, want)
		}
	}
	for _, bad := range []string{"", ":no-scheme", "a b:c"} {
		if _, err := canonical(bad); err == nil {
			t.Errorf("canonical(%q) accepted it", bad)
		}
	}
	if got := canonSet([]string{"", ":no-scheme", "api.example.test"}); len(got) != 1 || !got["dns:api.example.test"] {
		t.Errorf("canonSet kept %v", got)
	}
}
