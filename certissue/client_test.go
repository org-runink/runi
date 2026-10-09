// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// wired returns a Client whose Issue calls iss directly, counting calls.
func wired(iss *Issuer, cfg Config, calls *atomic.Int64) *Client {
	cfg.Issue = func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
		calls.Add(1)
		return iss.Issue(ctx, Request{CSR: csr, Identity: Identity{Subject: "api"}, TTL: ttl})
	}
	return NewClient(cfg)
}

func TestNewClientDefaults(t *testing.T) {
	names := []string{"api.example.test"}
	c := NewClient(Config{Names: names})
	names[0] = "changed.example.test"
	if c.cfg.Names[0] != "api.example.test" {
		t.Error("Names was not copied")
	}
	if c.cfg.RenewAt != 2.0/3 || c.cfg.Jitter != 0.1 || c.cfg.Retry != time.Second {
		t.Errorf("defaults: RenewAt %v Jitter %v Retry %v", c.cfg.RenewAt, c.cfg.Jitter, c.cfg.Retry)
	}
	if c.cfg.NewKey == nil || c.cfg.Rand == nil || c.cfg.Clock == nil {
		t.Error("a default function is missing")
	}
	if r := c.cfg.Rand(); r < 0 || r >= 1 {
		t.Errorf("default Rand gave %v", r)
	}
	key, err := c.cfg.NewKey()
	if err != nil || keyTypeOf(key.Public()) != ECDSAP256 {
		t.Errorf("default key: %v, %v", keyTypeOf(key.Public()), err)
	}
	if _, ok := c.cfg.Clock.(systemClock); !ok {
		t.Errorf("default clock is %T", c.cfg.Clock)
	}

	cases := []struct {
		renewAt, jitter         float64
		wantRenewAt, wantJitter float64
	}{
		{0, 0, 2.0 / 3, 0.1},
		{1, 0, 2.0 / 3, 0.1},
		{1.5, 0, 2.0 / 3, 0.1},
		{-0.2, 0, 2.0 / 3, 0.1},
		{math.NaN(), 0, 2.0 / 3, 0.1},
		{0.5, -1, 0.5, 0},
		{0.5, math.NaN(), 0.5, 0},
		{0.5, 0.9, 0.5, 0.25},
		{0.5, math.Inf(1), 0.5, 0.25},
		{0.8, 0.05, 0.8, 0.05},
	}
	for _, tc := range cases {
		c := NewClient(Config{RenewAt: tc.renewAt, Jitter: tc.jitter, Retry: -time.Second})
		if c.cfg.RenewAt != tc.wantRenewAt || c.cfg.Jitter != tc.wantJitter || c.cfg.Retry != time.Second {
			t.Errorf("RenewAt %v Jitter %v: got %v %v %v", tc.renewAt, tc.jitter, c.cfg.RenewAt, c.cfg.Jitter, c.cfg.Retry)
		}
	}
}

// The system clock is the default and the only one that reads the time.
func TestSystemClock(t *testing.T) {
	before := time.Now()
	if got := (systemClock{}).Now(); got.Before(before) {
		t.Fatalf("Now went backwards: %v before %v", got, before)
	}
	<-systemClock{}.After(time.Millisecond)
}

func TestClientGetsAndReuses(t *testing.T) {
	clock := newFakeClock(t0)
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, "api.example.test"), Options{Clock: clock})
	var calls atomic.Int64
	c := wired(iss, Config{Names: []string{"api.example.test"}, TTL: time.Hour, Clock: clock}, &calls)

	// crypto/tls always passes a context; a hand-made hello has none.
	first, err := c.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || calls.Load() != 1 {
		t.Fatalf("a valid certificate was not reused: %d issues", calls.Load())
	}
	if len(first.Certificate) != 2 || first.Leaf == nil || first.PrivateKey == nil {
		t.Fatalf("certificate is incomplete: %d DER blocks", len(first.Certificate))
	}

	clock.Advance(time.Hour)
	third, err := c.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if third == first || calls.Load() != 2 {
		t.Fatal("an expired certificate was not replaced")
	}
	if !clock.Now().Before(third.Leaf.NotAfter) {
		t.Fatal("the replacement is expired")
	}
	if third.PrivateKey.(crypto.Signer).Public().(interface{ Equal(crypto.PublicKey) bool }).Equal(first.PrivateKey.(crypto.Signer).Public()) {
		t.Fatal("a renewal reused the old key")
	}
}

// obtain checks again after taking its turn, so callers queued behind a
// renewal use its result rather than issuing again.
func TestObtainUsesACertificateIssuedWhileWaiting(t *testing.T) {
	clock := newFakeClock(t0)
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, "api.example.test"), Options{Clock: clock})
	var calls atomic.Int64
	c := wired(iss, Config{Names: []string{"api.example.test"}, TTL: time.Hour, Clock: clock}, &calls)
	want, err := c.refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.obtain(context.Background())
	if err != nil || got != want || calls.Load() != 1 {
		t.Fatalf("obtain issued again: %d calls, err %v", calls.Load(), err)
	}
}

