// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"bytes"
	"crypto/x509"
	"slices"
	"sync"
)

// Bundle is the set of CA certificates a peer trusts, changed in place while a
// CA is rotated. The zero value is an empty bundle, ready to use, and it is
// safe for concurrent use.
//
// Rotation is three steps. Add the new CA everywhere before it signs
// anything; switch the Issuer to the new CA; Retire the old one once the last
// leaf it signed has expired, which is at most the longest TTL you issue.
// In between, leaves under either CA verify.
//
// crypto/tls reads a pool when a config is made, so to follow a Bundle as it
// changes, build the pool per connection, for example from
// tls.Config.GetConfigForClient on a server.
type Bundle struct {
	mu  sync.Mutex
	cas []*x509.Certificate
}

// Add trusts ca. Adding a certificate already in the bundle, or nil, does
// nothing.
func (b *Bundle) Add(ca *x509.Certificate) {
	if ca == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if slices.ContainsFunc(b.cas, func(c *x509.Certificate) bool { return bytes.Equal(c.Raw, ca.Raw) }) {
		return
	}
	b.cas = append(b.cas, ca)
}

// Retire stops trusting ca. Retiring one that is not in the bundle, or nil,
// does nothing.
func (b *Bundle) Retire(ca *x509.Certificate) {
	if ca == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cas = slices.DeleteFunc(b.cas, func(c *x509.Certificate) bool { return bytes.Equal(c.Raw, ca.Raw) })
}

// Pool returns a new pool holding the bundle's CAs as they are now. Later
// changes to the bundle do not reach a pool already returned.
func (b *Bundle) Pool() *x509.CertPool {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := x509.NewCertPool()
	for _, c := range b.cas {
		p.AddCert(c)
	}
	return p
}
