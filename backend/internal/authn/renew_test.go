package authn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// providerControl steers the fake identity provider of the renewal tests.
type providerControl struct {
	mu             sync.Mutex
	refresh        string // "" ok, "invalid_grant", "down", "hang" (never answers until the caller gives up)
	strict         bool   // like a Keycloak that rotates: only the newest refresh token is accepted, a spent one is refused
	current        string // the newest refresh token (strict mode)
	refreshSubject string // the subject the refreshed ID token names ("" = the same person)
	noRefreshToken bool   // the code exchange gives no refresh token
	refreshCalls   atomic.Int32
	rotations      atomic.Int32
}

func (c *providerControl) set(f func(*providerControl)) { c.mu.Lock(); f(c); c.mu.Unlock() }

// renewalProvider is an OIDC provider that signs ID tokens and speaks the refresh-token grant.
func renewalProvider(t *testing.T) (*httptest.Server, *providerControl) {
	t.Helper()
	ctl := &providerControl{}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	idToken := func(sub string) string {
		raw, err := jwt.Signed(sig).Claims(jwt.Claims{
			Issuer: srv.URL + "/realms/vinaire", Subject: sub, Audience: jwt.Audience{"vinaire"},
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
			_ = r.ParseForm()
			w.Header().Set("Content-Type", "application/json")
			ctl.mu.Lock()
			mode, otherSub, noRT := ctl.refresh, ctl.refreshSubject, ctl.noRefreshToken
			ctl.mu.Unlock()
			if r.Form.Get("grant_type") == "refresh_token" {
				ctl.refreshCalls.Add(1)
				switch mode {
				case "invalid_grant":
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Session not active"}`))
					return
				case "hang":
					select {
					case <-r.Context().Done():
					case <-time.After(10 * time.Second):
					}
					return
				case "down":
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte("unavailable"))
					return
				}
				ctl.mu.Lock()
				if ctl.strict {
					if ctl.current == "" {
						ctl.current = "rt-1"
					}
					if r.Form.Get("refresh_token") != ctl.current {
						ctl.mu.Unlock()
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token is not active"}`))
						return
					}
				}
				ctl.mu.Unlock()
				sub := "sub-123"
				if otherSub != "" {
					sub = otherSub
				}
				n := ctl.rotations.Add(1)
				ctl.mu.Lock()
				ctl.current = fmt.Sprintf("rt-%d", n+1)
				ctl.mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a", "token_type": "Bearer", "id_token": idToken(sub), "refresh_token": fmt.Sprintf("rt-%d", n+1)})
				return
			}
			out := map[string]any{"access_token": "a", "token_type": "Bearer", "id_token": idToken("sub-123")}
			if !noRT {
				out["refresh_token"] = "rt-1"
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, ctl
}

// renewingBroker mints a new credential on every call and counts them.
type renewingBroker struct {
	logins, renewals atomic.Int32
	gone             atomic.Bool
	failRenew        atomic.Bool // the Kubernetes call that mints the new credential fails
	lastTTL          atomic.Int64
}

func (b *renewingBroker) token(prefix string, n int32, ttl time.Duration) BrokeredToken {
	if ttl <= 0 {
		ttl = 4 * time.Hour
	}
	b.lastTTL.Store(int64(ttl))
	return BrokeredToken{Token: fmt.Sprintf("%s-%d", prefix, n), Expiry: time.Now().Add(ttl), KubernetesUser: "system:serviceaccount:vinaire-users:oidc-shiraku"}
}
func (b *renewingBroker) TokenFor(_ context.Context, id Identity) (BrokeredToken, error) {
	return b.token("login", b.logins.Add(1), id.TokenTTL), nil
}
func (b *renewingBroker) RenewToken(ctx context.Context, id Identity) (BrokeredToken, error) {
	if b.gone.Load() {
		return BrokeredToken{}, ErrAccountGone
	}
	if err := ctx.Err(); err != nil { // a real client call does not run on a context that is already over
		return BrokeredToken{}, err
	}
	if b.failRenew.Load() {
		return BrokeredToken{}, errors.New("the Kubernetes API did not answer")
	}
	return b.token("renewed", b.renewals.Add(1), id.TokenTTL), nil
}

type renewalFixture struct {
	m      *Manager
	ctl    *providerControl
	broker *renewingBroker
	cookie *http.Cookie
	id     string
}

func newRenewalFixture(t *testing.T) *renewalFixture {
	t.Helper()
	p, ctl := renewalProvider(t)
	m := newTokenManager(t)
	b := &renewingBroker{}
	m.SetIdentityBroker(b)
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
		t.Fatal(err)
	}
	w := signIn(t, m)
	if w.Code != http.StatusFound {
		t.Fatalf("sign-in: %d %s", w.Code, w.Body)
	}
	f := &renewalFixture{m: m, ctl: ctl, broker: b}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			f.cookie, f.id = c, c.Value
		}
	}
	if f.cookie == nil {
		t.Fatal("no session cookie")
	}
	return f
}

