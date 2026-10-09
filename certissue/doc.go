// SPDX-License-Identifier: BSD-3-Clause

// Package certissue signs short-lived X.509 certificates from certificate
// requests, keeps the signing key out of reach of everything that asks for
// one, and lets a CA be rotated with the old and the new trusted side by side.
//
// There are four pieces, and none of them knows about a transport:
//
//   - Issuer holds the CA's crypto.Signer and turns a PKCS#10 request into a
//     leaf certificate. A Policy decides which names an authenticated
//     identity may have and for how long; the Issuer clamps the request to
//     that answer.
//   - Client generates its own key, asks for a certificate through a function
//     you wire to your transport, hands the result to crypto/tls, and renews
//     it before it expires. It never sees the CA's key.
//   - Bundle is the set of CAs a peer trusts. Add the new CA before it signs
//     anything, retire the old one once its last leaf has expired.
//   - VerifyPeerIdentity checks the NAME on a peer's certificate, not only
//     that the certificate chains to a trusted CA.
//
// A whole round trip, in one process for brevity:
//
//	iss, _ := certissue.NewIssuer(caSigner, caCert, policy, certissue.Options{MaxTTL: 24 * time.Hour})
//	cl := certissue.NewClient(certissue.Config{
//		Names: []string{"api.example.test"},
//		TTL:   time.Hour,
//		Issue: func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
//			// Normally this crosses your transport, and the host fills in
//			// Identity from what it authenticated, never from the request.
//			return iss.Issue(ctx, certissue.Request{CSR: csr, Identity: who, TTL: ttl})
//		},
//	})
//	err := cl.Start(ctx)
//	tlsConfig := &tls.Config{GetCertificate: cl.GetCertificate}
//
// # What the Issuer takes from a request
//
// Only the public key and the subject alternative names. The request's own
// signature must verify, so nobody can ask for a certificate on a key they do
// not hold. Its subject, its extensions and its e-mail addresses are ignored:
// the leaf has an empty subject, and carries exactly the DNS names, IP
// addresses and URIs that were both requested and returned by Policy. A
// requested name Policy did not return is dropped, not issued; a name Policy
// returned but nobody requested is not added. Matching is exact (DNS names
// ignore case), so a Policy that returns "*.example.test" allows that literal
// wildcard and nothing it would expand to.
//
// The lifetime is the shortest of the requested TTL, Policy's TTL and
// Options.MaxTTL, rounded down to whole seconds, and never past the CA's own
// expiry.
//
// # No path to the key
//
// The signer lives in an unexported field of Issuer. No method returns it, no
// exported field leads to it, and the type formats as a fixed string, so a
// stray %+v in a log line cannot print it. Use a crypto.Signer backed by a
// hardware module or a key service if the key must not be in process memory
// at all; the Issuer only ever calls Public and Sign.
//
// # Clocks and goroutines
//
// Every reading of the time goes through a Clock you can replace, and the
// only goroutine in the package is the renewal loop that Client.Start
// starts and its context stops. A test in this package parses the source and
// fails if either rule is broken.
//
// # What it does not do
//
// It does not authenticate anyone. Identity is whatever the host says it
// established, through mTLS, a token or anything else; certissue trusts it
// completely, so the host must never fill it from the request itself.
//
// It does not revoke. There is no CRL and no OCSP: short lifetimes are the
// revocation story, so keep them short, and stop issuing to an identity to
// remove it.
//
// It does not create, store or rotate CA keys, does not publish trust
// bundles, and does not keep a record of what it issued. Those belong to the
// host. If you need a complete CA server with storage, audit and ACME, use one
// (step-ca and HashiCorp Vault's PKI engine are two); for certificates a
// browser must trust, use ACME against a public CA.
//
// It issues end-entity certificates only, usable for both TLS server and
// client authentication, from a CA certificate you supply. Intermediates
// above that CA are yours to append to the chain.
package certissue
