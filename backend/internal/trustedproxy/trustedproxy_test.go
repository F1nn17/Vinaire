package trustedproxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// Roadmap Phase 96: X-Forwarded-Proto is believed only from a proxy the operator named.

func req(remote string, proto ...string) *http.Request {
	r := httptest.NewRequest("POST", "/api/v1/nodes/provision", nil)
	r.RemoteAddr = remote
	for _, p := range proto {
		r.Header.Add("X-Forwarded-Proto", p)
	}
	return r
}

func mustParse(t *testing.T, list string) Set {
	t.Helper()
	s, _, err := Parse(list)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNobodyIsTrustedByDefaultSoAForgedHeaderCountsForNothing(t *testing.T) {
	var none Set
	if !none.Empty() || none.Contains("10.0.0.1:1234") {
		t.Fatal("the zero value trusts somebody")
	}
	if none.IsHTTPS(req("10.0.0.1:1234", "https")) {
		t.Fatal("a client claimed https and was believed")
	}
	if !none.IsHTTPS(&http.Request{TLS: &tls.ConnectionState{}}) {
		t.Fatal("a real TLS connection must always count")
	}
}

func TestOnlyTheListedProxyIsBelieved(t *testing.T) {
	s := mustParse(t, "10.42.0.15/32, 192.168.88.0/24")
	for remote, want := range map[string]bool{
		"10.42.0.15:5000":       true,
		"10.42.0.16:5000":       false, // next to the proxy is not the proxy
		"192.168.88.200:80":     true,
		"192.168.89.1:80":       false,
		"[::ffff:10.42.0.15]:9": true, // the same address written as IPv4-mapped IPv6
		"8.8.8.8:443":           false,
		"garbage":               false,
		"":                      false,
	} {
		if got := s.IsHTTPS(req(remote, "https")); got != want {
			t.Errorf("%s: %v, want %v", remote, got, want)
		}
	}
	// the proxy says http: not https
	if s.IsHTTPS(req("10.42.0.15:5000", "http")) || s.IsHTTPS(req("10.42.0.15:5000")) {
		t.Error("a trusted proxy that didn't say https was taken as https")
	}
}

func TestAClientCannotPrependHttpsToWhatTheProxyAppends(t *testing.T) {
	s := mustParse(t, "10.42.0.15")
	// the client sent "https"; the proxy appended what it saw ("http")
	if s.IsHTTPS(req("10.42.0.15:1", "https, http")) || s.IsHTTPS(req("10.42.0.15:1", "https", "http")) {
		t.Fatal("the client's own entry was read instead of the proxy's")
	}
	if !s.IsHTTPS(req("10.42.0.15:1", "http, HTTPS")) {
		t.Fatal("the proxy's own entry (the last one) was not read")
	}
}

func TestIPv6AndMappedAddresses(t *testing.T) {
	s := mustParse(t, "fd00::10/128, 10.1.2.3")
	if !s.Contains("[fd00::10]:443") || s.Contains("[fd00::11]:443") {
		t.Error("an IPv6 proxy")
	}
	if !s.Contains("[::ffff:10.1.2.3]:80") || !s.Contains("10.1.2.3") {
		t.Error("an IPv4 proxy written the other way")
	}
	if Peer("[::ffff:10.1.2.3]:80") != "10.1.2.3" {
		t.Errorf("peer: %q", Peer("[::ffff:10.1.2.3]:80"))
	}
}

func TestTheListIsValidatedAndTooWideEntriesAreNamed(t *testing.T) {
	for _, bad := range []string{"0.0.0.0/0", "::/0", "10.0.0.0/8, 0.0.0.0/0", "not-an-ip", "10.0.0.1/33", "10.0.0.1/abc", strings.Repeat("10.0.0.1,", 70)} {
		if _, _, err := Parse(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	s, warn, err := Parse("10.0.0.0/8\n172.16.0.0/12 192.168.88.10/32 fd00::/48")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(warn, ","); got != "10.0.0.0/8,172.16.0.0/12,fd00::/48" {
		t.Errorf("broad entries: %q", got)
	}
	if len(s.Broad()) != 3 || len(s.Strings()) != 4 {
		t.Errorf("%v %v", s.Broad(), s.Strings())
	}
	if e, w, err := Parse(""); err != nil || !e.Empty() || len(w) != 0 {
		t.Errorf("an empty list: %v %v %v", e, w, err)
	}
	// a host written with host bits set is normalised
	if n := mustParse(t, "10.1.2.3/24").Strings(); len(n) != 1 || n[0] != "10.1.2.0/24" {
		t.Errorf("normalised: %v", n)
	}
}

// What the parser must never accept: a list that trusts everything, and an address outside the list.
func FuzzParseTrustedProxies(f *testing.F) {
	for _, s := range []string{"", "10.0.0.1", "10.0.0.0/8,192.168.1.0/24", "::1", "fd00::/8", "0.0.0.0/0", "1.2.3.4/0", "a,b", "[::ffff:1.2.3.4]"} {
		f.Add(s, "8.8.8.8:80")
	}
	f.Fuzz(func(t *testing.T, list, remote string) {
		set, _, err := Parse(list)
		if err != nil {
			return
		}
		for _, p := range set.Strings() {
			if strings.HasSuffix(p, "/0") {
				t.Fatalf("a /0 was accepted: %q", p)
			}
		}
		// an address the list doesn't contain is never believed
		if set.Contains(remote) {
			ok := false
			for _, p := range set.prefixes {
				a := Peer(remote)
				if strings.Contains(a, "%") {
					a = a[:strings.Index(a, "%")]
				}
				if pa, perr := netip.ParseAddr(strings.Trim(a, "[]")); perr == nil && p.Contains(pa) {
					ok = true
				}
			}
			if !ok {
				t.Fatalf("%q was trusted by %v", remote, set.Strings())
			}
		}
		// an empty set never believes anyone
		if set.Empty() && set.IsHTTPS(req("10.0.0.1:1", "https")) {
			t.Fatal("an empty set believed a header")
		}
	})
}
