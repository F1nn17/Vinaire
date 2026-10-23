package authn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Vinaire's own sign-in form (Roadmap Phase 85 — the owner's decision of 2026-10-23, which makes an exception to
// Phase 47's "no password grant").
//
// By default signing in through Keycloak sends the browser to Keycloak's own page (authorization code + PKCE).
// The owner does not want that hop, so when an administrator switches it on, Vinaire shows its own username and
// password form and passes what is typed to Keycloak's token endpoint (the "direct access" or password grant) —
// then carries on exactly as after the redirect flow (finishSignIn). What that costs, and how it is kept small:
//
//   - the password passes through Vinaire. It is read from one request body, bounded, sent to the provider and
//     dropped; it is never stored, logged, audited or put in an error, and the route accepts it only over TLS or,
//     if the operator allowed that, on a private network (PasswordTransportOK);
//   - the provider's own protections see Vinaire's address, not the person's, so Vinaire counts failures itself,
//     per address and per username, and blocks with a growing delay before the provider's lockout can be used to
//     lock a person out;
//   - things only the provider's pages can do — changing a temporary password, a required action — are not done
//     here: such an account is told to sign in on the provider's page once (the redirect flow stays available);
//   - no single sign-on cookie is set at the provider.

// maxDirectBody bounds the request: a username, a password and a code are small.
const maxDirectBody = 4 << 10

// Why a password sign-in failed — a small set, so the answer to the browser is fixed text.
var (
	errWrongCredentials   = errors.New("wrong username or password")
	errNeedsProviderPage  = errors.New("this account has to finish setting up on the provider's own page")
	errDirectNotAvailable = errors.New("signing in with a password is not enabled for this client at the provider")
	errProviderDown       = errors.New("the identity provider couldn't be reached")
)

// SetDirectLogin turns Vinaire's own sign-in form on or off (it needs OIDC to be configured).
func (m *Manager) SetDirectLogin(on bool) {
	m.oidcMu.Lock()
	m.directLogin = on
	m.oidcMu.Unlock()
}

// DirectLoginOn says whether the sign-in form is offered.
func (m *Manager) DirectLoginOn() bool {
	m.oidcMu.RLock()
	defer m.oidcMu.RUnlock()
	return m.directLogin && m.oidc != nil
}

type directLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// Code is the one-time code of a person who has one (optional).
	Code string `json:"code"`
}

// HandleOIDCPassword handles POST /api/v1/auth/oidc/password {"username","password","code"?}: the form's
// sign-in. 200 {"redirect": "..."} with the session cookie set, or an error whose text is fixed.
func (m *Manager) HandleOIDCPassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	oc := m.getOIDC()
	if m.Mode == ModeDisabled || oc == nil || !m.DirectLoginOn() {
		respondError(w, http.StatusBadRequest, "signing in with a password here is not turned on")
		return
	}
	if m.PasswordTransportOK != nil && !m.PasswordTransportOK(r) {
		respondError(w, http.StatusBadRequest, "a password may only be sent over HTTPS (or, if the operator allowed it, from a private network) — open Vinaire through a TLS address")
		return
	}
	var req directLoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDirectBody)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, `request body must be {"username": "…", "password": "…"}`)
		return
	}
	user, pass, code := strings.TrimSpace(req.Username), req.Password, strings.TrimSpace(req.Code)
	req.Password = "" // out of the request struct as soon as it is read
	if user == "" || pass == "" || len(user) > 255 || len(pass) > 1024 || len(code) > 32 || strings.ContainsAny(user+code, "\x00\r\n") {
		respondError(w, http.StatusBadRequest, "enter a username and a password")
		return
	}

	ip, ukey := clientIP(r, m.TrustForwardedFor), tokenKey(strings.ToLower(user))
	if ok, wait := m.limiter.allowed(ip, ukey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		respondError(w, http.StatusTooManyRequests, "too many failed sign-ins — try again in a little while")
		return
	}

	tokens, err := oc.passwordGrant(r.Context(), user, pass, code)
	pass = "" // #nosec G101 -- clearing a local; nothing here is a hard-coded credential
	switch {
	case err == nil:
	case errors.Is(err, errWrongCredentials):
		m.limiter.fail(ip, ukey)
		m.auditDirect("oidc-password-login-failed", user, ip, "wrong username or password")
		respondError(w, http.StatusUnauthorized, "wrong username or password")
		return
	case errors.Is(err, errNeedsProviderPage):
		// the password was right, so this is not a failure to count
		m.auditDirect("oidc-password-login-refused", user, ip, "the account has to finish setting up on the provider's page")
		respondErrorCode(w, http.StatusConflict, "needs_provider_page", "your account has to finish setting up (a temporary password to replace, or a step the identity provider asks for) — sign in once on the identity provider's own page")
		return
	case errors.Is(err, errDirectNotAvailable):
		m.auditDirect("oidc-password-login-refused", user, ip, "password sign-in is not enabled for the client")
		respondErrorCode(w, http.StatusServiceUnavailable, "direct_not_enabled", "signing in with a password isn't enabled on the identity provider for Vinaire — use the identity provider's own page, or ask the administrator to set Vinaire up again with the form turned on")
		return
	default:
		respondError(w, http.StatusBadGateway, "the identity provider couldn't be reached")
		return
	}
	m.limiter.success(ip, ukey)

	if m.finishSignIn(w, r, oc, tokens.IDToken, tokens.RefreshToken, "password") {
		target := m.FrontendURL
		if target == "" {
			target = "/"
		}
		respondJSON(w, map[string]string{"redirect": target})
	}
}

