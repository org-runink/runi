// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
)

// A san is one subject alternative name in its typed form, with a key that
// compares equal for names that mean the same thing. Keys carry their kind,
// so a DNS name can never match a URI or an e-mail address that happens to
// spell the same.
type san struct {
	key string
	dns string
	ip  net.IP
	uri *url.URL
}

var errEmptyName = errors.New("certissue: empty name")

// canonical reads a name the way Policy, Config.Names and VerifyPeerIdentity
// all write them: an IP address if it parses as one, a URI if it contains a
// colon, and otherwise a DNS name, compared without case.
func canonical(name string) (san, error) {
	if name == "" {
		return san{}, errEmptyName
	}
	if ip := net.ParseIP(name); ip != nil {
		return san{key: "ip:" + ip.String(), ip: ip}, nil
	}
	if strings.Contains(name, ":") {
		u, err := url.Parse(name)
		if err != nil {
			return san{}, err
		}
		return san{key: "uri:" + u.String(), uri: u}, nil
	}
	d := strings.ToLower(name)
	return san{key: "dns:" + d, dns: d}, nil
}

// canonSet is the set of keys for names. A name that cannot be read is left
// out: it cannot match anything, so leaving it out allows nothing extra.
func canonSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if s, err := canonical(n); err == nil {
			set[s.key] = true
		}
	}
	return set
}

// requested lists the names a certificate request asks for. E-mail addresses
// are not among them: the Issuer never issues one.
func requested(csr *x509.CertificateRequest) []san {
	var out []san
	for _, d := range csr.DNSNames {
		d = strings.ToLower(d)
		out = append(out, san{key: "dns:" + d, dns: d})
	}
	for _, ip := range csr.IPAddresses {
		out = append(out, san{key: "ip:" + ip.String(), ip: ip})
	}
	for _, u := range csr.URIs {
		out = append(out, san{key: "uri:" + u.String(), uri: u})
	}
	return out
}

// presented lists the keys of every name a certificate carries, e-mail
// addresses included, so that a peer check sees all of them.
func presented(c *x509.Certificate) []string {
	var out []string
	for _, d := range c.DNSNames {
		out = append(out, "dns:"+strings.ToLower(d))
	}
	for _, ip := range c.IPAddresses {
		out = append(out, "ip:"+ip.String())
	}
	for _, u := range c.URIs {
		out = append(out, "uri:"+u.String())
	}
	for _, e := range c.EmailAddresses {
		out = append(out, "email:"+e)
	}
	return out
}

// sanLists collects typed names for a certificate or a request template.
type sanLists struct {
	dns  []string
	ips  []net.IP
	uris []*url.URL
}

func (l *sanLists) add(s san) {
	switch {
	case s.ip != nil:
		l.ips = append(l.ips, s.ip)
	case s.uri != nil:
		l.uris = append(l.uris, s.uri)
	default:
		l.dns = append(l.dns, s.dns)
	}
}

func (l *sanLists) empty() bool {
	return len(l.dns)+len(l.ips)+len(l.uris) == 0
}