func TestClientIssueFailures(t *testing.T) {
	clock := newFakeClock(t0)
	ca := defaultCA(t)
	iss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{Clock: clock})
	errTransport := errors.New("transport down")
	errKey := errors.New("no key for you")
	otherLeaf, _, err := iss.Issue(context.Background(), Request{CSR: csrFor(t, newKey(t), "api.example.test"), TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	stale := newFakeClock(t0.Add(-2 * time.Hour))
	staleIss := newIssuer(t, ca, allow(time.Hour, "api.example.test"), Options{Clock: stale})

	noop := func(context.Context, []byte, time.Duration) ([]byte, [][]byte, error) { return nil, nil, nil }
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no Issue", Config{}},
		{"key generation fails", Config{
			NewKey: func() (crypto.Signer, error) { return nil, errKey },
			Issue:  noop,
		}},
		{"unreadable name", Config{
			Names: []string{":no-scheme"},
			Issue: noop,
		}},
		{"key cannot sign", Config{
			NewKey: func() (crypto.Signer, error) { return failingSigner{pub: newKey(t).Public()}, nil },
			Issue:  noop,
		}},
		{"transport fails", Config{
			Issue: func(context.Context, []byte, time.Duration) ([]byte, [][]byte, error) { return nil, nil, errTransport },
		}},
		{"answer is not a certificate", Config{
			Issue: func(context.Context, []byte, time.Duration) ([]byte, [][]byte, error) {
				return []byte("not DER"), nil, nil
			},
		}},
		{"answer is for another key", Config{
			Issue: func(context.Context, []byte, time.Duration) ([]byte, [][]byte, error) { return otherLeaf, nil, nil },
		}},
		{"answer has already expired", Config{
			Issue: func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
				return staleIss.Issue(ctx, Request{CSR: csr, TTL: ttl})
			},
		}},
	}
	for _, tc := range cases {
		cfg := tc.cfg
		if cfg.Names == nil {
			cfg.Names = []string{"api.example.test"}
		}
		cfg.TTL = time.Hour
		cfg.Clock = clock
		c := NewClient(cfg)
		cert, err := c.GetCertificate(&tls.ClientHelloInfo{})
		if err == nil || cert != nil {
			t.Errorf("%s: got a certificate", tc.name)
		}
	}
}