func (f *renewalFixture) request() *http.Request {
	r := httptest.NewRequest("GET", "/api/v1/pods", nil)
	r.AddCookie(f.cookie)
	return r
}

// dueIn makes the session's credential run out in d (the session itself stays as it was).
func (f *renewalFixture) dueIn(t *testing.T, d time.Duration) {
	t.Helper()
	cur, ok := f.m.Sessions.Get(f.id)
	if !ok {
		t.Fatal("no session")
	}
	cp := *cur
	cp.TokenExpiresAt = time.Now().Add(d)
	if err := f.m.Sessions.Update(&cp); err != nil {
		t.Fatal(err)
	}
}

func (f *renewalFixture) validatedAgo(t *testing.T, d time.Duration) {
	t.Helper()
	cur, _ := f.m.Sessions.Get(f.id)
	cp := *cur
	cp.ValidatedAt = time.Now().Add(-d)
	if err := f.m.Sessions.Update(&cp); err != nil {
		t.Fatal(err)
	}
}

func TestAProviderWithARefreshTokenGivesAShortCredentialAndALongSession(t *testing.T) {
	f := newRenewalFixture(t)
	s, _ := f.m.Sessions.Get(f.id)
	if s.RefreshToken != "rt-1" || s.Subject != "sub-123" || s.Issuer == "" {
		t.Fatalf("the session lacks what renewal needs: %+v", s)
	}
	if time.Duration(f.broker.lastTTL.Load()) != RenewableTokenTTL {
		t.Errorf("the credential was asked for with lifetime %s, want %s", time.Duration(f.broker.lastTTL.Load()), RenewableTokenTTL)
	}
	if d := time.Until(s.TokenExpiresAt); d > RenewableTokenTTL || d < RenewableTokenTTL-time.Minute {
		t.Errorf("credential lasts %s", d)
	}
	if d := time.Until(s.ExpiresAt); d < 11*time.Hour {
		t.Errorf("the session lasts only %s although the credential is renewed — it must not end with the credential", d)
	}
}

