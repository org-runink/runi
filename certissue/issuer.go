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
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"
)

// Request is what a requester asks for.
type Request struct {
	// CSR is a DER-encoded PKCS#10 certificate request. Its public key and
	// its subject alternative names are used; nothing else in it is.
	CSR []byte

	// Identity is who the HOST authenticated the requester as. Fill it from
	// your transport's authentication, never from the request's content.
	Identity Identity

	// TTL is the lifetime asked for. The certificate may be shorter, never
	// longer.
	TTL time.Duration
}

// Identity is an authenticated requester, as the host established it.
type Identity struct {
	Subject string
	Groups  []string
	Claims  map[string]string
}

// Policy decides what an authenticated identity may be issued.
//
// Allow returns the names the identity may carry and the longest lifetime it
// may have. It returns names, not a yes or no: the Issuer intersects them with
// what the request asks for. Returning an error refuses the request. A
// non-positive TTL refuses it too.
//
// Allow sees the parsed request, whose signature has already been verified,
// and must not modify it. It may be called concurrently.
type Policy interface {
	Allow(Identity, *x509.CertificateRequest) (names []string, ttl time.Duration, err error)
}

// PolicyFunc adapts a function to Policy.
type PolicyFunc func(Identity, *x509.CertificateRequest) ([]string, time.Duration, error)

// Allow calls f.
func (f PolicyFunc) Allow(id Identity, csr *x509.CertificateRequest) ([]string, time.Duration, error) {
	return f(id, csr)
}

// KeyType names a kind of public key a requester may hold.
type KeyType string

// The key types an Issuer can be told to accept.
const (
	ECDSAP256 KeyType = "ecdsa-p256"
	ECDSAP384 KeyType = "ecdsa-p384"
	ECDSAP521 KeyType = "ecdsa-p521"
	Ed25519   KeyType = "ed25519"
	RSA2048   KeyType = "rsa-2048"
	RSA3072   KeyType = "rsa-3072"
	RSA4096   KeyType = "rsa-4096"
)

// DefaultKeyTypes are accepted when Options.KeyTypes is empty.
var DefaultKeyTypes = []KeyType{ECDSAP256, ECDSAP384, Ed25519}

// Options configure an Issuer.
type Options struct {
	// MaxTTL caps every certificate's lifetime. Required.
	MaxTTL time.Duration

	// Clock is read once per issued certificate. Nil means the system clock.
	Clock Clock

	// Serial returns a fresh, positive serial number. It must be safe for
	// concurrent use. Nil means 128 random bits.
	Serial func() (*big.Int, error)

	// KeyTypes are the requester keys accepted. Empty means DefaultKeyTypes.
	KeyTypes []KeyType
}

// Errors returned by Issue and by the peer check. Each is wrapped with
// detail; test for them with errors.Is.
var (
	ErrInvalidCSR = errors.New("certissue: invalid certificate request")
	ErrKeyType    = errors.New("certissue: key type not allowed")
	ErrDenied     = errors.New("certissue: no requested name is allowed")
	ErrTTL        = errors.New("certissue: no usable lifetime")
	ErrCAValidity = errors.New("certissue: the CA certificate is not valid now")
)

// Issuer signs leaf certificates with a CA key it never exposes.
//
// Every field is unexported, no method returns the signer, and Format prints
// a fixed string, so there is no route from an Issuer value to its key.
// Issue is safe for concurrent use.
type Issuer struct {
	signer crypto.Signer
	ca     *x509.Certificate
	policy Policy
	maxTTL time.Duration
	clock  Clock
	serial func() (*big.Int, error)
	keys   map[KeyType]bool
}

// NewIssuer returns an Issuer that signs with signer under ca.
//
// ca must be a parsed CA certificate whose key usage includes certificate
// signing, and signer's public key must be ca's. The Issuer keeps its own
// copy of ca, so changing the caller's value later changes nothing.
func NewIssuer(signer crypto.Signer, ca *x509.Certificate, p Policy, opts Options) (*Issuer, error) {
	if signer == nil || ca == nil || p == nil {
		return nil, errors.New("certissue: signer, CA and policy are all required")
	}
	if opts.MaxTTL < time.Second {
		return nil, errors.New("certissue: Options.MaxTTL must be at least one second")
	}
	own, err := x509.ParseCertificate(ca.Raw)
	if err != nil {
		return nil, fmt.Errorf("certissue: the CA must be a parsed certificate: %w", err)
	}
	if !own.IsCA || own.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, errors.New("certissue: the CA certificate may not sign certificates")
	}
	pub, ok := signer.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !pub.Equal(own.PublicKey) {
		return nil, errors.New("certissue: the signer's key is not the CA certificate's key")
	}
	clock := opts.Clock
	if clock == nil {
		clock = systemClock{}
	}
	serial := opts.Serial
	if serial == nil {
		serial = randomSerial
	}
	types := opts.KeyTypes
	if len(types) == 0 {
		types = DefaultKeyTypes
	}
	keys := make(map[KeyType]bool, len(types))
	for _, t := range types {
		keys[t] = true
	}
	return &Issuer{
		signer: signer,
		ca:     own,
		policy: p,
		maxTTL: opts.MaxTTL,
		clock:  clock,
		serial: serial,
		keys:   keys,
	}, nil
}

