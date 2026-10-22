package authn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// oidcPendingCookie carries the PKCE verifier + state between
// HandleOIDCLogin (which redirects the browser to Keycloak) and
// HandleOIDCCallback (which Keycloak redirects back to) — short-lived
// (5 minutes) and SameSite=Lax, not Strict: it must survive the top-level
// cross-site redirect landing back from Keycloak, which a Strict cookie
// would not be sent on.
const oidcPendingCookie = "vinaire_oidc_pending"

type oidcClient struct {
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	// hc, when set, carries every request to the provider (discovery, token
	// exchange, keys): the guarded client of a provider named at run time.
	hc *http.Client
}

// ctx returns ctx carrying this client's HTTP client, if it has one.
func (c *oidcClient) ctx(ctx context.Context) context.Context {
	if c.hc == nil {
		return ctx
	}
	return oidc.ClientContext(ctx, c.hc)
}

// NewOIDCClient discovers the OIDC provider and builds the Authorization
// Code + PKCE client for it. Called once at startup when AUTH_MODE=oidc;
// callers should treat a non-nil error as fatal (see main.go and this
// phase's risk note: a misconfigured/unreachable issuer must fail loudly
// at boot, not silently break login later).
func NewOIDCClient(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string) (*oidcClient, error) {
	return newOIDCClient(ctx, nil, issuerURL, clientID, clientSecret, redirectURL)
}

func newOIDCClient(ctx context.Context, hc *http.Client, issuerURL, clientID, clientSecret, redirectURL string) (*oidcClient, error) {
	if hc != nil {
		ctx = oidc.ClientContext(ctx, hc)
	}
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider %s: %w", issuerURL, err)
	}
	return &oidcClient{
		hc:       hc,
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth2Config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}, nil
}

type oidcPending struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
}

// HandleOIDCLogin handles GET /api/v1/auth/oidc/login (ModeOIDC only):
// redirects the browser to Keycloak's real authorization endpoint. This
// whole exchange runs entirely in this Go process — no Node/Next.js
// server-side OIDC calls at all, unlike a next-auth-style setup, so there
// is no equivalent of the Node-specific local networking issue that a
// frontend-side OIDC client would be exposed to on this dev machine.
func (m *Manager) HandleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	oc := m.getOIDC()
	if m.Mode == ModeDisabled || oc == nil {
		respondError(w, http.StatusBadRequest, "oidc login is not enabled")
		return
	}

	state := uuid.NewString()
	verifier := oauth2.GenerateVerifier()

	pending, err := json.Marshal(oidcPending{State: state, Verifier: verifier})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to prepare login")
		return
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- HttpOnly is set; Secure is configurable (default true) and SameSite is Lax on purpose for the OIDC redirect
		Name:     oidcPendingCookie,
		Value:    base64.RawURLEncoding.EncodeToString(pending),
		Path:     "/",
		HttpOnly: true,
		Secure:   m.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})

	authURL := oc.oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleOIDCCallback handles GET /api/v1/auth/oidc/callback — Keycloak
// redirects the browser here with `code`/`state` after a successful
// login. Exchanges the code for tokens, verifies the ID token, and starts
// a session exactly like token mode's, just sourced from Keycloak instead
// of a pasted token. By default the K8s bearer for this session is the ID
// token itself; the target cluster's API server then needs its own
// --oidc-issuer-url/--oidc-client-id configured (and a ClusterRoleBinding
// for the resulting user/group) for RBAC to actually recognize it — and
// Kubernetes accepts only an https issuer. With an IdentityBroker (broker.go,
// Roadmap Phase 80) the bearer is a ServiceAccount token of the person's own
// instead, which needs nothing from the API server. Either way Vinaire itself
// never evaluates permissions; the K8s API does, on every call, same as token mode.
func (m *Manager) HandleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	oc := m.getOIDC()
	if m.Mode == ModeDisabled || oc == nil {
		respondError(w, http.StatusBadRequest, "oidc login is not enabled")
		return
	}

	pendingCookie, err := r.Cookie(oidcPendingCookie)
	if err != nil {
		respondError(w, http.StatusBadRequest, "login session expired or missing, try signing in again")
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(pendingCookie.Value)
	var pending oidcPending
	if err != nil || json.Unmarshal(raw, &pending) != nil {
		respondError(w, http.StatusBadRequest, "invalid login state, try signing in again")
		return
	}
	clearOIDCPendingCookie(w, m.CookieSecure)

	query := r.URL.Query()
	if query.Get("error") != "" {
		respondError(w, http.StatusBadRequest, "oidc provider returned an error: "+query.Get("error"))
		return
	}
	if query.Get("state") != pending.State {
		respondError(w, http.StatusBadRequest, "state mismatch, try signing in again")
		return
	}

	token, err := oc.oauth2Config.Exchange(oc.ctx(r.Context()), query.Get("code"), oauth2.VerifierOption(pending.Verifier))
	if err != nil {
		respondError(w, http.StatusBadGateway, "token exchange failed: "+err.Error())
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		respondError(w, http.StatusBadGateway, "oidc provider did not return an id_token")
		return
	}
	idToken, err := oc.verifier.Verify(oc.ctx(r.Context()), rawIDToken)
	if err != nil {
		respondError(w, http.StatusBadGateway, "id_token verification failed: "+err.Error())
		return
	}
	var claims struct {
		PreferredUsername string `json:"preferred_username"`
	}
	_ = idToken.Claims(&claims)

	// By default the ID token itself is the bearer for the cluster. With a broker (Roadmap Phase 80) Vinaire
	// has verified the sign-in and the cluster is given an ordinary credential of the person's own instead,
	// so the cluster doesn't have to trust the issuer — which Kubernetes only does over https.
	bearer, kubeUser, expiry := rawIDToken, "", m.capExpiry(idToken.Expiry)
	if broker := m.IdentityBroker(); broker != nil {
		bt, err := broker.TokenFor(oc.ctx(r.Context()), Identity{Subject: idToken.Subject, Username: claims.PreferredUsername})
		if err != nil {
			respondError(w, http.StatusBadGateway, "signed in with the identity provider, but Vinaire couldn't get a Kubernetes credential for you: "+err.Error())
			return
		}
		bearer, kubeUser, expiry = bt.Token, bt.KubernetesUser, m.capExpiry(bt.Expiry)
	}
	if old, err := r.Cookie(sessionCookie); err == nil {
		m.Sessions.Delete(old.Value) // a new login never inherits an existing session
	}
	sess := &Session{
		ID:             NewSessionID(),
		K8sToken:       bearer,
		Username:       claims.PreferredUsername,
		KubernetesUser: kubeUser,
		ExpiresAt:      expiry,
	}
	if err := m.Sessions.Create(sess); err != nil {
		respondError(w, http.StatusServiceUnavailable, "the server can't take more logins right now")
		return
	}
	m.setSessionCookie(w, sess.ID, expiry)

	redirectTo := m.FrontendURL
	if redirectTo == "" {
		redirectTo = "/"
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func clearOIDCPendingCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- clearing the OIDC state cookie, same attributes as when it was set
		Name:     oidcPendingCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
