package api

import (
	"github.com/f1nn17/vinaire/backend/internal/trustedproxy"
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqFrom(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest("POST", "http://vinaire.home/api/v1/auth/keycloak/setup", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientOnPrivateNetwork(t *testing.T) {
	for name, tc := range map[string]struct {
		r    *http.Request
		want bool
	}{
		"a LAN client":                          {reqFrom("192.168.1.20:5000"), true},
		"a NetBird peer":                        {reqFrom("100.80.1.2:5000"), true},
		"a public client":                       {reqFrom("203.0.113.9:5000"), false},
		"a proxy on the LAN, client public":     {reqFrom("10.42.0.5:5000", "203.0.113.9"), false},
		"a proxy on the LAN, client on the LAN": {reqFrom("10.42.0.5:5000", "192.168.1.20"), true},
		"a chain that ends public":              {reqFrom("10.42.0.5:5000", "192.168.1.20, 198.51.100.7"), false},
		"a garbage forwarded address":           {reqFrom("10.42.0.5:5000", "not-an-ip"), false},
		"loopback is not private":               {reqFrom("127.0.0.1:5000"), false},
		"no port in the peer address":           {reqFrom("192.168.1.20"), false},
	} {
		if got := clientOnPrivateNetwork(tc.r); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

// Without the operator's opt-in the administrator's password still needs TLS, whoever asks.
func TestKeycloakCredentialsNeedTLSUnlessThePrivateNetworkOptInIsOn(t *testing.T) {
	off := &Handler{}
	w := httptest.NewRecorder()
	if off.requireSecureTransportOrPrivate(w, reqFrom("192.168.1.20:5000")) {
		t.Fatal("plain http from a LAN client must be refused when the opt-in is off")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d", w.Code)
	}

	on := &Handler{OIDCAllowHTTPPrivate: true}
	if !on.requireSecureTransportOrPrivate(httptest.NewRecorder(), reqFrom("192.168.1.20:5000")) {
		t.Error("plain http from a LAN client should be accepted with the opt-in")
	}
	if on.requireSecureTransportOrPrivate(httptest.NewRecorder(), reqFrom("203.0.113.9:5000")) {
		t.Error("plain http from a public client must still be refused")
	}
	if on.requireSecureTransportOrPrivate(httptest.NewRecorder(), reqFrom("10.42.0.5:5000", "203.0.113.9")) {
		t.Error("a public client behind a LAN proxy must still be refused")
	}
	// TLS is accepted either way
	tlsReq := reqFrom("203.0.113.9:5000")
	tlsReq.Header.Set("X-Forwarded-Proto", "https")
	if off.requireSecureTransportOrPrivate(httptest.NewRecorder(), tlsReq) {
		t.Error("a client's own X-Forwarded-Proto: https must not be believed (Roadmap Phase 96)")
	}
	trusted, _, err := trustedproxy.Parse("203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	behind := &Handler{TrustedProxies: trusted}
	if !behind.requireSecureTransportOrPrivate(httptest.NewRecorder(), tlsReq) {
		t.Error("https via a proxy the operator named must be accepted")
	}
}
