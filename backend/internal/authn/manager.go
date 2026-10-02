package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidToken is what a TokenValidator returns for a token the cluster
// rejects, as opposed to a cluster that couldn't be asked.
var ErrInvalidToken = errors.New("the cluster doesn't accept this token")

// TokenValidator asks the cluster whether a token is accepted. A token it
// refuses never gets a session, so the session store can't be filled by someone
// with no credentials, and a typo shows up at login instead of as 401s later.
type TokenValidator func(ctx context.Context, token string) error

// Mode selects which AuthProvider implementation is active, via the
// AUTH_MODE env var (see internal/config).
type Mode string

const (
	// ModeToken (the default): log in by pasting a Kubernetes
	// ServiceAccount token directly.
	ModeToken Mode = "token"
	// ModeOIDC: log in via Keycloak (or any OIDC provider) once one is
	// deployed in the cluster — see oidc.go.
	ModeOIDC Mode = "oidc"
	// ModeDisabled: no login at all, for local k3d development. Every
	// request uses the backend's own kubeconfig-derived credentials,
	// exactly like every phase before this one.
	ModeDisabled Mode = "disabled"
)

// sessionCookie is the browser's only artifact of being logged in — an
// opaque session ID, never the real K8s bearer token it stands in for.
const sessionCookie = "vinaire_session"

// defaultTokenSessionTTL is used when a pasted token isn't a JWT with a
// readable "exp" claim (e.g. an opaque/legacy K8s token) — falls back to
// a conservative session lifetime rather than refusing the login outright.
const defaultTokenSessionTTL = time.Hour

// Manager is the single AuthProvider entry point: it owns the session
// store, knows which Mode is active, and provides both the HTTP handlers
// for /api/v1/auth/* and the Middleware every other protected route goes
// through.
type Manager struct {
	Mode           Mode
	Sessions       Store
	CookieSecure   bool
	CookieSameSite http.SameSite
	FrontendURL    string

	// ValidateToken, when set, is asked about every pasted token before a session
	// is created (token mode).
	ValidateToken TokenValidator
	// MaxSessionTTL caps a session's life however long the token it holds lives
	// (a ServiceAccount token can be valid for a year).
	MaxSessionTTL time.Duration
	// TrustForwardedFor makes the login throttle count X-Forwarded-For's first
	// address instead of the connection's peer — only for a deployment where a
	// trusted proxy sets it.
	TrustForwardedFor bool

	limiter *loginLimiter
	oidc    *oidcClient // nil unless Mode == ModeOIDC
}

// defaultMaxSessionTTL is how long a login lasts at most unless configured.
const defaultMaxSessionTTL = 12 * time.Hour

func (m *Manager) capExpiry(exp time.Time) time.Time {
	max := m.MaxSessionTTL
	if max <= 0 {
		max = defaultMaxSessionTTL
	}
	if limit := time.Now().Add(max); exp.After(limit) {
		return limit
	}
	return exp
}

// ManagerConfig is the subset of internal/config.Config NewManager needs —
// kept as its own small struct so this package doesn't import
// internal/config (avoiding a needless dependency edge).
type ManagerConfig struct {
	AuthMode         string
	CookieSecure     bool
	CookieSameSite   string // "none", "lax", or "strict" — see internal/config.Config
	FrontendURL      string
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	// SessionMaxTTL and TrustForwardedFor: see Manager.
	SessionMaxTTL     time.Duration
	TrustForwardedFor bool
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "strict":
		return http.SameSiteStrictMode
	case "lax":
		return http.SameSiteLaxMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteStrictMode // the safe reading of anything unrecognised
	}
}

// NewManager builds the Manager for cfg.AuthMode. When AuthMode is "oidc",
// it discovers the OIDC provider immediately and returns an error if that
// fails — main.go treats this as fatal, per this phase's own risk note: a
// misconfigured/unreachable issuer must fail loudly at boot, not silently
// leave login broken.
func NewManager(ctx context.Context, cfg ManagerConfig) (*Manager, error) {
	m := &Manager{
		Mode:           Mode(cfg.AuthMode),
		Sessions:       NewMemoryStore(),
		CookieSecure:   cfg.CookieSecure,
		CookieSameSite: parseSameSite(cfg.CookieSameSite),
		FrontendURL:    cfg.FrontendURL,

		MaxSessionTTL:     cfg.SessionMaxTTL,
		TrustForwardedFor: cfg.TrustForwardedFor,
		limiter:           newLoginLimiter(),
	}
	if ms, ok := m.Sessions.(*MemoryStore); ok {
		ms.StartJanitor(ctx)
	}

	switch m.Mode {
	case ModeToken, ModeDisabled:
		// Nothing further to set up.
	case ModeOIDC:
		client, err := NewOIDCClient(ctx, cfg.OIDCIssuerURL, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCRedirectURL)
		if err != nil {
			return nil, err
		}
		m.oidc = client
	default:
		return nil, fmt.Errorf("unknown AUTH_MODE %q (want %q, %q, or %q)", cfg.AuthMode, ModeToken, ModeOIDC, ModeDisabled)
	}

	return m, nil
}

