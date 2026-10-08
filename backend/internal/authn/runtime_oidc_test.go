package authn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// a provider that only answers discovery — enough to switch OIDC on
func fakeProvider(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/realms/vinaire/.well-known/openid-configuration" {
			_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
				srv.URL+"/realms/vinaire", srv.URL+"/auth", srv.URL+"/token", srv.URL+"/jwks")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTokenManager(t *testing.T) *Manager {
	m, err := NewManager(context.Background(), ManagerConfig{AuthMode: "token", CookieSameSite: "strict"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOIDCCanBeSwitchedOnAndOffAtRunTime(t *testing.T) {
	p := fakeProvider(t)
	m := newTokenManager(t)
	ctx := context.Background()

	if o, tk := m.SignInMethods(); o || !tk {
		t.Fatalf("start: oidc=%v token=%v", o, tk)
	}
	if err := m.SetTokenLogin(false); err == nil {
		t.Error("turning token sign-in off with no OIDC would lock everyone out")
	}
	// the OIDC sign-in route says no while it's off
	w := httptest.NewRecorder()
	m.HandleOIDCLogin(w, httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("oidc login while off: %d", w.Code)
	}

	// a broken provider leaves things as they were
	if err := m.ConfigureOIDC(ctx, http.DefaultClient, p.URL+"/realms/nope", "vinaire", "s", "https://v/cb", true); err == nil {
		t.Error("an unreachable realm was accepted")
	}
	if o, _ := m.SignInMethods(); o {
		t.Error("a failed setup switched OIDC on")
	}

	if err := m.ConfigureOIDC(ctx, http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", false); err != nil {
		t.Fatal(err)
	}
	if o, tk := m.SignInMethods(); !o || tk {
		t.Errorf("after setup with token sign-in off: oidc=%v token=%v", o, tk)
	}
	w = httptest.NewRecorder()
	m.HandleOIDCLogin(w, httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil))
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), p.URL+"/auth?") {
		t.Errorf("oidc login: %d -> %s", w.Code, w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	m.HandleTokenLogin(w, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"token":"x"}`)))
	if w.Code != http.StatusForbidden {
		t.Errorf("a pasted token is refused while turned off: %d", w.Code)
	}

	if err := m.SetTokenLogin(true); err != nil {
		t.Fatal(err)
	}
	if o, tk := m.SignInMethods(); !o || !tk {
		t.Errorf("both on: oidc=%v token=%v", o, tk)
	}

	m.DisableOIDC()
	if o, tk := m.SignInMethods(); o || !tk {
		t.Errorf("after disabling: oidc=%v token=%v", o, tk)
	}
}

func TestRuntimeOIDCOnlyInTokenMode(t *testing.T) {
	p := fakeProvider(t)
	m, err := NewManager(context.Background(), ManagerConfig{AuthMode: "disabled", CookieSameSite: "strict"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err == nil {
		t.Error("OIDC was switched on in disabled mode")
	}
	if o, tk := m.SignInMethods(); o || tk {
		t.Errorf("disabled mode has no sign-in: oidc=%v token=%v", o, tk)
	}
}
