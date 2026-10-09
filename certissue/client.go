// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"sync"
	"time"
)

// Config configures a Client.
type Config struct {
	// Names are the DNS names, IP addresses and URIs to ask for. The issuer
	// may grant fewer.
	Names []string

	// TTL is the lifetime to ask for. The issuer may grant less.
	TTL time.Duration

	// Issue sends a DER-encoded certificate request to the issuer and returns
	// the DER leaf and the chain above it. Wire it to your transport. It is
	// called from GetCertificate, GetClientCertificate and the renewal loop,
	// never concurrently with itself.
	Issue func(ctx context.Context, csr []byte, ttl time.Duration) (leaf []byte, chain [][]byte, err error)

	// NewKey makes the key for one certificate; every renewal gets a fresh
	// one. Nil means ECDSA P-256.
	NewKey func() (crypto.Signer, error)

	// RenewAt is the point in a certificate's lifetime, from 0 to 1, at which
	// it is renewed. Values outside (0, 1) mean 2/3.
	RenewAt float64

	// Jitter moves each renewal earlier by a random share of the lifetime, up
	// to this much, so that many clients started together do not renew
	// together. Zero means 0.1, a negative value means none, and it is never
	// more than half of RenewAt.
	Jitter float64

	// Retry is the shortest wait between two renewal attempts. After a failed
	// one the loop waits a quarter of the remaining lifetime, but never less
	// than this. Zero or negative means one second.
	Retry time.Duration

	// Rand returns a number in [0, 1) for the jitter. Nil means math/rand/v2.
	Rand func() float64

	// Clock is the only source of time. Nil means the system clock.
	Clock Clock
}

// ErrStarted is returned by a second call to Client.Start.
var ErrStarted = errors.New("certissue: renewal already started")

// Client holds a certificate for crypto/tls and renews it. It generates its
// own keys and never sees the issuer's.
type Client struct {
	cfg Config

	// issueMu serialises calls to cfg.Issue.
	issueMu sync.Mutex

	// mu guards the rest. next is when the loop should renew; done is closed
	// when the loop returns.
	mu      sync.Mutex
	cert    *tls.Certificate
	next    time.Time
	started bool
	done    chan struct{}
}

// NewClient returns a Client for c. It does nothing until a certificate is
// asked for or Start is called.
func NewClient(c Config) *Client {
	if c.NewKey == nil {
		c.NewKey = newP256
	}
	if !(c.RenewAt > 0 && c.RenewAt < 1) {
		c.RenewAt = 2.0 / 3
	}
	switch {
	case c.Jitter == 0:
		c.Jitter = 0.1
	case !(c.Jitter > 0):
		c.Jitter = 0
	}
	c.Jitter = min(c.Jitter, c.RenewAt/2)
	if c.Retry <= 0 {
		c.Retry = time.Second
	}
	if c.Rand == nil {
		c.Rand = mrand.Float64
	}
	if c.Clock == nil {
		c.Clock = systemClock{}
	}
	c.Names = slices.Clone(c.Names)
	return &Client{cfg: c}
}

func newP256() (crypto.Signer, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// GetCertificate is for tls.Config.GetCertificate. It returns the current
// certificate, or obtains one if there is none or it has expired. It never
// returns an expired certificate: if a new one cannot be had, it fails.
func (c *Client) GetCertificate(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return c.get(h.Context())
}

// GetClientCertificate is for tls.Config.GetClientCertificate, and behaves
// like GetCertificate.
func (c *Client) GetClientCertificate(r *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	return c.get(r.Context())
}

// Start obtains a certificate, then renews it in the background until ctx is
// done. Each renewal happens at RenewAt of the certificate's lifetime, less
// the jitter. A failed renewal is retried, and while it keeps failing the old
// certificate is served until it expires, then not at all.
//
// Start returns the first attempt's error, and starts nothing if it failed,
// so it can be called again. Once it has succeeded, further calls return
// ErrStarted. This is the only function in the package that starts a
// goroutine.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return ErrStarted
	}
	c.started = true
	c.done = make(chan struct{})
	c.mu.Unlock()
	if _, err := c.refresh(ctx); err != nil {
		c.mu.Lock()
		c.started = false
		c.mu.Unlock()
		return err
	}
	go c.loop(ctx, c.done)
	return nil
}