// disabledSession is the fixed pseudo-identity used in ModeDisabled — no
// per-user K8s token exists in this mode, so
// internal/clusters.Entry.ClientsetForToken is never called; handlers use
// the entry's own backend-credentialed Clientset instead (see
// internal/api's resolveCluster).
var disabledSession = &Session{Username: "local"}

// Authenticate resolves r's session (or, in ModeDisabled, the fixed
// pseudo-session) from its session cookie. Shared by Middleware (for plain
// REST handlers) and internal/stream's WebSocket handlers, which perform
// their own upgrade and so can't just wrap a plain http.Handler.
func (m *Manager) Authenticate(r *http.Request) (*Session, bool) {
	if m.Mode == ModeDisabled {
		return disabledSession, true
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, false
	}
	return m.Sessions.Get(cookie.Value)
}

// Middleware resolves the caller's session via Authenticate and stores it
// in the request context — see FromContext. Requests without a valid
// session get 401 and never reach the wrapped handler.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := m.Authenticate(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), sess)))
	})
}

// HandleConfig handles GET /api/v1/auth/config — tells the frontend which
// login screen to show.
func (m *Manager) HandleConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, map[string]string{"mode": string(m.Mode)})
}

type tokenLoginRequest struct {
	Token string `json:"token"`
}

// HandleTokenLogin handles POST /api/v1/auth/login (ModeToken only): takes
// a pasted Kubernetes ServiceAccount token, creates a session for it, and
// sets the session cookie. The token itself is never echoed back or
// logged — see AGENTS.md's core rule on treating it like a password.
func (m *Manager) HandleTokenLogin(w http.ResponseWriter, r *http.Request) {
	if m.Mode != ModeToken {
		respondError(w, http.StatusBadRequest, "token login is not enabled (AUTH_MODE="+string(m.Mode)+")")
		return
	}

	var req tokenLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		respondError(w, http.StatusBadRequest, `request body must be {"token": "<service account token>"}`)
		return
	}

	ip, tkey := clientIP(r, m.TrustForwardedFor), tokenKey(req.Token)
	if ok, wait := m.limiter.allowed(ip, tkey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		respondError(w, http.StatusTooManyRequests, "too many failed logins — try again in a little while")
		return
	}
	if m.ValidateToken != nil {
		vctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		err := m.ValidateToken(vctx, req.Token)
		cancel()
		switch {
		case errors.Is(err, ErrInvalidToken):
			m.limiter.fail(ip, tkey)
			respondError(w, http.StatusUnauthorized, "the cluster doesn't accept this token")
			return
		case err != nil:
			// the cluster couldn't be asked: not the user's mistake, so not counted
			respondError(w, http.StatusServiceUnavailable, "couldn't reach the cluster to check this token")
			return
		}
	}
	m.limiter.success(ip, tkey)

	expiry, err := tokenExpiry(req.Token)
	if err != nil {
		expiry = time.Now().Add(defaultTokenSessionTTL)
	}
	expiry = m.capExpiry(expiry)

	// a new login never inherits an existing session
	if old, err := r.Cookie(sessionCookie); err == nil {
		m.Sessions.Delete(old.Value)
	}
	sess := &Session{ID: NewSessionID(), K8sToken: req.Token, ExpiresAt: expiry}
	if err := m.Sessions.Create(sess); err != nil {
		respondError(w, http.StatusServiceUnavailable, "the server can't take more logins right now")
		return
	}
	m.setSessionCookie(w, sess.ID, expiry)
	respondJSON(w, map[string]string{"status": "ok"})
}

// HandleLogout handles POST /api/v1/auth/logout for every mode except
// ModeDisabled (which has no session to log out of).
func (m *Manager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		m.Sessions.Delete(cookie.Value)
	}
	m.clearSessionCookie(w)
	respondJSON(w, map[string]string{"status": "ok"})
}

func (m *Manager) setSessionCookie(w http.ResponseWriter, sessionID string, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- HttpOnly always; Secure and SameSite come from configuration (defaults: Secure, Strict)
		Name:     sessionCookie,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.CookieSecure,
		SameSite: m.CookieSameSite,
		Expires:  expiry,
	})
}

func (m *Manager) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- clearing the session cookie, same attributes as when it was set
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.CookieSecure,
		SameSite: m.CookieSameSite,
	})
}

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
