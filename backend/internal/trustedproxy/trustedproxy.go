// Package trustedproxy decides whether a request really arrived over TLS (Roadmap Phase 96).
//
// A reverse proxy that terminates TLS tells Vinaire so with the header X-Forwarded-Proto: https. But any client that can
// reach Vinaire directly can send that header too, so it is believed ONLY when the connection itself comes from an
// address the operator named as a proxy of theirs (the chart's trustedProxies / TRUSTED_PROXIES). The default is
// nobody: then only a TLS connection of Vinaire's own counts. The proxy must overwrite the header, not pass the
// client's through — and, where several proxies append to it, the LAST entry (the one the nearest proxy wrote) is read.
package trustedproxy

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// maxEntries bounds the list: it is configuration, not data.
const maxEntries = 64

// Set is the list of proxies whose forwarded headers are believed. The zero value trusts nobody.
type Set struct{ prefixes []netip.Prefix }

// Parse reads a list of addresses and CIDRs separated by commas, spaces or newlines ("10.42.0.15/32, 192.168.88.10").
// A bare address is a single host. An unparseable entry, a /0 (which would trust the whole internet) or too long a list
// is an error. warnings names the entries that are broad — wider than a /24 (IPv4) or /64 (IPv6) — which are accepted
// but could let any pod or machine in that range forge the header.
func Parse(list string) (set Set, warnings []string, err error) {
	fields := strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' })
	if len(fields) > maxEntries {
		return Set{}, nil, fmt.Errorf("at most %d trusted proxies may be listed", maxEntries)
	}
	for _, f := range fields {
		var p netip.Prefix
		if strings.Contains(f, "/") {
			pp, perr := netip.ParsePrefix(f)
			if perr != nil {
				return Set{}, nil, fmt.Errorf("trusted proxy %q is not an address or a CIDR", f)
			}
			p = pp
		} else {
			a, aerr := netip.ParseAddr(f)
			if aerr != nil {
				return Set{}, nil, fmt.Errorf("trusted proxy %q is not an address or a CIDR", f)
			}
			a = a.Unmap()
			p = netip.PrefixFrom(a, a.BitLen())
		}
		if p.Bits() == 0 {
			return Set{}, nil, fmt.Errorf("trusted proxy %q would trust every address — name your proxy's own address", f)
		}
		// an IPv4-mapped IPv6 prefix means the IPv4 one
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		p = p.Masked()
		set.prefixes = append(set.prefixes, p)
		if Broad(p) {
			warnings = append(warnings, p.String())
		}
	}
	return set, warnings, nil
}

// Broad says whether a prefix covers more than one small subnet: wider than a /24 (IPv4) or a /64 (IPv6).
func Broad(p netip.Prefix) bool {
	if p.Addr().Is4() {
		return p.Bits() < 24
	}
	return p.Bits() < 64
}

// Empty is true when nobody is trusted.
func (s Set) Empty() bool { return len(s.prefixes) == 0 }

// Strings lists the entries (normalised), for display.
func (s Set) Strings() []string {
	out := make([]string, 0, len(s.prefixes))
	for _, p := range s.prefixes {
		out = append(out, p.String())
	}
	return out
}

// Broad lists the entries that Broad finds too wide.
func (s Set) Broad() []string {
	var out []string
	for _, p := range s.prefixes {
		if Broad(p) {
			out = append(out, p.String())
		}
	}
	return out
}

// Contains says whether the address a connection came from (an http.Request's RemoteAddr: "ip:port", or a bare
// address) is one of the trusted proxies.
func (s Set) Contains(remoteAddr string) bool {
	if len(s.prefixes) == 0 {
		return false
	}
	host := remoteAddr
	if ap, err := netip.ParseAddrPort(remoteAddr); err == nil {
		host = ap.Addr().String()
	}
	if i := strings.LastIndex(host, "%"); i >= 0 { // a zone
		host = host[:i]
	}
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range s.prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// IsHTTPS is true when the request came over TLS: Vinaire's own TLS connection, or a proxy of the list saying so.
func (s Set) IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !s.Contains(r.RemoteAddr) {
		return false
	}
	return lastToken(r.Header.Values("X-Forwarded-Proto")) == "https"
}

// lastToken is the last comma-separated entry of the last header line, lower-cased: what the nearest proxy wrote.
func lastToken(values []string) string {
	if len(values) == 0 {
		return ""
	}
	v := values[len(values)-1]
	if i := strings.LastIndex(v, ","); i >= 0 {
		v = v[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(v))
}

// ClientIP is the address of the client a request is really from (Roadmap Phase 102). Not from a trusted proxy: the
// connection's own peer, whatever X-Forwarded-For says — any client can send that header. From a trusted proxy: the
// X-Forwarded-For chain is read from the RIGHT, skipping the entries that are themselves trusted proxies (each hop
// appended the address it received from), and the first other entry is the client. A chain that is empty, unreadable
// or made only of proxies gives the peer. The first entry on the left is never believed: it is what the client wrote.
func (s Set) ClientIP(r *http.Request) string {
	peer := Peer(r.RemoteAddr)
	if !s.Contains(r.RemoteAddr) {
		return peer
	}
	var chain []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, e := range strings.Split(v, ",") {
			chain = append(chain, strings.TrimSpace(e))
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(chain[i])
		if err != nil {
			return peer // a hop wrote something that is not an address: don't guess
		}
		a = a.Unmap()
		if !s.Contains(a.String()) {
			return a.String()
		}
	}
	return peer
}

// Peer is the host part of RemoteAddr, for a message that tells the operator which address to list.
func Peer(remoteAddr string) string {
	if ap, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return ap.Addr().Unmap().String()
	}
	return remoteAddr
}