// Format prints a fixed description for every verb, so that formatting an
// Issuer, by accident or otherwise, cannot reveal its key.
func (Issuer) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "certissue.Issuer{key: withheld}")
}

// Issue signs a leaf certificate for r, or says why not.
//
// The request's signature must verify, its key type must be allowed, and
// Policy must allow at least one of the names it asks for. The leaf carries
// only those names, for the shortest of the three lifetimes (see the package
// documentation). chain holds the issuing CA certificate.
func (i *Issuer) Issue(ctx context.Context, r Request) (leaf []byte, chain [][]byte, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	csr, err := x509.ParseCertificateRequest(r.CSR)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidCSR, err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, nil, fmt.Errorf("%w: signature: %v", ErrInvalidCSR, err)
	}
	if kt := keyTypeOf(csr.PublicKey); !i.keys[kt] {
		return nil, nil, fmt.Errorf("%w: %q", ErrKeyType, kt)
	}
	// Read the names before Policy sees the request, so nothing Policy does to
	// it can change what was asked for.
	want := requested(csr)
	names, policyTTL, err := i.policy.Allow(r.Identity, csr)
	if err != nil {
		return nil, nil, fmt.Errorf("certissue: policy refused: %w", err)
	}
	allowed := canonSet(names)
	var grant sanLists
	for _, s := range want {
		if allowed[s.key] {
			grant.add(s)
		}
	}
	if grant.empty() {
		return nil, nil, ErrDenied
	}
	ttl := min(r.TTL, policyTTL, i.maxTTL).Truncate(time.Second)
	if ttl < time.Second {
		return nil, nil, fmt.Errorf("%w: requested %v, policy %v, maximum %v", ErrTTL, r.TTL, policyTTL, i.maxTTL)
	}
	now := i.clock.Now()
	if now.Before(i.ca.NotBefore) || !now.Before(i.ca.NotAfter) {
		return nil, nil, ErrCAValidity
	}
	notBefore := now.UTC().Truncate(time.Second)
	notAfter := notBefore.Add(ttl)
	if notAfter.After(i.ca.NotAfter) {
		notAfter = i.ca.NotAfter
	}
	serial, err := i.serial()
	if err != nil {
		return nil, nil, fmt.Errorf("certissue: serial number: %w", err)
	}
	if serial == nil || serial.Sign() <= 0 {
		return nil, nil, errors.New("certissue: serial number must be positive")
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              grant.dns,
		IPAddresses:           grant.ips,
		URIs:                  grant.uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, i.ca, csr.PublicKey, i.signer)
	if err != nil {
		return nil, nil, fmt.Errorf("certissue: signing: %w", err)
	}
	return der, [][]byte{bytes.Clone(i.ca.Raw)}, nil
}

var curveTypes = map[elliptic.Curve]KeyType{
	elliptic.P256(): ECDSAP256,
	elliptic.P384(): ECDSAP384,
	elliptic.P521(): ECDSAP521,
}

var rsaTypes = map[int]KeyType{2048: RSA2048, 3072: RSA3072, 4096: RSA4096}

// keyTypeOf names a public key's type, or returns "" for one it does not know.
func keyTypeOf(pub crypto.PublicKey) KeyType {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		return curveTypes[k.Curve]
	case ed25519.PublicKey:
		return Ed25519
	case *rsa.PublicKey:
		return rsaTypes[k.N.BitLen()]
	}
	return ""
}

// serialLimit is 2^128: serials are 128 random bits, plus one so that none is
// zero.
var serialLimit = new(big.Int).Lsh(big.NewInt(1), 128)

func randomSerial() (*big.Int, error) {
	return serialFrom(rand.Reader)
}

func serialFrom(r io.Reader) (*big.Int, error) {
	n, err := rand.Int(r, serialLimit)
	if err != nil {
		return nil, err
	}
	return n.Add(n, big.NewInt(1)), nil
}
