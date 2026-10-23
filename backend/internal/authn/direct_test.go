package authn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Roadmap Phase 85: Vinaire's own sign-in form.

type directFixture struct {
	m      *Manager
	ctl    *providerControl
	broker *renewingBroker
	mu     sync.Mutex
	audit  []string
}

func newDirectFixture(t *testing.T, direct bool) *directFixture {
	t.Helper()
	p, ctl := renewalProvider(t)
	m := newTokenManager(t)
	f := &directFixture{m: m, ctl: ctl, broker: &renewingBroker{}}
	m.SetIdentityBroker(f.broker)
	m.Audit = func(action, name, identity string, detail map[string]any) {
		b, _ := json.Marshal(detail)
		f.mu.Lock()
		f.audit = append(f.audit, action+" "+name+" "+identity+" "+string(b))
		f.mu.Unlock()
	}
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
		t.Fatal(err)
	}
	m.SetDirectLogin(direct)
	return f
}

func (f *directFixture) post(body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/auth/oidc/password", strings.NewReader(body))
	r.RemoteAddr = "192.168.1.20:5000"
	w := httptest.NewRecorder()
	f.m.HandleOIDCPassword(w, r)
	return w
}

func (f *directFixture) everything() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.audit, "\n")
}

func loginBody(user, pass string) string {
	b, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	return string(b)
}