func TestStartRenewsOnSchedule(t *testing.T) {
	clock := newFakeClock(t0)
	iss := newIssuer(t, defaultCA(t), allow(time.Hour, "api.example.test"), Options{Clock: clock})
	var calls atomic.Int64
	var fail atomic.Bool
	c := NewClient(Config{
		Names:  []string{"api.example.test"},
		TTL:    time.Hour,
		Clock:  clock,
		Jitter: -1,
		Issue: func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
			calls.Add(1)
			if fail.Load() {
				return nil, nil, errors.New("issuer unavailable")
			}
			return iss.Issue(ctx, Request{CSR: csr, TTL: ttl})
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fail.Store(true)
	if err := c.Start(ctx); err == nil {
		t.Fatal("Start succeeded with a failing issuer")
	}
	fail.Store(false)
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); !errors.Is(err, ErrStarted) {
		t.Fatalf("second Start: %v, want ErrStarted", err)
	}
	<-clock.armed
	first, _ := c.GetCertificate(&tls.ClientHelloInfo{})

	// Not due yet: nothing fires.
	if clock.Advance(39*time.Minute) != 0 {
		t.Fatal("renewed before two thirds of the lifetime")
	}
	// Two thirds of an hour after NotBefore, which is t0 rounded down.
	due := first.Leaf.NotBefore.Add(40 * time.Minute).Sub(clock.Now())
	if clock.Advance(due) != 1 {
		t.Fatal("did not renew at two thirds of the lifetime")
	}
	<-clock.armed
	second, _ := c.GetCertificate(&tls.ClientHelloInfo{})
	if second == first {
		t.Fatal("the loop did not replace the certificate")
	}

	// A failed renewal backs off to a quarter of what is left, and the old
	// certificate keeps serving meanwhile.
	fail.Store(true)
	due = second.Leaf.NotBefore.Add(40 * time.Minute).Sub(clock.Now())
	clock.Advance(due)
	<-clock.armed
	left := second.Leaf.NotAfter.Sub(clock.Now())
	c.mu.Lock()
	next := c.next
	c.mu.Unlock()
	if want := clock.Now().Add(left / 4); !next.Equal(want) {
		t.Fatalf("after a failure next = %v, want %v", next, want)
	}
	if got, err := c.GetCertificate(&tls.ClientHelloInfo{}); err != nil || got != second {
		t.Fatal("the still-valid certificate stopped being served after a failed renewal")
	}

	before := calls.Load()
	fail.Store(false)
	clock.Advance(left / 4)
	<-clock.armed
	if calls.Load() != before+1 {
		t.Fatal("the loop did not retry")
	}

	cancel()
	<-c.done
}

// The jitter source is clamped, so a bad one cannot schedule a renewal
// before the certificate starts or after RenewAt.
func TestRenewalPointClampsRand(t *testing.T) {
	for _, tc := range []struct {
		r, wantShare float64
	}{
		{-1, 0.5},
		{math.NaN(), 0.5},
		{0.5, 0.45},
		{7, 0.4},
	} {
		clock := newFakeClock(t0)
		iss := newIssuer(t, defaultCA(t), allow(100*time.Second, "api.example.test"), Options{Clock: clock})
		var calls atomic.Int64
		r := tc.r
		c := wired(iss, Config{
			Names:   []string{"api.example.test"},
			TTL:     time.Hour,
			Clock:   clock,
			RenewAt: 0.5,
			Jitter:  0.1,
			Rand:    func() float64 { return r },
		}, &calls)
		cert, err := c.refresh(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := cert.Leaf.NotBefore.Add(time.Duration(tc.wantShare * float64(100*time.Second)))
		if d := c.next.Sub(want); d < -time.Millisecond || d > time.Millisecond {
			t.Errorf("rand %v: next renewal %v, want %v", tc.r, c.next, want)
		}
	}
}

// End to end over crypto/tls: both sides get certificates from the same
// issuer, trust it through a Bundle, and check each other's NAME.
func TestMutualTLS(t *testing.T) {
	clock := newFakeClock(t0)
	ca := defaultCA(t)
	p := PolicyFunc(func(id Identity, _ *x509.CertificateRequest) ([]string, time.Duration, error) {
		return []string{id.Subject + ".example.test"}, time.Hour, nil
	})
	iss := newIssuer(t, ca, p, Options{Clock: clock})
	client := func(subject string) *Client {
		return NewClient(Config{
			Names: []string{subject + ".example.test"},
			TTL:   time.Hour,
			Clock: clock,
			Issue: func(ctx context.Context, csr []byte, ttl time.Duration) ([]byte, [][]byte, error) {
				return iss.Issue(ctx, Request{CSR: csr, Identity: Identity{Subject: subject}, TTL: ttl})
			},
		})
	}
	var trust Bundle
	trust.Add(ca.cert)

	handshake := func(serverName, clientName string, serverAllows []string) (serverErr, clientErr error) {
		srv, cli := client(serverName), client(clientName)
		// Loopback TCP, not net.Pipe: a refusing server writes its alert
		// while the client is still writing, and a pipe has no buffer to
		// hold either.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		serverConfig := &tls.Config{
			Time:                   clock.Now,
			SessionTicketsDisabled: true,
			GetCertificate:         srv.GetCertificate,
			ClientAuth:             tls.RequireAndVerifyClientCert,
			ClientCAs:              trust.Pool(),
			VerifyPeerCertificate:  VerifyPeerIdentity(serverAllows),
		}
		done := make(chan error, 1)
		go func() {
			raw, err := ln.Accept()
			if err != nil {
				done <- err
				return
			}
			defer raw.Close()
			done <- tls.Server(raw, serverConfig).Handshake()
		}()
		raw, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		conn := tls.Client(raw, &tls.Config{
			Time:                  clock.Now,
			ServerName:            serverName + ".example.test",
			RootCAs:               trust.Pool(),
			GetClientCertificate:  cli.GetClientCertificate,
			VerifyPeerCertificate: VerifyPeerIdentity([]string{serverName + ".example.test"}),
		})
		clientErr = conn.Handshake()
		return <-done, clientErr
	}

	if s, c := handshake("api", "db", []string{"db.example.test"}); s != nil || c != nil {
		t.Fatalf("allowed peers failed: server %v, client %v", s, c)
	}
	if s, _ := handshake("api", "cache", []string{"db.example.test"}); !errors.Is(s, ErrPeerName) {
		t.Fatalf("server accepted a client outside its allowlist: %v", s)
	}
}

func TestStartNeedsAWorkingIssuer(t *testing.T) {
	c := NewClient(Config{Names: []string{"api.example.test"}, TTL: time.Hour, Clock: newFakeClock(t0)})
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("Start with no Issue function succeeded")
	}
	if c.started {
		t.Fatal("a failed Start left the client marked as started")
	}
	if !slices.Equal(c.cfg.Names, []string{"api.example.test"}) {
		t.Fatal("names changed")
	}
}
