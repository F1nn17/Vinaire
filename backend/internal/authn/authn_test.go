package authn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func jwtExpiring(t *testing.T, at time.Time) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	payload, _ := json.Marshal(map[string]any{"exp": at.Unix(), "sub": "tester"})
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(payload) + "." + enc([]byte("sig"))
}

func newTestManager(t *testing.T, validate TokenValidator) *Manager {
	t.Helper()
	m, err := NewManager(context.Background(), ManagerConfig{AuthMode: "token", CookieSecure: true, CookieSameSite: "strict"})
	if err != nil {
		t.Fatal(err)
	}
	m.ValidateToken = validate
	return m
}

func login(m *Manager, token string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"token": token})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
	r.RemoteAddr = "203.0.113.7:51000"
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	m.HandleTokenLogin(w, r)
	return w
}

func sessionCookieOf(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func TestSessionIDsAreLongAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 2000 {
		id := NewSessionID()
		if len(id) < 43 { // 32 bytes, base64url
			t.Fatalf("session id too short: %q", id)
		}
		if seen[id] {
			t.Fatal("duplicate session id")
		}
		seen[id] = true
	}
}

func TestLoginCookieAttributesAndFreshID(t *testing.T) {
	m := newTestManager(t, func(context.Context, string) error { return nil })

	w := login(m, "good-token", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	c := sessionCookieOf(w)
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("session cookie = %+v", c)
	}
	if strings.Contains(w.Body.String(), "good-token") {
		t.Error("the token was echoed back")
	}

	// logging in again with an existing cookie issues a NEW id and drops the old session
	w2 := login(m, "good-token", func(r *http.Request) { r.AddCookie(c) })
	c2 := sessionCookieOf(w2)
	if c2 == nil || c2.Value == c.Value {
		t.Fatal("a fresh session id must be issued at every login (session fixation)")
	}
	if _, ok := m.Sessions.Get(c.Value); ok {
		t.Error("the previous session survived a new login")
	}
	if _, ok := m.Sessions.Get(c2.Value); !ok {
		t.Error("the new session doesn't exist")
	}
}

func TestInvalidTokenGetsNoSession(t *testing.T) {
	m := newTestManager(t, func(context.Context, string) error { return ErrInvalidToken })
	w := login(m, "bad-token", nil)
	if w.Code != http.StatusUnauthorized || sessionCookieOf(w) != nil {
		t.Fatalf("status %d, cookie %v", w.Code, sessionCookieOf(w))
	}
	if strings.Contains(w.Body.String(), "bad-token") {
		t.Error("the rejected token appears in the response")
	}
	ms := m.Sessions.(*MemoryStore)
	if len(ms.sessions) != 0 {
		t.Error("a rejected token created a session")
	}
}

func TestUnreachableClusterIsNotCountedAsAFailure(t *testing.T) {
	m := newTestManager(t, func(context.Context, string) error { return errors.New("dial tcp: i/o timeout") })
	for range 10 {
		if w := login(m, "tok", nil); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", w.Code)
		}
	}
}

