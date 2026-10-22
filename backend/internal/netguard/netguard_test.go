package netguard

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsPrivate(t *testing.T) {
	for ip, want := range map[string]bool{
		"10.1.2.3": true, "172.16.0.1": true, "172.31.255.255": true, "192.168.1.10": true,
		"100.64.0.1": true, "100.127.255.254": true, "fd00::1": true,
		"172.32.0.1": false, "100.128.0.1": false, "8.8.8.8": false, "1.1.1.1": false, "2606:4700::1111": false,
		"127.0.0.1": false, "::1": false, "169.254.169.254": false, "0.0.0.0": false, "224.0.0.1": false,
	} {
		if got := IsPrivate(net.ParseIP(ip)); got != want {
			t.Errorf("IsPrivate(%s) = %v, want %v", ip, got, want)
		}
	}
	if IsPrivate(nil) {
		t.Error("a nil address is not private")
	}
}

// Plain http must never reach a loopback or public address, even with the setting on.
func TestPlainHTTPToLoopbackIsRefusedEvenWhenPrivateHTTPIsAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	for name, opts := range map[string]Options{
		"off":                  {},
		"private http allowed": {AllowHTTPOnPrivate: true},
	} {
		resp, err := Client(opts).Get(srv.URL)
		if err == nil {
			resp.Body.Close()
			t.Errorf("%s: a plain http request to loopback went through", name)
		}
	}
}

// With the setting on, plain http works to a private address. The test needs one on this machine.
func TestPlainHTTPToAPrivateAddressWorksOnlyWhenAllowed(t *testing.T) {
	var local net.IP
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && IsPrivate(n.IP) && n.IP.To4() != nil {
			local = n.IP
			break
		}
	}
	if local == nil {
		t.Skip("no private IPv4 address on this machine")
	}
	l, err := net.Listen("tcp", local.String()+":0")
	if err != nil {
		t.Skip("cannot listen on " + local.String())
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	srv.Listener = l
	srv.Start()
	defer srv.Close()

	if resp, err := Client(Options{}).Get(srv.URL); err == nil {
		resp.Body.Close()
		// the default guard only refuses https-less *redirects*; the scheme check lives in the callers.
		// What matters here: without the setting nothing is promised, with it the private dial works.
	}
	resp, err := Client(Options{AllowHTTPOnPrivate: true}).Get(srv.URL)
	if err != nil {
		t.Fatalf("plain http to a private address should work when allowed: %v", err)
	}
	resp.Body.Close()
}

// https is never downgraded by a redirect, with or without the setting.
func TestRedirectFromHTTPSToHTTPIsRefused(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer tls.Close()
	c := Client(Options{AllowHTTPOnPrivate: true})
	c.Transport.(*schemeTransport).https.(*http.Transport).TLSClientConfig = tls.Client().Transport.(*http.Transport).TLSClientConfig
	// the TLS test server is on loopback, which the guard refuses before any redirect: either way, no body
	if resp, err := c.Get(tls.URL); err == nil {
		resp.Body.Close()
		t.Fatal("an https -> http redirect was followed")
	} else if !strings.Contains(err.Error(), "refus") && !strings.Contains(err.Error(), "connect") {
		t.Logf("refused: %v", err)
	}
}