func TestARightPasswordStartsTheSameSessionAsTheRedirectFlow(t *testing.T) {
	f := newDirectFixture(t, true)
	w := f.post(loginBody("shiraku", "right-password"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"redirect"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	s := sessionFrom(t, f.m, w)
	if s == nil || s.Username != "shiraku" || s.Subject != "sub-123" || s.RefreshToken != "rt-1" || s.KubernetesUser == "" {
		t.Fatalf("not the session the redirect flow makes: %+v", s)
	}
	if time.Until(s.ExpiresAt) < 11*time.Hour {
		t.Errorf("a renewable session lasts only %s", time.Until(s.ExpiresAt))
	}
	// the client authenticated itself to the provider, and the password went only there
	form := f.ctl.lastPassword
	if form.Get("client_id") != "vinaire" || form.Get("client_secret") != "s" || form.Get("grant_type") != "password" {
		t.Errorf("the grant was not made as the client: %v", form)
	}
	if !strings.Contains(f.everything(), "oidc-login shiraku shiraku") || !strings.Contains(f.everything(), `"method":"password"`) {
		t.Errorf("the sign-in was not audited: %s", f.everything())
	}
	if strings.Contains(w.Body.String()+f.everything(), "right-password") {
		t.Fatal("the password reached a response or the audit trail")
	}
}

func TestAWrongPasswordIsRefusedWithFixedTextAndAudited(t *testing.T) {
	f := newDirectFixture(t, true)
	w := f.post(loginBody("shiraku", "SECRET-WRONG-ATTEMPT"))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "wrong username or password") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if sessionFrom(t, f.m, w) != nil {
		t.Fatal("a session was made for a wrong password")
	}
	if !strings.Contains(f.everything(), "oidc-password-login-failed") {
		t.Errorf("a failure was not audited: %s", f.everything())
	}
	for _, leak := range []string{"SECRET-WRONG-ATTEMPT", "Invalid user credentials", `"shiraku"`} {
		if strings.Contains(w.Body.String()+f.everything(), leak) {
			t.Errorf("%q reached a response or the audit trail (the username is recorded only as a hash)", leak)
		}
	}
}

func TestRepeatedFailuresAreThrottledPerUsername(t *testing.T) {
	f := newDirectFixture(t, true)
	for i := 0; i < tokenFailLimit; i++ {
		if w := f.post(loginBody("shiraku", "nope")); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	calls := f.ctl.rotations.Load()
	w := f.post(loginBody("shiraku", "right-password"))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("after %d failures: %d %s", tokenFailLimit, w.Code, w.Body)
	}
	_ = calls
	// another person is not affected by it
	if w := f.post(loginBody("someone-else", "nope")); w.Code != http.StatusUnauthorized {
		t.Errorf("a different username was blocked too: %d", w.Code)
	}
}

func TestAnAccountThatMustFinishSettingUpIsSentToTheProviderPageAndNotCountedAsAFailure(t *testing.T) {
	f := newDirectFixture(t, true)
	f.ctl.set(func(c *providerControl) { c.passwordMode = "needs_setup" })
	for i := 0; i < tokenFailLimit+3; i++ { // the password is right: it must never lock the person out
		w := f.post(loginBody("shiraku", "right-password"))
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"needs_provider_page"`) {
			t.Fatalf("attempt %d: %d %s", i, w.Code, w.Body)
		}
	}
}

func TestAClientWithoutDirectAccessIsReportedAsSuch(t *testing.T) {
	f := newDirectFixture(t, true)
	f.ctl.set(func(c *providerControl) { c.passwordMode = "not_enabled" })
	w := f.post(loginBody("shiraku", "right-password"))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"direct_not_enabled"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestAnUnreachableProviderIsABadGatewayNotAWrongPassword(t *testing.T) {
	f := newDirectFixture(t, true)
	f.ctl.set(func(c *providerControl) { c.passwordMode = "down" })
	for i := 0; i < tokenFailLimit+2; i++ {
		if w := f.post(loginBody("shiraku", "right-password")); w.Code != http.StatusBadGateway {
			t.Fatalf("attempt %d: %d %s", i, w.Code, w.Body)
		}
	}
}

func TestTheFormIsOffUnlessAnAdministratorTurnedItOn(t *testing.T) {
	f := newDirectFixture(t, false)
	if w := f.post(loginBody("shiraku", "right-password")); w.Code != http.StatusBadRequest {
		t.Fatalf("form off: %d %s", w.Code, w.Body)
	}
	if f.ctl.lastPassword != nil {
		t.Error("the provider was asked although the form is off")
	}
	cfg := httptest.NewRecorder()
	f.m.HandleConfig(cfg, httptest.NewRequest("GET", "/api/v1/auth/config", nil))
	if !strings.Contains(cfg.Body.String(), `"oidcDirect":false`) {
		t.Errorf("config: %s", cfg.Body)
	}
	f.m.SetDirectLogin(true)
	cfg = httptest.NewRecorder()
	f.m.HandleConfig(cfg, httptest.NewRequest("GET", "/api/v1/auth/config", nil))
	if !strings.Contains(cfg.Body.String(), `"oidcDirect":true`) {
		t.Errorf("config with the form on: %s", cfg.Body)
	}
	// turning OIDC off turns the form off with it
	f.m.DisableOIDC()
	if f.m.DirectLoginOn() {
		t.Error("the form outlived the Keycloak setting")
	}
}

func TestAPasswordOnAnInsecureRequestIsNeverSentAnywhere(t *testing.T) {
	f := newDirectFixture(t, true)
	f.m.PasswordTransportOK = func(*http.Request) bool { return false }
	w := f.post(loginBody("shiraku", "right-password"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "HTTPS") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if f.ctl.lastPassword != nil {
		t.Fatal("a password from an insecure request was forwarded to the provider")
	}
}

func TestBadRequestsNeverReachTheProvider(t *testing.T) {
	f := newDirectFixture(t, true)
	for name, body := range map[string]string{
		"not json":      "nope",
		"empty":         `{}`,
		"no password":   `{"username":"shiraku"}`,
		"newline in id": `{"username":"a\nb","password":"x"}`,
		"huge username": `{"username":"` + strings.Repeat("u", 300) + `","password":"x"}`,
		"huge password": `{"username":"u","password":"` + strings.Repeat("p", 2000) + `"}`,
		"huge body":     `{"username":"u","password":"x","pad":"` + strings.Repeat("z", 5000) + `"}`,
		"huge code":     `{"username":"u","password":"x","code":"` + strings.Repeat("1", 40) + `"}`,
	} {
		if w := f.post(body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if f.ctl.lastPassword != nil {
		t.Error("an invalid request reached the provider")
	}
}

func TestAOneTimeCodeIsPassedOnAsTotp(t *testing.T) {
	f := newDirectFixture(t, true)
	body, _ := json.Marshal(map[string]string{"username": "shiraku", "password": "right-password", "code": "123456"})
	if w := f.post(string(body)); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if f.ctl.lastPassword.Get("totp") != "123456" {
		t.Errorf("the code was not passed on: %v", f.ctl.lastPassword)
	}
}

func TestTheProviderTextIsClassifiedNeverPassedOn(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{401, `{"error":"invalid_grant","error_description":"Invalid user credentials"}`, errWrongCredentials},
		{400, `{"error":"invalid_grant","error_description":"Account disabled"}`, errWrongCredentials},
		{400, `{"error":"invalid_grant","error_description":"Account is not fully set up"}`, errNeedsProviderPage},
		{400, `{"error":"invalid_grant","error_description":"Resolve required actions first"}`, errNeedsProviderPage},
		{401, `{"error":"unauthorized_client","error_description":"Client not allowed for direct access grants"}`, errDirectNotAvailable},
		{500, `<html>boom</html>`, errProviderDown},
		{200, `{"access_token":"a"}`, errProviderDown},
	} {
		if _, err := classifyPasswordResponse(tc.status, []byte(tc.body)); err != tc.want {
			t.Errorf("%d %s -> %v, want %v", tc.status, tc.body, err, tc.want)
		}
	}
}
