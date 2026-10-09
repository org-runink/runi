// SPDX-License-Identifier: BSD-3-Clause

package certissue_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"time"

	"github.com/org-runink/runi/certissue"
)

// fixedClock stops time so the examples print the same thing every run.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func (c fixedClock) After(time.Duration) <-chan time.Time { return nil }

var now = time.Date(2030, 6, 1, 12, 0, 0, 0, time.UTC)

// exampleCA makes a throwaway CA. Real ones are made once, kept offline or
// in a key service, and passed to NewIssuer as a crypto.Signer.
func exampleCA(name string) (crypto.Signer, *x509.Certificate) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return key, cert
}

func Example() {
	caKey, caCert := exampleCA("Example Test CA")

	// The policy: an authenticated identity may have its own name under
	// example.test, for at most an hour.
	policy := certissue.PolicyFunc(func(id certissue.Identity, _ *x509.CertificateRequest) ([]string, time.Duration, error) {
		return []string{id.Subject + ".example.test"}, time.Hour, nil
	})
	issuer, err := certissue.NewIssuer(caKey, caCert, policy, certissue.Options{
		MaxTTL: 24 * time.Hour,
		Clock:  fixedClock{now},
	})
	if err != nil {
		panic(err)
	}

	// The client asks for more than it may have, for longer than it may.
	client := certissue.NewClient(certissue.Config{
		Names: []string{"api.example.test", "admin.example.test"},
		TTL:   8 * time.Hour,
		Clock: fixedClock{now},
		Issue: func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
			// In a real deployment this crosses your transport, and the host
			// fills in Identity from what it authenticated.
			return issuer.Issue(ctx, certissue.Request{CSR: csr, Identity: certissue.Identity{Subject: "api"}, TTL: ttl})
		},
	})
	cert, err := client.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil {
		panic(err)
	}
	fmt.Println("names:", cert.Leaf.DNSNames)
	fmt.Println("lifetime:", cert.Leaf.NotAfter.Sub(cert.Leaf.NotBefore))

	// A peer that trusts the CA still checks the name.
	var trust certissue.Bundle
	trust.Add(caCert)
	chains, err := cert.Leaf.Verify(x509.VerifyOptions{Roots: trust.Pool(), CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	if err != nil {
		panic(err)
	}
	fmt.Println("expecting api:", certissue.VerifyPeerIdentity([]string{"api.example.test"})(nil, chains) == nil)
	fmt.Println("expecting db: ", certissue.VerifyPeerIdentity([]string{"db.example.test"})(nil, chains) == nil)
	// Output:
	// names: [api.example.test]
	// lifetime: 1h0m0s
	// expecting api: true
	// expecting db:  false
}

func ExampleBundle() {
	oldKey, oldCA := exampleCA("Example Test CA 1")
	_, newCA := exampleCA("Example Test CA 2")
	allowAll := certissue.PolicyFunc(func(certissue.Identity, *x509.CertificateRequest) ([]string, time.Duration, error) {
		return []string{"api.example.test"}, time.Hour, nil
	})
	issuer, err := certissue.NewIssuer(oldKey, oldCA, allowAll, certissue.Options{MaxTTL: time.Hour, Clock: fixedClock{now}})
	if err != nil {
		panic(err)
	}
	client := certissue.NewClient(certissue.Config{
		Names: []string{"api.example.test"},
		TTL:   time.Hour,
		Clock: fixedClock{now},
		Issue: func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
			return issuer.Issue(ctx, certissue.Request{CSR: csr, TTL: ttl})
		},
	})
	cert, err := client.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		panic(err)
	}
	trusted := func(b *certissue.Bundle) bool {
		_, err := cert.Leaf.Verify(x509.VerifyOptions{Roots: b.Pool(), CurrentTime: now})
		return err == nil
	}

	var b certissue.Bundle
	b.Add(oldCA)
	b.Add(newCA) // step 1: trust the new CA everywhere before it signs anything
	fmt.Println("old leaf, both CAs trusted:", trusted(&b))
	// step 2: point the issuer at the new CA; step 3, once every old leaf
	// has expired:
	b.Retire(oldCA)
	fmt.Println("old leaf, old CA retired:  ", trusted(&b))
	// Output:
	// old leaf, both CAs trusted: true
	// old leaf, old CA retired:   false
}