func TestADueSessionIsRenewedAndKeepsGoing(t *testing.T) {
	f := newRenewalFixture(t)
	// plenty left: no renewal
	if _, ok := f.m.Authenticate(f.request()); !ok || f.broker.renewals.Load() != 0 || f.ctl.refreshCalls.Load() != 0 {
		t.Fatal("renewed with plenty of time left")
	}
	f.dueIn(t, time.Minute)
	sess, ok := f.m.Authenticate(f.request())
	if !ok {
		t.Fatal("a due session was refused")
	}
	if sess.K8sToken != "renewed-1" || f.broker.renewals.Load() != 1 || f.ctl.refreshCalls.Load() != 1 {
		t.Fatalf("not renewed: %+v (renewals %d, refreshes %d)", sess, f.broker.renewals.Load(), f.ctl.refreshCalls.Load())
	}
	if time.Until(sess.TokenExpiresAt) < 25*time.Minute {
		t.Errorf("the new credential lasts only %s", time.Until(sess.TokenExpiresAt))
	}
	if sess.RefreshToken != "rt-2" {
		t.Errorf("the rotated refresh token was not kept: %q", sess.RefreshToken)
	}
	// the stored session is the renewed one, and a second request doesn't renew again
	if _, ok := f.m.Authenticate(f.request()); !ok || f.broker.renewals.Load() != 1 {
		t.Errorf("renewed twice (%d)", f.broker.renewals.Load())
	}
}

func TestAPersonDisabledAtTheProviderLosesTheSessionAtTheNextRenewal(t *testing.T) {
	f := newRenewalFixture(t)
	f.ctl.set(func(c *providerControl) { c.refresh = "invalid_grant" })
	f.dueIn(t, time.Minute)
	if _, ok := f.m.Authenticate(f.request()); ok {
		t.Fatal("a person the provider no longer vouches for kept their session")
	}
	if _, ok := f.m.Sessions.Get(f.id); ok {
		t.Fatal("the ended session is still in the store")
	}
	if f.broker.renewals.Load() != 0 {
		t.Error("a credential was minted for a person the provider refused")
	}
}

func TestAnUnreachableProviderIsToleratedForTheGraceAndNotLonger(t *testing.T) {
	f := newRenewalFixture(t)
	f.ctl.set(func(c *providerControl) { c.refresh = "down" })
	f.dueIn(t, time.Minute)
	sess, ok := f.m.Authenticate(f.request())
	if !ok {
		t.Fatal("the session ended at the first sign of the provider being down")
	}
	if f.broker.renewals.Load() != 1 || sess.K8sToken != "renewed-1" {
		t.Errorf("within the grace the credential should still be renewed (Kubernetes needs no provider): %+v", sess)
	}
	// the provider has not said yes for longer than the grace period
	f.validatedAgo(t, RenewGrace+time.Minute)
	f.dueIn(t, time.Minute)
	if _, ok := f.m.Authenticate(f.request()); ok {
		t.Fatal("a session went on although the provider hasn't confirmed the person for longer than the grace period")
	}
}

func TestADeletedAccountEndsTheSessionAndRenewalNeverCreatesOne(t *testing.T) {
	f := newRenewalFixture(t)
	f.broker.gone.Store(true)
	f.dueIn(t, time.Minute)
	if _, ok := f.m.Authenticate(f.request()); ok {
		t.Fatal("a session went on after the person's account was deleted")
	}
}

func TestARefreshedTokenForSomeoneElseEndsTheSession(t *testing.T) {
	f := newRenewalFixture(t)
	f.ctl.set(func(c *providerControl) { c.refreshSubject = "someone-else" })
	f.dueIn(t, time.Minute)
	if _, ok := f.m.Authenticate(f.request()); ok {
		t.Fatal("the provider answered for another subject and the session went on")
	}
	if f.broker.renewals.Load() != 0 {
		t.Error("a credential was minted after a mismatch")
	}
}

