// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"crypto/x509"
	"errors"
	"fmt"
)

// Errors returned by the function VerifyPeerIdentity makes.
var (
	ErrUnverified = errors.New("certissue: the peer's chain was not verified")
	ErrPeerName   = errors.New("certissue: peer name not allowed")
)

// VerifyPeerIdentity returns a check for tls.Config.VerifyPeerCertificate that
// accepts a peer only if EVERY name on its certificate is in allow, and it has
// at least one. Names are written as for Policy: DNS names (compared without
// case), IP addresses and URIs. A certificate carrying an e-mail address is
// always refused, because allow cannot name one.
//
// A certificate that chains to a trusted CA proves a CA vouched for it, not
// that it is the peer you meant to talk to: any workload the CA issues to
// would pass. This is the second half of the check.
//
// It reads the chains crypto/tls has already verified and refuses when there
// are none, so it fails closed if verification was skipped (InsecureSkipVerify,
// or a server's ClientAuth below RequireAndVerifyClientCert). crypto/tls does
// not call VerifyPeerCertificate on a resumed session; to check those too, call
// the same function from VerifyConnection with the state's VerifiedChains:
//
//	check := certissue.VerifyPeerIdentity(allow)
//	cfg.VerifyConnection = func(cs tls.ConnectionState) error { return check(nil, cs.VerifiedChains) }
func VerifyPeerIdentity(allow []string) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	set := canonSet(allow)
	return func(_ [][]byte, chains [][]*x509.Certificate) error {
		if len(chains) == 0 || len(chains[0]) == 0 {
			return ErrUnverified
		}
		names := presented(chains[0][0])
		if len(names) == 0 {
			return fmt.Errorf("%w: the certificate carries no names", ErrPeerName)
		}
		for _, n := range names {
			if !set[n] {
				return fmt.Errorf("%w: %s", ErrPeerName, n)
			}
		}
		return nil
	}
}
