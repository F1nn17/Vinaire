package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shiraku/vinaire/backend/internal/clusters"
)

type fakeDocs struct {
	mu        sync.Mutex
	docs      map[string][]byte
	encrypted bool
}

func newFakeDocs(encrypted bool) *fakeDocs {
	return &fakeDocs{docs: map[string][]byte{}, encrypted: encrypted}
}

func (f *fakeDocs) Load(context.Context) ([]clusters.StoredCluster, error) { return nil, nil }
func (f *fakeDocs) Save(context.Context, []clusters.StoredCluster) error   { return nil }
func (f *fakeDocs) Kind() string                                           { return "fake" }
func (f *fakeDocs) Encrypted() bool                                        { return f.encrypted }
func (f *fakeDocs) LoadDoc(_ context.Context, name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.docs[name], nil
}
func (f *fakeDocs) SaveDoc(_ context.Context, name string, plain []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs[name] = append([]byte(nil), plain...)
	return nil
}

// a Keycloak with the admin API, the client secret, and OIDC discovery
func fakeKeycloakServer(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	clients := map[string]map[string]any{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/realms/master/protocol/openid-connect/token":
			_ = r.ParseForm()
			if r.Form.Get("password") != "kc-admin-pass" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"t"}`))
		case r.URL.Path == "/realms/vinaire/.well-known/openid-configuration":
			_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
				srv.URL+"/realms/vinaire", srv.URL+"/auth", srv.URL+"/token", srv.URL+"/jwks")
		case r.Method == "POST" && r.URL.Path == "/admin/realms":
			w.WriteHeader(http.StatusCreated)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/clients"):
			var out []map[string]any
			if c, ok := clients[r.URL.Query().Get("clientId")]; ok {
				out = append(out, c)
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/clients"):
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			b["id"] = "cid"
			clients[b["clientId"].(string)] = b
			w.Header().Set("Location", r.URL.Path+"/cid")
			w.WriteHeader(http.StatusCreated)
		case strings.HasSuffix(r.URL.Path, "/client-secret"):
			_, _ = w.Write([]byte(`{"value":"SUPER-SECRET-CLIENT-VALUE"}`))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/clients/cid"):
			_ = json.NewEncoder(w).Encode(clients["vinaire"])
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/clients/cid"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/users"):
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (hs *harness) post(handler http.HandlerFunc, method, token string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/x", bytes.NewReader(b))
	r.Header.Set("Origin", "https://vinaire.example.com")
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	}
	w := httptest.NewRecorder()
	hs.mgr.Middleware(handler).ServeHTTP(w, r)
	return w
}

func TestKeycloakSetupSwitchesSignInOnAndKeepsSecretsOutOfSight(t *testing.T) {
	hs := newHarness(t, "token")
	kc := fakeKeycloakServer(t)
	docs := newFakeDocs(true)
	hs.h.AuthDocs, hs.h.OIDCAllowInsecure, hs.h.ProvisionAllowInsecure = docs, true, true
	hs.api.grant("admin", "vinaire.io/auth/create", "vinaire.io/auth/get", "vinaire.io/auth/update", "vinaire.io/auth/delete")
	hs.api.grant("viewer", "/pods/list")

	body := map[string]any{
		"keycloakUrl": kc.URL, "adminUser": "admin", "adminPassword": "kc-admin-pass",
		"user": "alice", "userEmail": "alice@example.com", "userPassword": "alice-password-1", "allowTokenLogin": true,
	}
	if w := hs.post(hs.h.KeycloakSetup, "POST", "viewer", body); w.Code != http.StatusForbidden {
		t.Errorf("an ordinary user: %d %s", w.Code, w.Body)
	}
	if o, _ := hs.mgr.SignInMethods(); o {
		t.Fatal("OIDC on before anyone set it up")
	}

	// wrong admin password: nothing saved, nothing switched on
	bad := map[string]any{}
	for k, v := range body {
		bad[k] = v
	}
	bad["adminPassword"] = "nope"
	if w := hs.post(hs.h.KeycloakSetup, "POST", "admin", bad); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong admin password: %d %s", w.Code, w.Body)
	}
	if len(docs.docs) != 0 {
		t.Error("a failed setup saved something")
	}

	w := hs.post(hs.h.KeycloakSetup, "POST", "admin", body)
	if w.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	if o, tk := hs.mgr.SignInMethods(); !o || !tk {
		t.Errorf("after setup: oidc=%v token=%v", o, tk)
	}
	for _, leak := range []string{"kc-admin-pass", "alice-password-1", "SUPER-SECRET-CLIENT-VALUE"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("the response carries %q", leak)
		}
	}
	if !strings.Contains(w.Body.String(), "--oidc-issuer-url="+kc.URL+"/realms/vinaire") || !strings.Contains(w.Body.String(), "https://vinaire.example.com/api/v1/auth/oidc/callback") {
		t.Errorf("response lacks the issuer flag or the redirect: %s", w.Body)
	}
	// what is kept: the client secret (sealed by the store), never the admin's or the user's password
	saved := string(docs.docs["auth-oidc"])
	if !strings.Contains(saved, "SUPER-SECRET-CLIENT-VALUE") || strings.Contains(saved, "kc-admin-pass") || strings.Contains(saved, "alice-password-1") {
		t.Errorf("saved document: %s", saved)
	}

	// status never returns the secret
	w = hs.post(hs.h.KeycloakStatus, "GET", "admin", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "SUPER-SECRET") || !strings.Contains(w.Body.String(), `"active":true`) {
		t.Errorf("status: %d %s", w.Code, w.Body)
	}

	// token sign-in off, then the way back
	if w := hs.post(hs.h.KeycloakTokenLogin, "PUT", "admin", map[string]bool{"allow": false}); w.Code != 200 {
		t.Fatalf("token-login off: %d %s", w.Code, w.Body)
	}
	if _, tk := hs.mgr.SignInMethods(); tk {
		t.Error("token sign-in still open")
	}
	if w := hs.post(hs.h.KeycloakDisable, "DELETE", "admin", nil); w.Code != 200 {
		t.Fatalf("disable: %d %s", w.Code, w.Body)
	}
	if o, tk := hs.mgr.SignInMethods(); o || !tk {
		t.Errorf("after disabling: oidc=%v token=%v", o, tk)
	}
	if s, _ := hs.h.loadStoredOIDC(context.Background()); s != nil {
		t.Error("the saved client secret is still there after disabling")
	}

	// and it comes back after a restart
	if w := hs.post(hs.h.KeycloakSetup, "POST", "admin", body); w.Code != 200 {
		t.Fatalf("setup again: %d %s", w.Code, w.Body)
	}
	hs.mgr.DisableOIDC() // what a restart does
	hs.h.RestoreOIDC(context.Background())
	if o, _ := hs.mgr.SignInMethods(); !o {
		t.Error("the saved sign-in didn't come back")
	}
}

func TestKeycloakSetupRefusesWhatItShould(t *testing.T) {
	hs := newHarness(t, "token")
	kc := fakeKeycloakServer(t)
	hs.api.grant("admin", "vinaire.io/auth/create")
	body := map[string]any{"keycloakUrl": kc.URL, "adminUser": "admin", "adminPassword": "kc-admin-pass"}

	// no encrypted store -> no client secret kept in the clear
	hs.h.AuthDocs, hs.h.OIDCAllowInsecure, hs.h.ProvisionAllowInsecure = newFakeDocs(false), true, true
	if w := hs.post(hs.h.KeycloakSetup, "POST", "admin", body); w.Code != http.StatusConflict {
		t.Errorf("unencrypted store: %d %s", w.Code, w.Body)
	}
	// http Keycloak without the dev opt-out
	hs.h.AuthDocs, hs.h.OIDCAllowInsecure = newFakeDocs(true), false
	if w := hs.post(hs.h.KeycloakSetup, "POST", "admin", body); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "https") {
		t.Errorf("http Keycloak: %d %s", w.Code, w.Body)
	}
	// loopback is refused by the guarded client even with the opt-out missing for the dial
	hs.h.OIDCAllowInsecure = true
	body["realm"] = "../master"
	if w := hs.post(hs.h.KeycloakSetup, "POST", "admin", body); w.Code != http.StatusBadRequest {
		t.Errorf("a realm that is a path: %d %s", w.Code, w.Body)
	}
	// no storage at all
	hs.h.AuthDocs = nil
	if w := hs.post(hs.h.KeycloakSetup, "POST", "admin", map[string]any{"keycloakUrl": kc.URL, "adminUser": "a", "adminPassword": "b"}); w.Code != http.StatusServiceUnavailable {
		t.Errorf("no store: %d %s", w.Code, w.Body)
	}
	// disabled deployment
	hd := newHarness(t, "disabled")
	if w := hd.post(hd.h.KeycloakSetup, "POST", "admin", body); w.Code != http.StatusConflict {
		t.Errorf("AUTH_MODE=disabled: %d %s", w.Code, w.Body)
	}
}