func TestConcurrentRequestsShareOneRenewal(t *testing.T) {
	f := newRenewalFixture(t)
	f.dueIn(t, time.Minute)
	var wg sync.WaitGroup
	var denied atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := f.m.Authenticate(f.request()); !ok {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()
	if denied.Load() != 0 {
		t.Errorf("%d requests were refused during a renewal", denied.Load())
	}
	if got := f.ctl.refreshCalls.Load(); got != 1 {
		t.Errorf("the provider was asked %d times, want once (a rotated refresh token must not be used twice)", got)
	}
	if got := f.broker.renewals.Load(); got != 1 {
		t.Errorf("%d credentials were minted, want one", got)
	}
}

func TestWithoutARefreshTokenNothingIsRenewedAndTheSessionIsAsShortAsTheCredential(t *testing.T) {
	p, ctl := renewalProvider(t)
	ctl.set(func(c *providerControl) { c.noRefreshToken = true })
	m := newTokenManager(t)
	b := &renewingBroker{}
	m.SetIdentityBroker(b)
	if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
		t.Fatal(err)
	}
	w := signIn(t, m)
	s := sessionFrom(t, m, w)
	if s == nil || s.RefreshToken != "" {
		t.Fatalf("unexpected session: %+v", s)
	}
	if time.Duration(b.lastTTL.Load()) == RenewableTokenTTL {
		t.Error("a short credential was asked for although nothing can renew it")
	}
	if d := time.Until(s.ExpiresAt); d > 4*time.Hour+time.Minute {
		t.Errorf("the session outlives its credential: %s", d)
	}
}

func TestARenewedSessionIsSharedBetweenReplicasAndSealed(t *testing.T) {
	fast(t)
	kv := newFakeKV()
	a, b := NewSharedStore(kv, fakeSealer{}), NewSharedStore(kv, fakeSealer{})
	id := NewSessionID()
	first := &Session{ID: id, K8sToken: "cred-1", Username: "alice", ExpiresAt: time.Now().Add(time.Hour), RefreshToken: "REFRESH-SECRET-1",
		Subject: "sub-1", Issuer: "http://kc/realms/x", TokenExpiresAt: time.Now().Add(5 * time.Minute), ValidatedAt: time.Now()}
	if err := a.Create(first); err != nil {
		t.Fatal(err)
	}
	renewed := *first
	renewed.K8sToken, renewed.RefreshToken, renewed.TokenExpiresAt = "cred-2", "REFRESH-SECRET-2", time.Now().Add(30*time.Minute)
	if err := a.Update(&renewed); err != nil {
		t.Fatal(err)
	}
	// the other replica sees the renewed session, with everything renewal needs
	got, ok := b.Get(id)
	if !ok || got.K8sToken != "cred-2" || got.RefreshToken != "REFRESH-SECRET-2" || got.Subject != "sub-1" || got.Issuer != "http://kc/realms/x" ||
		time.Until(got.TokenExpiresAt) < 25*time.Minute || got.ValidatedAt.IsZero() {
		t.Fatalf("replica B: %+v %v", got, ok)
	}
	// the refresh token is sealed like the credential
	for k, v := range kv.data {
		if bytes.Contains(v, []byte("REFRESH-SECRET")) || bytes.Contains(v, []byte("cred-")) {
			t.Errorf("the shared store holds a readable secret under %q", k)
		}
	}
	// a session that has ended can't be brought back by an update
	a.Delete(id)
	if err := NewSharedStore(kv, fakeSealer{}).Update(&renewed); !errors.Is(err, ErrNoSession) {
		t.Errorf("updating an ended session: %v", err)
	}
}

func TestTheSessionsListNeverShowsASecret(t *testing.T) {
	f := newRenewalFixture(t)
	l, ok := f.m.Sessions.(Lister)
	if !ok {
		t.Skip("the store has no list")
	}
	for _, info := range l.List() {
		b, _ := json.Marshal(info)
		if strings.Contains(string(b), "rt-1") || strings.Contains(string(b), "login-1") {
			t.Errorf("the sessions list shows a secret: %s", b)
		}
	}
}

func TestTheConfiguredCredentialLifetimeIsKeptWithinWhatMakesSense(t *testing.T) {
	m := newTokenManager(t)
	for in, want := range map[time.Duration]time.Duration{
		0: RenewableTokenTTL, time.Minute: 10 * time.Minute, 10 * time.Minute: 10 * time.Minute,
		20 * time.Minute: 20 * time.Minute, 5 * time.Hour: 2 * time.Hour,
	} {
		m.TokenTTL = in
		if got := m.tokenTTL(); got != want {
			t.Errorf("TokenTTL %s -> %s, want %s", in, got, want)
		}
	}
	m.TokenTTL = 30 * time.Minute
	if m.renewLead() != 10*time.Minute {
		t.Errorf("a 30 minute credential should be renewed in its last 10 minutes, got %s", m.renewLead())
	}
}

