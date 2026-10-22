package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// signingProvider is an OIDC provider that really signs: discovery, keys, and a token endpoint that answers
// any code with an ID token for subject "sub-123" / user "shiraku".
func signingProvider(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	idToken := func() string {
		raw, err := jwt.Signed(sig).Claims(jwt.Claims{
			Issuer: srv.URL + "/realms/vinaire", Subject: "sub-123", Audience: jwt.Audience{"vinaire"},
			IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		}).Claims(map[string]any{"preferred_username": "shiraku"}).Serialize()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/vinaire/.well-known/openid-configuration":
			_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q,"id_token_signing_alg_values_supported":["RS256"]}`,
				srv.URL+"/realms/vinaire", srv.URL+"/auth", srv.URL+"/token", srv.URL+"/jwks")
		case "/jwks":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a", "token_type": "Bearer", "id_token": idToken()})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, idToken()
}

type stubBroker struct {
	err  error
	seen Identity
}

func (s *stubBroker) TokenFor(_ context.Context, id Identity) (BrokeredToken, error) {
	s.seen = id
	if s.err != nil {
		return BrokeredToken{}, s.err
	}
	return BrokeredToken{Token: "sa-token", Expiry: time.Now().Add(time.Hour), KubernetesUser: "system:serviceaccount:vinaire-users:oidc-shiraku"}, nil
}

// signIn runs the browser's part of the flow against m and returns the callback's response.
func signIn(t *testing.T, m *Manager) *httptest.ResponseRecorder {
	t.Helper()
	login := httptest.NewRecorder()
	m.HandleOIDCLogin(login, httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil))
	loc, err := url.Parse(login.Header().Get("Location"))
	if err != nil || loc.Query().Get("state") == "" {
		t.Fatalf("no redirect to the provider: %d %q", login.Code, login.Header().Get("Location"))
	}
	cb := httptest.NewRequest("GET", "/api/v1/auth/oidc/callback?code=abc&state="+loc.Query().Get("state"), nil)
	for _, c := range login.Result().Cookies() {
		cb.AddCookie(c)
	}
	w := httptest.NewRecorder()
	m.HandleOIDCCallback(w, cb)
	return w
}

func sessionFrom(t *testing.T, m *Manager, w *httptest.ResponseRecorder) *Session {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			if s, ok := m.Sessions.Get(c.Value); ok {
				return s
			}
		}
	}
	return nil
}

func TestWithoutABrokerTheIDTokenIsTheBearer(t *testing.T) {
	p, _ := signingProvider(t)
	m := newTokenManager(t)
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
		t.Fatal(err)
	}
	w := signIn(t, m)
	if w.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", w.Code, w.Body)
	}
	s := sessionFrom(t, m, w)
	if s == nil || s.K8sToken == "" || s.K8sToken == "sa-token" || s.KubernetesUser != "" || s.Username != "shiraku" {
		t.Fatalf("unexpected session: %+v", s)
	}
}

func TestWithABrokerTheSessionHoldsTheBrokeredCredentialNotTheIDToken(t *testing.T) {
	p, _ := signingProvider(t)
	m := newTokenManager(t)
	b := &stubBroker{}
	m.SetIdentityBroker(b)
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
		t.Fatal(err)
	}
	w := signIn(t, m)
	if w.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", w.Code, w.Body)
	}
	s := sessionFrom(t, m, w)
	if s == nil || s.K8sToken != "sa-token" || s.KubernetesUser != "system:serviceaccount:vinaire-users:oidc-shiraku" || s.Username != "shiraku" {
		t.Fatalf("unexpected session: %+v", s)
	}
	if b.seen.Subject != "sub-123" || b.seen.Username != "shiraku" {
		t.Errorf("the broker was told %+v", b.seen)
	}
	if time.Until(s.ExpiresAt) < 30*time.Minute {
		t.Errorf("the session ends in %s although the credential lasts an hour — it must not be tied to the ID token's five minutes", time.Until(s.ExpiresAt))
	}
}

func TestWhenTheBrokerFailsThereIsNoSession(t *testing.T) {
	p, _ := signingProvider(t)
	m := newTokenManager(t)
	m.SetIdentityBroker(&stubBroker{err: errors.New("forbidden")})
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
		t.Fatal(err)
	}
	w := signIn(t, m)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", w.Code)
	}
	if sessionFrom(t, m, w) != nil {
		t.Fatal("a session was created although no Kubernetes credential could be obtained")
	}
}

func TestDisablingOIDCDropsTheBroker(t *testing.T) {
	p, _ := signingProvider(t)
	m := newTokenManager(t)
	m.SetIdentityBroker(&stubBroker{})
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
		t.Fatal(err)
	}
	m.DisableOIDC()
	if m.IdentityBroker() != nil {
		t.Fatal("the broker outlived the Keycloak setting")
	}
}