func TestLoginThrottlePerTokenWithBackoff(t *testing.T) {
	invalid := true
	m := newTestManager(t, func(context.Context, string) error {
		if invalid {
			return ErrInvalidToken
		}
		return nil
	})
	now := time.Now()
	m.limiter.now = func() time.Time { return now }

	for i := 0; i < tokenFailLimit; i++ {
		if w := login(m, "guess", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w := login(m, "guess", nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("after %d failures: %d retry-after=%q", tokenFailLimit, w.Code, w.Header().Get("Retry-After"))
	}
	// a different token from the same address is still allowed (the address limit is far higher)
	if w := login(m, "another", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("a different token was blocked too: %d", w.Code)
	}

	// after the block, five more failures block for twice as long
	now = now.Add(baseBlock + time.Second)
	for i := 0; i < tokenFailLimit; i++ {
		login(m, "guess", nil)
	}
	blocked := login(m, "guess", nil)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("second block: %d", blocked.Code)
	}
	now = now.Add(baseBlock + time.Second) // the first block's length: still blocked
	if w := login(m, "guess", nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("the backoff didn't grow: %d", w.Code)
	}
	now = now.Add(baseBlock + time.Second) // past the doubled block
	invalid = false
	if w := login(m, "guess", nil); w.Code != http.StatusOK {
		t.Errorf("after the block a valid login should work: %d", w.Code)
	}
}

func TestLoginThrottlePerAddress(t *testing.T) {
	m := newTestManager(t, func(context.Context, string) error { return ErrInvalidToken })
	for i := 0; i < ipFailLimit; i++ {
		login(m, fmt.Sprintf("token-%d", i), nil)
	}
	if w := login(m, "yet-another", nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("the address wasn't throttled after %d failures: %d", ipFailLimit, w.Code)
	}
	// another address is unaffected
	if w := login(m, "yet-another", func(r *http.Request) { r.RemoteAddr = "198.51.100.9:1234" }); w.Code != http.StatusUnauthorized {
		t.Errorf("a different address was blocked: %d", w.Code)
	}
}

func TestClientIPOnlyTrustsForwardedForWhenTold(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	if got := clientIP(r, false); got != "10.0.0.1" {
		t.Errorf("untrusted: %s", got)
	}
	if got := clientIP(r, true); got != "203.0.113.50" {
		t.Errorf("trusted: %s", got)
	}
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := clientIP(r, true); got != "10.0.0.1" {
		t.Errorf("a garbage header must fall back to the peer: %s", got)
	}
}

func TestSessionLifetimeIsCappedAndExpires(t *testing.T) {
	m := newTestManager(t, func(context.Context, string) error { return nil })
	w := login(m, jwtExpiring(t, time.Now().Add(365*24*time.Hour)), nil) // a year-long ServiceAccount token
	c := sessionCookieOf(w)
	if c == nil {
		t.Fatal("no cookie")
	}
	if until := time.Until(c.Expires); until > defaultMaxSessionTTL+time.Minute {
		t.Errorf("session lasts %s, want at most %s", until, defaultMaxSessionTTL)
	}
	sess, ok := m.Sessions.Get(c.Value)
	if !ok || time.Until(sess.ExpiresAt) > defaultMaxSessionTTL+time.Minute {
		t.Errorf("stored session = %+v", sess)
	}

	// a short-lived token keeps its own, shorter, lifetime
	w = login(m, jwtExpiring(t, time.Now().Add(10*time.Minute)), nil)
	if until := time.Until(sessionCookieOf(w).Expires); until > 11*time.Minute {
		t.Errorf("short token: %s", until)
	}

	// an expired session is refused and removed
	ms := m.Sessions.(*MemoryStore)
	ms.Create(&Session{ID: "old", K8sToken: "x", ExpiresAt: time.Now().Add(-time.Second)})
	if _, ok := ms.Get("old"); ok {
		t.Error("an expired session was returned")
	}
}

func TestStoreBoundsAndPurgesExpired(t *testing.T) {
	ms := NewMemoryStore()
	for i := 0; i < maxSessions; i++ {
		if err := ms.Create(&Session{ID: fmt.Sprint(i), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.Create(&Session{ID: "overflow", ExpiresAt: time.Now().Add(time.Hour)}); !errors.Is(err, ErrTooManySessions) {
		t.Errorf("a full store accepted another session: %v", err)
	}
	// expired ones are purged to make room
	for i := 0; i < 10; i++ {
		ms.sessions[fmt.Sprint(i)].ExpiresAt = time.Now().Add(-time.Minute)
	}
	if err := ms.Create(&Session{ID: "fits-now", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Errorf("expired sessions weren't purged: %v", err)
	}
}

func TestLogoutInvalidatesTheSession(t *testing.T) {
	m := newTestManager(t, func(context.Context, string) error { return nil })
	c := sessionCookieOf(login(m, "tok", nil))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	m.HandleLogout(w, r)
	if _, ok := m.Sessions.Get(c.Value); ok {
		t.Error("the session survived logout")
	}
	cleared := sessionCookieOf(w)
	if cleared == nil || cleared.MaxAge >= 0 || !cleared.HttpOnly || !cleared.Secure {
		t.Errorf("logout cookie = %+v", cleared)
	}
	// and the cookie no longer authenticates a request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.AddCookie(c)
	if _, ok := m.Authenticate(req); ok {
		t.Error("a logged-out cookie still authenticates")
	}
}

func TestSameSiteDefaultsToStrict(t *testing.T) {
	for in, want := range map[string]http.SameSite{
		"": http.SameSiteStrictMode, "strict": http.SameSiteStrictMode, "garbage": http.SameSiteStrictMode,
		"lax": http.SameSiteLaxMode, "none": http.SameSiteNoneMode, "NONE": http.SameSiteNoneMode,
	} {
		if got := parseSameSite(in); got != want {
			t.Errorf("parseSameSite(%q) = %v, want %v", in, got, want)
		}
	}
}
