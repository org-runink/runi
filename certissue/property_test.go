// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The unit tests pin one case of each rule. These state the rules themselves,
// over generated requests, policies, lifetimes and clocks. The generator is
// seeded, so a failure reproduces; keys come from crypto/rand, so every run
// also tries new ones.

func subset(r *mrand.Rand, from []string) []string {
	var out []string
	for _, s := range from {
		if r.IntN(2) == 0 {
			out = append(out, s)
		}
	}
	return out
}

// request asks for names, with the DNS ones in random case, a subject, and
// sometimes an e-mail address: everything a requester might try to slip in.
func request(t *testing.T, r *mrand.Rand, key *ecdsa.PrivateKey, names []string) []byte {
	t.Helper()
	var l sanLists
	for _, n := range names {
		s, err := canonical(n)
		if err != nil {
			t.Fatal(err)
		}
		if s.dns != "" && r.IntN(2) == 0 {
			s.dns = strings.ToUpper(s.dns)
		}
		l.add(s)
	}
	tmpl := &x509.CertificateRequest{
		Subject:     pkix.Name{CommonName: "requested-subject.example.test"},
		DNSNames:    l.dns,
		IPAddresses: l.ips,
		URIs:        l.uris,
	}
	if r.IntN(2) == 0 {
		tmpl.EmailAddresses = []string{"someone@example.test"}
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// Invariant: an issued leaf never carries a name Policy did not return, in
// any field: SANs, e-mail addresses or the subject.
func TestPropertyLeafCarriesOnlyNamesPolicyReturned(t *testing.T) {
	r := mrand.New(mrand.NewPCG(31, 32))
	ca := defaultCA(t)
	key := newKey(t)
	issued := 0
	for i := 0; i < 400; i++ {
		granted := subset(r, universe)
		if r.IntN(3) == 0 {
			// Strings spelled like the e-mail address and the subject the
			// request carries. Neither may come through.
			granted = append(granted, "someone@example.test", "requested-subject.example.test")
		}
		iss := newIssuer(t, ca, allow(time.Hour, granted...), Options{Clock: newFakeClock(t0)})
		der, _, err := iss.Issue(context.Background(), Request{CSR: request(t, r, key, subset(r, universe)), TTL: time.Hour})
		if errors.Is(err, ErrDenied) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		issued++
		leaf := parse(t, der)
		ok := canonSet(granted)
		for _, n := range presented(leaf) {
			if !ok[n] {
				t.Fatalf("leaf carries %s; policy returned %v", n, granted)
			}
		}
		if leaf.Subject.String() != "" {
			t.Fatalf("leaf has subject %q", leaf.Subject)
		}
	}
	if issued < 100 {
		t.Fatalf("only %d of 400 cases issued anything; the property was barely exercised", issued)
	}
}

// Invariant: names in the request that Policy did not allow are dropped, not
// issued, and names Policy allowed but nobody asked for are not added. The
// leaf is exactly the intersection, and an empty intersection is ErrDenied.
func TestPropertyDisallowedNamesAreDropped(t *testing.T) {
	r := mrand.New(mrand.NewPCG(33, 34))
	ca := defaultCA(t)
	key := newKey(t)
	for i := 0; i < 400; i++ {
		asked, granted := subset(r, universe), subset(r, universe)
		want := map[string]bool{}
		allowed := canonSet(granted)
		for k := range canonSet(asked) {
			if allowed[k] {
				want[k] = true
			}
		}
		iss := newIssuer(t, ca, allow(time.Hour, granted...), Options{Clock: newFakeClock(t0)})
		der, _, err := iss.Issue(context.Background(), Request{CSR: request(t, r, key, asked), TTL: time.Hour})
		if len(want) == 0 {
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("asked %v, granted %v: err = %v, want ErrDenied", asked, granted, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, n := range presented(parse(t, der)) {
			got[n] = true
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("asked %v, granted %v: leaf has %v, want exactly %v", asked, granted, got, want)
		}
	}
}

// Policy sees the request after its names have been read, so a policy that
// edits the request cannot add a name to it.
func TestPolicyCannotAddNamesByEditingTheRequest(t *testing.T) {
	p := PolicyFunc(func(_ Identity, csr *x509.CertificateRequest) ([]string, time.Duration, error) {
		csr.DNSNames = append(csr.DNSNames, "db.example.test")
		return []string{"api.example.test", "db.example.test"}, time.Hour, nil
	})
	iss := newIssuer(t, defaultCA(t), p, Options{Clock: newFakeClock(t0)})
	der, _, err := iss.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if got := parse(t, der).DNSNames; len(got) != 1 || got[0] != "api.example.test" {
		t.Fatalf("leaf names %v, want only the requested one", got)
	}
}

// Invariant: the lifetime is never longer than the request's TTL, Policy's
// TTL or Options.MaxTTL, measured both as NotAfter-NotBefore and as NotAfter
// from the moment of issue, and never runs past the CA. Below one second it
// is refused, and only then.
func TestPropertyLifetimeNeverExceedsAnyLimit(t *testing.T) {
	r := mrand.New(mrand.NewPCG(35, 36))
	long := defaultCA(t)
	short := newCA(t, "Example Test CA", t0.Add(-time.Hour), t0.Add(90*time.Minute))
	csr := csrFor(t, newKey(t), "api.example.test")
	span := int64(3 * time.Hour)
	dur := func() time.Duration {
		if r.IntN(10) == 0 {
			return time.Duration(r.Int64N(int64(2*time.Second))) - time.Second // around the one-second edge
		}
		return time.Duration(r.Int64N(span))
	}
	for i := 0; i < 2000; i++ {
		requested, policy := dur(), dur()
		maxTTL := time.Second + time.Duration(r.Int64N(span))
		ca := long
		if r.IntN(3) == 0 {
			ca = short
		}
		now := t0.Add(time.Duration(r.Int64N(int64(time.Hour))))
		iss := newIssuer(t, ca, allow(policy, "api.example.test"), Options{MaxTTL: maxTTL, Clock: newFakeClock(now)})
		der, _, err := iss.Issue(context.Background(), Request{CSR: csr, TTL: requested})
		limit := min(requested, policy, maxTTL)
		if err != nil {
			if !errors.Is(err, ErrTTL) || limit >= time.Second {
				t.Fatalf("requested %v policy %v max %v: %v", requested, policy, maxTTL, err)
			}
			continue
		}
		if limit < time.Second {
			t.Fatalf("issued although the shortest limit is %v", limit)
		}
		leaf := parse(t, der)
		life := leaf.NotAfter.Sub(leaf.NotBefore)
		fromNow := leaf.NotAfter.Sub(now)
		for _, bound := range []time.Duration{requested, policy, maxTTL} {
			if life > bound || fromNow > bound {
				t.Fatalf("requested %v policy %v max %v: lifetime %v (%v from issue) exceeds %v",
					requested, policy, maxTTL, life, fromNow, bound)
			}
		}
		if leaf.NotAfter.After(ca.cert.NotAfter) || leaf.NotBefore.After(now) {
			t.Fatalf("validity %v to %v, issued at %v under a CA ending %v", leaf.NotBefore, leaf.NotAfter, now, ca.cert.NotAfter)
		}
	}
}

// Invariant: during a rotation a leaf under the old CA verifies against a
// Bundle holding both CAs, and stops verifying once the old CA is retired,
// while a leaf under the new CA verifies throughout.
func TestPropertyRotationBundle(t *testing.T) {
	r := mrand.New(mrand.NewPCG(37, 38))
	const n = 4
	cas := make([]testCA, n)
	issuers := make([]*Issuer, n)
	for k := range cas {
		cas[k] = newCA(t, fmt.Sprintf("Example Test CA %d", k+1), t0.Add(-time.Hour), t0.Add(48*time.Hour))
		issuers[k] = newIssuer(t, cas[k], allow(time.Hour, "api.example.test"), Options{Clock: newFakeClock(t0)})
	}
	csr := csrFor(t, newKey(t), "api.example.test")
	leafUnder := func(k int) *x509.Certificate {
		der, _, err := issuers[k].Issue(context.Background(), Request{CSR: csr, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return parse(t, der)
	}
	for i := 0; i < 200; i++ {
		perm := r.Perm(n)
		oldK, newK, otherK := perm[0], perm[1], perm[2]
		oldLeaf, newLeaf := leafUnder(oldK), leafUnder(newK)

		var b Bundle
		order := []int{oldK, newK}
		if r.IntN(2) == 0 {
			order = []int{newK, oldK}
		}
		for _, k := range order {
			b.Add(cas[k].cert)
			if r.IntN(2) == 0 {
				b.Add(cas[k].cert)
			}
		}
		if r.IntN(2) == 0 {
			b.Add(cas[otherK].cert)
		}
		pool := b.Pool()
		if !verifies(oldLeaf, pool) || !verifies(newLeaf, pool) {
			t.Fatalf("case %d: with both CAs trusted, a leaf did not verify", i)
		}

		b.Retire(cas[oldK].cert)
		if r.IntN(2) == 0 {
			b.Retire(cas[oldK].cert)
		}
		pool = b.Pool()
		if verifies(oldLeaf, pool) {
			t.Fatalf("case %d: a leaf under the retired CA still verifies", i)
		}
		if !verifies(newLeaf, pool) {
			t.Fatalf("case %d: retiring the old CA broke the new one", i)
		}
	}
}

// Invariant: VerifyPeerIdentity rejects a certificate that chains correctly
// but carries any name outside the allowlist, and accepts it when every name
// is inside.
func TestPropertyPeerNameOutsideAllowlistIsRejected(t *testing.T) {
	r := mrand.New(mrand.NewPCG(39, 40))
	ca := defaultCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	key := newKey(t)
	anything := newIssuer(t, ca, allow(time.Hour, universe...), Options{Clock: newFakeClock(t0)})
	rejected := 0
	for i := 0; i < 400; i++ {
		names := subset(r, universe)
		if len(names) == 0 {
			continue
		}
		allowList := subset(r, universe)
		der, _, err := anything.Issue(context.Background(), Request{CSR: csrFor(t, key, names...), TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		chains, err := parse(t, der).Verify(x509.VerifyOptions{Roots: roots, CurrentTime: t0, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
		if err != nil {
			t.Fatalf("the certificate does not chain: %v", err)
		}
		allowed := canonSet(allowList)
		inside := true
		for k := range canonSet(names) {
			if !allowed[k] {
				inside = false
			}
		}
		err = VerifyPeerIdentity(allowList)(nil, chains)
		if inside != (err == nil) {
			t.Fatalf("names %v, allow %v: err = %v", names, allowList, err)
		}
		if !inside {
			rejected++
			if !errors.Is(err, ErrPeerName) {
				t.Fatalf("names %v, allow %v: err = %v, want ErrPeerName", names, allowList, err)
			}
		}
	}
	if rejected < 100 {
		t.Fatalf("only %d rejections; the property was barely exercised", rejected)
	}
}

// Invariant: renewal never leaves GetCertificate returning an expired leaf.
// The renewal loop runs against a fake clock while time jumps by random
// amounts and the issuer fails a third of the time; at every step
// GetCertificate returns a leaf that has not expired, or an error, and it
// returns an error only while the issuer is failing.
func TestPropertyGetCertificateNeverReturnsExpired(t *testing.T) {
	r := mrand.New(mrand.NewPCG(41, 42))
	ca := defaultCA(t)
	errDown := errors.New("issuer unavailable")
	for run := 0; run < 30; run++ {
		clock := newFakeClock(t0)
		ttl := 10*time.Second + time.Duration(r.Int64N(int64(2*time.Hour)))
		iss := newIssuer(t, ca, allow(ttl, "api.example.test"), Options{Clock: clock})
		var fail atomic.Bool
		seed := r.Uint64()
		jitter := mrand.New(mrand.NewPCG(seed, seed+1))
		c := NewClient(Config{
			Names:   []string{"api.example.test"},
			TTL:     ttl,
			Clock:   clock,
			RenewAt: 0.3 + 0.6*r.Float64(),
			Jitter:  0.3 * r.Float64(),
			Rand:    jitter.Float64,
			Issue: func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
				if fail.Load() {
					return nil, nil, errDown
				}
				return iss.Issue(ctx, Request{CSR: csr, TTL: ttl})
			},
		})
		ctx, cancel := context.WithCancel(context.Background())
		if err := c.Start(ctx); err != nil {
			t.Fatal(err)
		}
		<-clock.armed
		for step := 0; step < 30; step++ {
			fail.Store(r.IntN(3) == 0)
			if clock.Advance(time.Duration(r.Int64N(int64(ttl*6/10)))) > 0 {
				<-clock.armed // the loop has run and parked again
			}
			cert, err := c.GetCertificate(&tls.ClientHelloInfo{})
			if err != nil {
				if !fail.Load() || cert != nil {
					t.Fatalf("run %d step %d: %v with a healthy issuer", run, step, err)
				}
				continue
			}
			if now := clock.Now(); !now.Before(cert.Leaf.NotAfter) {
				t.Fatalf("run %d step %d: served a leaf that expired at %v; now %v", run, step, cert.Leaf.NotAfter, now)
			}
		}
		cancel()
		<-c.done
	}
}

// rawCSR is the outer shape of a PKCS#10 request, enough to re-sign one.
type rawCSR struct {
	TBS asn1.RawValue
	Alg pkix.AlgorithmIdentifier
	Sig asn1.BitString
}

// Invariant: the request's own signature must verify. A request carrying
// someone else's public key, signed by a key that is not it, is refused; so
// is an honest request with any single bit flipped.
func TestPropertyForgedCSRIsRejected(t *testing.T) {
	r := mrand.New(mrand.NewPCG(43, 44))
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, universe...), Options{Clock: newFakeClock(t0)})
	victim, attacker := newKey(t), newKey(t)
	for i := 0; i < 300; i++ {
		names := subset(r, universe)
		if len(names) == 0 {
			names = universe[:1]
		}
		honest := csrFor(t, victim, names...)
		if _, _, err := iss.Issue(context.Background(), Request{CSR: honest, TTL: time.Hour}); err != nil {
			t.Fatalf("the honest request was refused: %v", err)
		}

		var parts rawCSR
		if rest, err := asn1.Unmarshal(honest, &parts); err != nil || len(rest) != 0 {
			t.Fatalf("splitting the request: %v", err)
		}
		digest := sha256.Sum256(parts.TBS.FullBytes)
		sig, err := ecdsa.SignASN1(rand.Reader, attacker, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		forged, err := asn1.Marshal(rawCSR{TBS: parts.TBS, Alg: parts.Alg, Sig: asn1.BitString{Bytes: sig, BitLength: 8 * len(sig)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := x509.ParseCertificateRequest(forged); err != nil {
			t.Fatalf("the forgery is not even well-formed, so it proves nothing: %v", err)
		}
		if _, _, err := iss.Issue(context.Background(), Request{CSR: forged, TTL: time.Hour}); !errors.Is(err, ErrInvalidCSR) {
			t.Fatalf("a request signed by another key: err = %v, want ErrInvalidCSR", err)
		}

		flipped := bytes.Clone(honest)
		at, bit := r.IntN(len(flipped)), r.IntN(8)
		flipped[at] ^= 1 << bit
		if _, _, err := iss.Issue(context.Background(), Request{CSR: flipped, TTL: time.Hour}); !errors.Is(err, ErrInvalidCSR) {
			t.Fatalf("bit %d of byte %d flipped: err = %v, want ErrInvalidCSR", bit, at, err)
		}
	}
}

// Invariant: Issue never mutates the CA certificate: not the caller's value,
// not the Issuer's copy, and not through the chain it returns.
func TestPropertyIssueNeverMutatesCA(t *testing.T) {
	r := mrand.New(mrand.NewPCG(45, 46))
	ca := defaultCA(t)
	raw := bytes.Clone(ca.cert.Raw)
	snapshot := parse(t, raw)
	clock := newFakeClock(t0)
	var granted []string
	var ttl time.Duration
	p := PolicyFunc(func(Identity, *x509.CertificateRequest) ([]string, time.Duration, error) {
		return granted, ttl, nil
	})
	iss := newIssuer(t, ca, p, Options{Clock: clock})
	key := newKey(t)
	for i := 0; i < 300; i++ {
		granted = subset(r, universe)
		ttl = time.Duration(r.Int64N(int64(2 * time.Hour)))
		clock.Advance(time.Duration(r.Int64N(int64(time.Hour))))
		_, chain, err := iss.Issue(context.Background(), Request{CSR: request(t, r, key, subset(r, universe)), TTL: time.Hour})
		if err == nil {
			if !bytes.Equal(chain[0], raw) {
				t.Fatal("the chain is not the CA certificate")
			}
			chain[0][0] ^= 0xff // scribble on what the caller was handed
		}
		if !bytes.Equal(ca.cert.Raw, raw) || !reflect.DeepEqual(ca.cert, snapshot) || !reflect.DeepEqual(iss.ca, snapshot) {
			t.Fatalf("case %d: the CA certificate changed", i)
		}
	}
}