// auditDirect records a refused or failed password sign-in. The username is recorded only as a short hash and
// its length — people sometimes type the password into the username field.
func (m *Manager) auditDirect(action, user, ip, reason string) {
	if m.Audit == nil {
		return
	}
	sum := sha256.Sum256([]byte(strings.ToLower(user)))
	m.Audit(action, "oidc", "", map[string]any{"reason": reason, "user": hex.EncodeToString(sum[:4]), "userLength": len(user), "ip": ip})
}

type providerTokens struct {
	IDToken      string
	RefreshToken string
}

// passwordGrant asks the provider's token endpoint for tokens for a username and password. It uses the guarded
// client when there is one. The provider's own error text is not passed on: it is classified.
func (c *oidcClient) passwordGrant(ctx context.Context, username, password, code string) (providerTokens, error) {
	form := url.Values{
		"grant_type":    {"password"},
		"username":      {username},
		"password":      {password},
		"scope":         {strings.Join(c.oauth2Config.Scopes, " ")},
		"client_id":     {c.oauth2Config.ClientID},
		"client_secret": {c.oauth2Config.ClientSecret},
	}
	if code != "" {
		form.Set("totp", code)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.oauth2Config.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return providerTokens{}, errProviderDown
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	hc := c.hc
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return providerTokens{}, errProviderDown
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return classifyPasswordResponse(resp.StatusCode, body)
}

// classifyPasswordResponse turns the token endpoint's answer into tokens or one of the fixed errors. It never
// returns the provider's text.
func classifyPasswordResponse(status int, body []byte) (providerTokens, error) {
	var out struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &out)
	if status == http.StatusOK && out.IDToken != "" {
		return providerTokens{IDToken: out.IDToken, RefreshToken: out.RefreshToken}, nil
	}
	desc := strings.ToLower(out.Description)
	switch {
	case out.Error == "unauthorized_client" || strings.Contains(desc, "not allowed for direct access"):
		return providerTokens{}, errDirectNotAvailable
	case strings.Contains(desc, "not fully set up") || strings.Contains(desc, "required action") || strings.Contains(desc, "update password"):
		return providerTokens{}, errNeedsProviderPage
	case out.Error == "invalid_grant" || out.Error == "invalid_request" || status == http.StatusUnauthorized || status == http.StatusBadRequest:
		// wrong password, an unknown or disabled user, a missing one-time code: one answer for all of them
		return providerTokens{}, errWrongCredentials
	}
	return providerTokens{}, errProviderDown
}
