// Package netguard is the HTTP client Vinaire uses whenever the *server* fetches
// something on a user's behalf (Helm repositories, OCI registries, git
// repositories): it refuses to connect to addresses that would turn the fetch
// into an attack on the machine Vinaire runs on or on its cloud — loopback,
// link-local (the cloud metadata service lives at 169.254.169.254), multicast
// and unspecified addresses. Private ranges are allowed: that is where
// self-hosted repositories live.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Forbidden reports addresses a server-side fetch must never reach.
func Forbidden(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast()
}

// Options relax the guard for local development and tests only.
type Options struct {
	// AllowLoopback permits connecting to 127.0.0.0/8 and ::1, and — because a
	// loopback test server rarely has TLS — redirects and URLs that aren't
	// https. Never set it on a real deployment.
	AllowLoopback bool
	// AllowHTTPOnPrivate permits plain http:// URLs — but only to a private address, decided on the
	// address actually connected to: RFC 1918, the carrier-grade range 100.64.0.0/10 (what NetBird and
	// Tailscale hand out) and IPv6 unique-local. An http:// URL that resolves anywhere else is
	// refused, so the setting cannot turn into plain text over the internet. For a home server on a
	// private network that has no certificate; the operator turns it on, a request never can.
	AllowHTTPOnPrivate bool
	// Timeout bounds one whole request (0 = 30s).
	Timeout time.Duration
}

var privateRanges = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"} {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

// IsPrivate reports addresses on a private network: RFC 1918, 100.64.0.0/10 and IPv6 unique-local.
// Loopback and link-local are not private here — Forbidden keeps refusing those.
func IsPrivate(ip net.IP) bool {
	if ip == nil || Forbidden(ip) {
		return false
	}
	for _, n := range privateRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// DialContext is a dial function with the same guard, for clients that don't go
// through net/http's transport (client-go's rest.Config.Dial). Loopback is
// allowed when allowLoopback is set: a Kubernetes API on 127.0.0.1 is a normal
// thing (k3d, kind), while link-local, multicast and unspecified never are.
func DialContext(allowLoopback bool) func(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("refusing to connect to %q", host)
			}
			if Forbidden(ip) && !(allowLoopback && ip.IsLoopback()) {
				return fmt.Errorf("refusing to connect to %s", ip)
			}
			return nil
		},
	}
	return d.DialContext
}

// Client returns an HTTP client with the guard. The check runs in the dialer's
// Control hook, on the address actually being connected to, so a hostname that
// resolves somewhere forbidden (or changes between lookups) can't get past it.
func Client(opts Options) *http.Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("refusing to connect to %q", host)
			}
			if Forbidden(ip) && !(opts.AllowLoopback && ip.IsLoopback()) {
				return fmt.Errorf("refusing to connect to %s", ip)
			}
			return nil
		},
	}
	newTransport := func(d *net.Dialer) *http.Transport {
		return &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           d.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
		}
	}
	tr := http.RoundTripper(newTransport(dialer))
	if opts.AllowHTTPOnPrivate {
		// plain http goes through a dialer that connects to private addresses only
		plain := &net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); !IsPrivate(ip) {
					return fmt.Errorf("refusing plain http to %s: only private-network addresses may be reached without TLS", host)
				}
				return nil
			},
		}
		tr = &schemeTransport{https: tr, http: newTransport(plain)}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && !opts.AllowLoopback {
				// plain http is only followed from plain http (never downgraded from https), and
				// the plain transport still connects to private addresses only
				if !(opts.AllowHTTPOnPrivate && req.URL.Scheme == "http" && len(via) > 0 && via[len(via)-1].URL.Scheme == "http") {
					return errors.New("refusing to follow a redirect away from https")
				}
			}
			return nil
		},
	}
}

// schemeTransport sends https requests through the ordinary guarded transport and plain http through
// one that reaches private addresses only.
type schemeTransport struct {
	https, http http.RoundTripper
}

func (t *schemeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" {
		return t.http.RoundTrip(req)
	}
	return t.https.RoundTrip(req)
}