func (c *Client) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.cfg.Clock.After(c.wait()):
		}
		if _, err := c.refresh(ctx); err != nil {
			c.backoff()
		}
	}
}

// wait is how long the loop sleeps before its next attempt.
func (c *Client) wait() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return max(c.next.Sub(c.cfg.Clock.Now()), c.cfg.Retry)
}

// backoff schedules the next attempt after a failed one: a quarter of what is
// left of the current certificate, floored at Retry by wait.
func (c *Client) backoff() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Clock.Now()
	c.next = now.Add(c.cert.Leaf.NotAfter.Sub(now) / 4)
}

func (c *Client) get(ctx context.Context) (*tls.Certificate, error) {
	if cert := c.current(); cert != nil {
		return cert, nil
	}
	return c.obtain(ctx)
}

// obtain issues a certificate unless another caller did while this one
// waited its turn.
func (c *Client) obtain(ctx context.Context) (*tls.Certificate, error) {
	c.issueMu.Lock()
	defer c.issueMu.Unlock()
	if cert := c.current(); cert != nil {
		return cert, nil
	}
	return c.issue(ctx)
}

// refresh issues a certificate even if the current one is still good.
func (c *Client) refresh(ctx context.Context) (*tls.Certificate, error) {
	c.issueMu.Lock()
	defer c.issueMu.Unlock()
	return c.issue(ctx)
}

// current is the certificate if it has not expired, and nil otherwise.
func (c *Client) current() *tls.Certificate {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cert != nil && c.cfg.Clock.Now().Before(c.cert.Leaf.NotAfter) {
		return c.cert
	}
	return nil
}

// issue makes a key and a request, has it signed, checks the answer and
// installs it. The caller holds issueMu.
func (c *Client) issue(ctx context.Context) (*tls.Certificate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.cfg.Issue == nil {
		return nil, errors.New("certissue: Config.Issue is not set")
	}
	key, err := c.cfg.NewKey()
	if err != nil {
		return nil, fmt.Errorf("certissue: new key: %w", err)
	}
	var names sanLists
	for _, n := range c.cfg.Names {
		s, err := canonical(n)
		if err != nil {
			return nil, fmt.Errorf("certissue: name %q: %w", n, err)
		}
		names.add(s)
	}
	tmpl := &x509.CertificateRequest{DNSNames: names.dns, IPAddresses: names.ips, URIs: names.uris}
	csr, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return nil, fmt.Errorf("certissue: certificate request: %w", err)
	}
	der, chain, err := c.cfg.Issue(ctx, csr, c.cfg.TTL)
	if err != nil {
		return nil, fmt.Errorf("certissue: issue: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("certissue: issued certificate: %w", err)
	}
	pub, ok := key.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !pub.Equal(leaf.PublicKey) {
		return nil, errors.New("certissue: issued certificate is for a different key")
	}
	if !c.cfg.Clock.Now().Before(leaf.NotAfter) {
		return nil, errors.New("certissue: issued certificate has already expired")
	}
	cert := &tls.Certificate{
		Certificate: append([][]byte{der}, chain...),
		PrivateKey:  key,
		Leaf:        leaf,
	}
	r := c.cfg.Rand()
	if !(r >= 0) {
		r = 0
	}
	share := c.cfg.RenewAt - c.cfg.Jitter*min(r, 1)
	life := leaf.NotAfter.Sub(leaf.NotBefore)
	c.mu.Lock()
	c.cert = cert
	c.next = leaf.NotBefore.Add(time.Duration(float64(life) * share))
	c.mu.Unlock()
	return cert, nil
}
