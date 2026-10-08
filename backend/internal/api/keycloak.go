package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/keycloak"
	"github.com/shiraku/vinaire/backend/internal/netguard"
)

// Sign-in through Keycloak in one click (Roadmap Phase 47). A wizard on the
// Settings page takes a Keycloak administrator's credentials once, creates a realm,
// a confidential client for Vinaire and (optionally) a first user, and switches
// OIDC sign-in on — at run time, without a restart, with pasting a token kept as
// the way back in unless the administrator turns it off.
//
// What this changes is Vinaire's *own* configuration, so every handler asks the
// cluster (as the caller) for a right on the virtual resource auth.vinaire.io
// (requireVinaireRight). The administrator's password is used for one token request
// and never stored; the client secret that comes back is sealed in the same store as
// the cluster registry, which refuses to take it unless it is encrypted.

const authDocName = "auth-oidc"

// storedOIDC is the sealed document.
type storedOIDC struct {
	KeycloakURL  string    `json:"keycloakUrl"`
	Realm        string    `json:"realm"`
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"clientId"`
	ClientSecret string    `json:"clientSecret"`
	RedirectURL  string    `json:"redirectUrl"`
	AllowToken   bool      `json:"allowToken"`
	SetUpBy      string    `json:"setUpBy"`
	SetUpAt      time.Time `json:"setUpAt"`
}

// keycloakState is what the Keycloak endpoints share on the Handler.
type keycloakState struct {
	mu sync.Mutex
	// pending: a stored configuration that couldn't be switched on yet (Keycloak was
	// down at start); retried in the background.
	pendingErr string
}

func (h *Handler) authDocsAvailable(w http.ResponseWriter) bool {
	switch {
	case h.Auth.Mode == authn.ModeDisabled:
		respondError(w, http.StatusConflict, "sign-in is turned off for this deployment (AUTH_MODE=disabled), so there is nothing to put Keycloak in front of")
	case h.Auth.Mode == authn.ModeOIDC:
		respondError(w, http.StatusConflict, "this deployment already signs in through OIDC, configured by its operator (AUTH_MODE=oidc)")
	case h.AuthDocs == nil:
		respondError(w, http.StatusServiceUnavailable, "there is nowhere to keep the encrypted settings — run Vinaire in a cluster or set VINAIRE_DATA_DIR")
	default:
		return true
	}
	return false
}

func (h *Handler) oidcHTTPClient() *http.Client {
	return netguard.Client(netguard.Options{AllowLoopback: h.OIDCAllowInsecure, Timeout: 30 * time.Second})
}

func (h *Handler) loadStoredOIDC(ctx context.Context) (*storedOIDC, error) {
	if h.AuthDocs == nil {
		return nil, nil
	}
	raw, err := h.AuthDocs.LoadDoc(ctx, authDocName)
	if err != nil || raw == nil {
		return nil, err
	}
	var s storedOIDC
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.Issuer == "" { // removed (the document holds null)
		return nil, nil
	}
	return &s, nil
}

// RestoreOIDC switches a saved Keycloak sign-in back on after a restart. If
// Keycloak can't be reached yet it keeps trying in the background (a cluster that
// is starting up usually has Vinaire before Keycloak), while signing in with a
// token keeps working in the meantime.
func (h *Handler) RestoreOIDC(ctx context.Context) {
	if h.Auth.Mode != authn.ModeToken {
		return
	}
	s, err := h.loadStoredOIDC(ctx)
	if err != nil {
		slog.Warn("keycloak sign-in: couldn't read the saved settings", "error", err)
		return
	}
	if s == nil {
		return
	}
	try := func() error {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return h.Auth.ConfigureOIDC(cctx, h.oidcHTTPClient(), s.Issuer, s.ClientID, s.ClientSecret, s.RedirectURL, s.AllowToken)
	}
	if err := try(); err == nil {
		slog.Info("keycloak sign-in restored", "issuer", s.Issuer)
		return
	} else {
		h.keycloak.mu.Lock()
		h.keycloak.pendingErr = err.Error()
		h.keycloak.mu.Unlock()
		slog.Warn("keycloak sign-in is saved but Keycloak didn't answer — retrying; signing in with a token still works", "issuer", s.Issuer, "error", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			if err := try(); err != nil {
				h.keycloak.mu.Lock()
				h.keycloak.pendingErr = err.Error()
				h.keycloak.mu.Unlock()
				continue
			}
			h.keycloak.mu.Lock()
			h.keycloak.pendingErr = ""
			h.keycloak.mu.Unlock()
			slog.Info("keycloak sign-in restored", "issuer", s.Issuer)
			return
		}
	}()
}

// KeycloakStatus is GET /api/v1/auth/keycloak.
type KeycloakStatus struct {
	Available bool `json:"available"`
	// Reason: why it isn't available.
	Reason string `json:"reason,omitempty"`
	// Active: sign-in through Keycloak is on right now.
	Active      bool   `json:"active"`
	Configured  bool   `json:"configured"` // saved settings exist
	Issuer      string `json:"issuer,omitempty"`
	Realm       string `json:"realm,omitempty"`
	ClientID    string `json:"clientId,omitempty"`
	RedirectURL string `json:"redirectUrl,omitempty"`
	TokenLogin  bool   `json:"tokenLogin"`
	SetUpBy     string `json:"setUpBy,omitempty"`
	PendingErr  string `json:"pendingError,omitempty"`
	// OperatorManaged: AUTH_MODE=oidc, set by the deployment, not by this wizard.
	OperatorManaged bool `json:"operatorManaged,omitempty"`
}

// KeycloakStatus handles GET /api/v1/auth/keycloak.
func (h *Handler) KeycloakStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "auth", "get") {
		return
	}
	st := KeycloakStatus{Available: true}
	oidcOn, tokenOn := h.Auth.SignInMethods()
	st.Active, st.TokenLogin = oidcOn, tokenOn
	switch {
	case h.Auth.Mode == authn.ModeDisabled:
		st.Available, st.Reason = false, "sign-in is turned off for this deployment (AUTH_MODE=disabled)"
	case h.Auth.Mode == authn.ModeOIDC:
		st.Available, st.OperatorManaged = false, true
		st.Reason = "this deployment already signs in through OIDC, configured by its operator (AUTH_MODE=oidc)"
	case h.AuthDocs == nil:
		st.Available, st.Reason = false, "there is nowhere to keep the encrypted settings — run Vinaire in a cluster or set VINAIRE_DATA_DIR"
	}
	if s, err := h.loadStoredOIDC(r.Context()); err == nil && s != nil {
		st.Configured, st.Issuer, st.Realm, st.ClientID, st.RedirectURL, st.SetUpBy =
			true, s.Issuer, s.Realm, s.ClientID, s.RedirectURL, s.SetUpBy
	}
	h.keycloak.mu.Lock()
	st.PendingErr = h.keycloak.pendingErr
	h.keycloak.mu.Unlock()
	respondJSON(w, st)
}

type keycloakSetupBody struct {
	KeycloakURL   string `json:"keycloakUrl"`
	AdminRealm    string `json:"adminRealm"`
	AdminUser     string `json:"adminUser"`
	AdminPassword string `json:"adminPassword"`
	Realm         string `json:"realm"`
	ClientID      string `json:"clientId"`
	// the first user — optional
	User               string `json:"user"`
	UserEmail          string `json:"userEmail"`
	UserPassword       string `json:"userPassword"`
	UserChangePassword bool   `json:"userChangePassword"`
	// AllowTokenLogin keeps pasting a token as a way in next to Keycloak.
	AllowTokenLogin bool `json:"allowTokenLogin"`
}

// KeycloakSetupResult is the answer to a setup.
type KeycloakSetupResult struct {
	Steps       []keycloak.Step `json:"steps"`
	Issuer      string          `json:"issuer"`
	RedirectURL string          `json:"redirectUrl"`
	// APIServerFlags: what the cluster's API server needs so it accepts the ID tokens
	// Keycloak issues — a cluster setting Vinaire can't change.
	APIServerFlags []string `json:"apiServerFlags"`
}

// publicOrigin is the scheme://host the browser reached Vinaire on — what Keycloak
// must send the browser back to. The browser's own Origin header is the best
// witness (the origin guard has already checked it); else the request's host.
func publicOrigin(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		if u, err := url.Parse(o); err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") {
			return u.Scheme + "://" + u.Host
		}
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// KeycloakSetup handles POST /api/v1/auth/keycloak/setup.
func (h *Handler) KeycloakSetup(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "auth", "create") {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !h.authDocsAvailable(w) || !h.requireSecureTransport(w, r) {
		return
	}
	if !h.AuthDocs.Encrypted() {
		respondError(w, http.StatusConflict, "the settings store isn't encrypted, and this would put a client secret in it — set CLUSTER_STORE_KEY (or give Vinaire access to its key Secret) first")
		return
	}
	var b keycloakSetupBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if b.AdminRealm == "" {
		b.AdminRealm = "master"
	}
	if b.Realm == "" {
		b.Realm = "vinaire"
	}
	if b.ClientID == "" {
		b.ClientID = "vinaire"
	}
	origin := publicOrigin(r)
	redirect := origin + "/api/v1/auth/oidc/callback"
	p := keycloak.Params{
		BaseURL: b.KeycloakURL, AdminRealm: b.AdminRealm, AdminUser: b.AdminUser, AdminPass: keycloak.NewSecret(b.AdminPassword),
		Realm: b.Realm, ClientID: b.ClientID, RedirectURL: redirect, Origin: origin,
		User: b.User, UserEmail: b.UserEmail, UserPass: keycloak.NewSecret(b.UserPassword), UserMustChangePassword: b.UserChangePassword,
	}
	b.AdminPassword, b.UserPassword = "", "" // out of the plain struct as soon as they're wrapped
	if err := p.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A Keycloak URL comes from the request, so it is held to the same rules as any
	// other server-side fetch: https, no loopback/link-local — unless the operator
	// opted into local development.
	if u, _ := url.Parse(p.BaseURL); u != nil && u.Scheme != "https" && !h.OIDCAllowInsecure {
		respondError(w, http.StatusBadRequest, "the Keycloak URL must be https (OIDC_ALLOW_INSECURE=true allows http on loopback for local development)")
		return
	}

	hc := h.oidcHTTPClient()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	res, err := keycloak.Setup(ctx, hc, p)
	if err != nil {
		h.auditLog(r, "keycloak-setup-failed", "", p.Realm, "keycloak", hostOf(p.BaseURL), "error", truncate(err.Error(), 300))
		switch {
		case errors.Is(err, keycloak.ErrInvalid):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, keycloak.ErrAuth):
			respondError(w, http.StatusUnauthorized, err.Error())
		default:
			respondError(w, http.StatusBadGateway, err.Error())
		}
		return
	}

	// switch it on — discovery proves the realm and the secret work before anything is saved
	if err := h.Auth.ConfigureOIDC(ctx, hc, res.IssuerURL, res.ClientID, res.ClientSecret.Reveal(), redirect, b.AllowTokenLogin); err != nil {
		h.auditLog(r, "keycloak-setup-failed", "", p.Realm, "keycloak", hostOf(p.BaseURL), "error", "keycloak answered but Vinaire can't use it: "+truncate(err.Error(), 250))
		respondError(w, http.StatusBadGateway, "Keycloak was set up, but Vinaire couldn't start using it: "+err.Error()+" — the URL Vinaire reaches Keycloak on must be the one the realm's issuer reports")
		return
	}
	doc, _ := json.Marshal(storedOIDC{ // #nosec G117 -- sealed by the store, see above
		KeycloakURL: strings.TrimRight(p.BaseURL, "/"), Realm: p.Realm, Issuer: res.IssuerURL, ClientID: res.ClientID,
		ClientSecret: res.ClientSecret.Reveal(), RedirectURL: redirect, AllowToken: b.AllowTokenLogin,
		SetUpBy: callerName(r), SetUpAt: time.Now().UTC(),
	})
	if err := h.AuthDocs.SaveDoc(r.Context(), authDocName, doc); err != nil {
		h.Auth.DisableOIDC() // never leave a setting on that wouldn't survive a restart
		respondError(w, http.StatusInternalServerError, "couldn't save the settings: "+err.Error())
		return
	}
	h.keycloak.mu.Lock()
	h.keycloak.pendingErr = ""
	h.keycloak.mu.Unlock()
	h.auditLog(r, "keycloak-setup", "", p.Realm, "keycloak", hostOf(p.BaseURL), "client", res.ClientID,
		"firstUser", p.User != "", "allowTokenLogin", b.AllowTokenLogin, "steps", stepSummary(res.Steps))
	respondJSON(w, KeycloakSetupResult{
		Steps: res.Steps, Issuer: res.IssuerURL, RedirectURL: redirect,
		APIServerFlags: []string{
			"--oidc-issuer-url=" + res.IssuerURL,
			"--oidc-client-id=" + res.ClientID,
			"--oidc-username-claim=preferred_username",
			"--oidc-username-prefix=oidc:",
		},
	})
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func stepSummary(steps []keycloak.Step) string {
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, s.What+": "+s.Status)
	}
	return strings.Join(parts, "; ")
}

// KeycloakTokenLogin handles PUT /api/v1/auth/keycloak/token-login {"allow": bool}.
func (h *Handler) KeycloakTokenLogin(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "auth", "update") {
		return
	}
	if !h.authDocsAvailable(w) {
		return
	}
	var b struct {
		Allow bool `json:"allow"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&b); err != nil {
		respondError(w, http.StatusBadRequest, `request body must be {"allow": true|false}`)
		return
	}
	s, err := h.loadStoredOIDC(r.Context())
	if err != nil || s == nil {
		respondError(w, http.StatusConflict, "Keycloak sign-in isn't set up")
		return
	}
	if err := h.Auth.SetTokenLogin(b.Allow); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	s.AllowToken = b.Allow
	doc, _ := json.Marshal(s) // #nosec G117 -- the document is handed to the sealed store (it refuses an unencrypted one); it never reaches a response
	if err := h.AuthDocs.SaveDoc(r.Context(), authDocName, doc); err != nil {
		_ = h.Auth.SetTokenLogin(!b.Allow)
		respondError(w, http.StatusInternalServerError, "couldn't save the setting: "+err.Error())
		return
	}
	h.auditLog(r, "keycloak-token-login", "", s.Realm, "allow", b.Allow)
	respondJSON(w, map[string]bool{"tokenLogin": b.Allow})
}

// KeycloakDisable handles DELETE /api/v1/auth/keycloak. It only forgets Vinaire's
// side (the saved client and secret); Keycloak itself is not touched. Signing in
// with a token is open again.
func (h *Handler) KeycloakDisable(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "auth", "delete") {
		return
	}
	if !h.authDocsAvailable(w) {
		return
	}
	s, _ := h.loadStoredOIDC(r.Context())
	if err := h.AuthDocs.SaveDoc(r.Context(), authDocName, []byte("null")); err != nil {
		respondError(w, http.StatusInternalServerError, "couldn't save the setting: "+err.Error())
		return
	}
	h.Auth.DisableOIDC()
	realm := ""
	if s != nil {
		realm = s.Realm
	}
	h.keycloak.mu.Lock()
	h.keycloak.pendingErr = ""
	h.keycloak.mu.Unlock()
	h.auditLog(r, "keycloak-disable", "", realm)
	respondJSON(w, map[string]string{"status": "ok"})
}