// Roadmap Phase 94, defect 1: one deadline used to cover the call to the provider AND the minting of the Kubernetes
// credential, so a provider that never answered used the whole time up and the credential could not be minted — the
// person lost their session although the grace period had hours left.
func TestAProviderThatNeverAnswersDoesNotUseUpTheTimeForTheKubernetesCredential(t *testing.T) {
	old := renewTimeout
	renewTimeout = 300 * time.Millisecond
	t.Cleanup(func() { renewTimeout = old })
	f := newRenewalFixture(t)
	f.ctl.set(func(c *providerControl) { c.refresh = "hang" })
	f.dueIn(t, time.Minute)
	start := time.Now()
	sess, ok := f.m.Authenticate(f.request())
	if !ok {
		t.Fatal("the session ended because the provider timed out — within the grace period it must go on")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the request waited %s for a provider that never answers", time.Since(start))
	}
	if f.broker.renewals.Load() != 1 || sess.K8sToken != "renewed-1" {
		t.Fatalf("the Kubernetes credential was not renewed after the provider timed out: %+v (renewals %d)", sess, f.broker.renewals.Load())
	}
}

// Roadmap Phase 94, defect 2: the provider rotates the refresh token on every use. If the Kubernetes credential then
// could not be minted, the new refresh token was dropped with the unsaved session, and the next renewal presented the
// spent one: invalid_grant, session over.
func TestARotatedRefreshTokenIsKeptEvenWhenTheKubernetesCredentialCouldNotBeMinted(t *testing.T) {
	f := newRenewalFixture(t)
	f.ctl.set(func(c *providerControl) { c.strict = true })
	f.broker.failRenew.Store(true)
	f.dueIn(t, time.Minute)
	if _, ok := f.m.Authenticate(f.request()); !ok {
		t.Fatal("the session ended although the current credential still works")
	}
	stored, _ := f.m.Sessions.Get(f.id)
	if stored == nil || stored.RefreshToken != "rt-2" {
		t.Fatalf("the rotated refresh token was lost: %+v", stored)
	}
	// Kubernetes is back: the next renewal presents the NEW token, which the provider accepts
	f.broker.failRenew.Store(false)
	f.dueIn(t, time.Minute)
	sess, ok := f.m.Authenticate(f.request())
	if !ok || f.broker.renewals.Load() != 1 || sess.K8sToken != "renewed-1" {
		t.Fatalf("the next renewal failed: ok=%v renewals=%d %+v", ok, f.broker.renewals.Load(), sess)
	}
	if sess.RefreshToken != "rt-3" {
		t.Errorf("the second rotation was not kept: %q", sess.RefreshToken)
	}
}

// A renewal belongs to the session, not to the request that started it.
func TestARenewalIsNotCancelledWithTheRequestThatStartedIt(t *testing.T) {
	f := newRenewalFixture(t)
	f.ctl.set(func(c *providerControl) { c.strict = true })
	f.dueIn(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the client went away before the renewal began
	cur, _ := f.m.Sessions.Get(f.id)
	if out, ok := f.m.renewIfDue(ctx, cur); !ok || out.K8sToken != "renewed-1" {
		t.Fatalf("a request that was already gone cancelled the renewal: ok=%v %+v", ok, out)
	}
	if stored, _ := f.m.Sessions.Get(f.id); stored == nil || stored.RefreshToken != "rt-2" {
		t.Fatalf("the rotated refresh token was not saved: %+v", stored)
	}
}
