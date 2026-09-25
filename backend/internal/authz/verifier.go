package authz

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

type contextKey int

const claimsKey contextKey = iota

// Verifier validates OIDC bearer tokens issued by the configured provider
// (Keycloak in dev — see /keycloak at the repo root).
type Verifier struct {
	idTokenVerifier *oidc.IDTokenVerifier
}

// NewVerifier fetches the OIDC provider's discovery document and builds a
// token verifier against it. Call once at startup; if the provider is
// unreachable this returns an error and main.go treats it the same way as
// the metrics/Gateway API clients — non-fatal, degrading to "auth
// unavailable" (fail closed: no valid token can ever be produced without
// it, so every request gets 401/403 rather than being let through).
func NewVerifier(ctx context.Context, issuerURL, clientID string) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider %s: %w", issuerURL, err)
	}
	return &Verifier{idTokenVerifier: provider.Verifier(&oidc.Config{ClientID: clientID})}, nil
}

// tokenClaims is the subset of Keycloak's token payload this backend reads.
// `groups` is added by the realm's protocol mapper — see
// keycloak/realm-export.json.
type tokenClaims struct {
	Subject           string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
}

// Middleware verifies the request's bearer token and stores the resulting
// Claims in the request context, rejecting the request with 401 otherwise.
// Mount it on every /api/v1 route except /healthz.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := v.VerifyRequest(r)
		if err != nil {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
	})
}

// VerifyRequest is Middleware's check exposed directly, for the WebSocket
// handlers in internal/stream — they upgrade the connection themselves
// before chi's normal middleware chain would apply to a plain handler.
func (v *Verifier) VerifyRequest(r *http.Request) (Claims, error) {
	raw := bearerToken(r)
	if raw == "" {
		return Claims{}, fmt.Errorf("missing bearer token")
	}
	idToken, err := v.idTokenVerifier.Verify(r.Context(), raw)
	if err != nil {
		return Claims{}, err
	}
	var c tokenClaims
	if err := idToken.Claims(&c); err != nil {
		return Claims{}, fmt.Errorf("decode token claims: %w", err)
	}
	return Claims{Subject: c.Subject, Username: c.PreferredUsername, Groups: c.Groups}, nil
}

// bearerToken reads the token from the Authorization header, falling back
// to an `access_token` query param — browsers can't attach custom headers
// to a WebSocket handshake, so the log/exec/workload-stream endpoints in
// internal/stream rely on the query param instead.
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("access_token")
}

// FromContext retrieves the Claims Middleware stored on this request.
func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}
