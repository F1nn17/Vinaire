package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/f1nn17/vinaire/backend/internal/api"
	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
	"github.com/f1nn17/vinaire/backend/internal/config"
	"github.com/f1nn17/vinaire/backend/internal/stream"
)

// Roadmap Phase 50: the router is walked, route by route, so that a new endpoint cannot
// quietly skip authentication or the write rate limit — it has to be named in the lists
// below, in a diff a reviewer sees — and so there is always an accurate inventory of what
// Vinaire exposes (docs/api-routes.md).

type route struct {
	Method, Path string
	Session      bool // behind the session middleware
	Limited      bool // behind the write rate limiter
}

func testRouter(t *testing.T) (*chi.Mux, *authn.Manager, *api.WriteRateLimiter) {
	t.Helper()
	cfg := config.Config{CORSAllowedOrigins: []string{"http://localhost:3100"}}
	auth, err := authn.NewManager(t.Context(), authn.ManagerConfig{AuthMode: "token", CookieSameSite: "strict"})
	if err != nil {
		t.Fatal(err)
	}
	h := api.NewHandler(clusters.NewRegistry(), auth, api.ObservabilityConfig{})
	limiter := api.NewWriteRateLimiter(1, 1)
	r := newRouter(routerDeps{
		cfg: cfg, auth: auth, h: h,
		streamHandler: unavailableWSHandler, logsHandler: unavailableWSHandler, execHandler: unavailableWSHandler,
		provisionStream: stream.ServeProvision(nil, auth, cfg.CORSAllowedOrigins),
		writeLimiter:    limiter,
	})
	return r, auth, limiter
}

func fnPtr(f any) uintptr { return reflect.ValueOf(f).Pointer() }

func walk(t *testing.T) []route {
	r, auth, limiter := testRouter(t)
	sessionPtr, limitPtr := fnPtr(auth.Middleware), fnPtr(limiter.Middleware)
	var out []route
	err := chi.Walk(r, func(method, path string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		rt := route{Method: method, Path: path}
		for _, m := range mws {
			switch fnPtr(m) {
			case sessionPtr:
				rt.Session = true
			case limitPtr:
				rt.Limited = true
			}
		}
		out = append(out, rt)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	if len(out) < 100 {
		t.Fatalf("only %d routes found — the walk isn't covering the router", len(out))
	}
	return out
}

// The only routes that answer without a session: they ARE the sign-in flow, plus the
// liveness probe. Anything else must sit behind the session middleware.
var publicRoutes = map[string]string{
	"GET /healthz":                    "liveness probe",
	"GET /api/v1/auth/config":         "tells the login screen which methods are open",
	"POST /api/v1/auth/login":         "pasted-token sign-in",
	"POST /api/v1/auth/logout":        "ends the caller's own session",
	"GET /api/v1/auth/oidc/login":     "starts Keycloak sign-in",
	"GET /api/v1/auth/oidc/callback":  "Keycloak sign-in return",
	"POST /api/v1/auth/oidc/password": "Vinaire's own Keycloak sign-in form (an option an administrator switches on)",
}

func TestEveryRouteNeedsASessionExceptTheSignInFlow(t *testing.T) {
	r, _, _ := testRouter(t)
	for _, rt := range walk(t) {
		key := rt.Method + " " + rt.Path
		if _, public := publicRoutes[key]; public {
			continue
		}
		if !rt.Session {
			t.Errorf("%s is not behind the session middleware — add it, or name it in publicRoutes with the reason", key)
			continue
		}
		// and in practice: no cookie, a junk cookie -> 401 before any handler runs
		path := strings.NewReplacer("{", "", "}", "", "*", "x").Replace(rt.Path)
		for _, cookie := range []string{"", "junk-session-id"} {
			req := httptest.NewRequest(rt.Method, path, nil)
			if cookie != "" {
				req.AddCookie(&http.Cookie{Name: "vinaire_session", Value: cookie})
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s with cookie %q: %d, want 401", key, cookie, w.Code)
			}
		}
	}
	// the allow-list itself must not rot
	seen := map[string]bool{}
	for _, rt := range walk(t) {
		seen[rt.Method+" "+rt.Path] = true
	}
	for k := range publicRoutes {
		if !seen[k] {
			t.Errorf("publicRoutes names %s, which no longer exists", k)
		}
	}
}

// State-changing routes are rate limited per identity (Roadmap Phase 17). These are the
// ones that deliberately are not, each with the reason.
var unlimitedWrites = map[string]string{
	"POST /api/v1/auth/login":                         "sign-in has its own throttle (per address and per token)",
	"POST /api/v1/auth/oidc/password":                 "sign-in has its own throttle (per address and per username)",
	"POST /api/v1/auth/logout":                        "ends the caller's own session",
	"POST /api/v1/resources/dry-run":                  "persists nothing; iterating on a manifest must not burn the write budget",
	"POST /api/v1/rbac/check":                         "a read (SubjectAccessReview), POST only for the body",
	"POST /api/v1/clusters/import-kubeconfig/preview": "a read: describes a pasted kubeconfig without keeping it",
	"POST /api/v1/secrets/{namespace}/{name}/reveal":  "audited read of one Secret value, as the caller",
}

func TestEveryStateChangingRouteIsRateLimited(t *testing.T) {
	for _, rt := range walk(t) {
		if rt.Method == http.MethodGet || rt.Method == http.MethodHead || rt.Method == http.MethodOptions {
			continue
		}
		key := rt.Method + " " + rt.Path
		if strings.HasPrefix(rt.Path, "/proxy/") {
			continue // a pass-through to a pod the caller may reach; capped by its own concurrency limit and audited per reach
		}
		if _, ok := unlimitedWrites[key]; ok {
			if rt.Limited {
				t.Errorf("%s is listed as unlimited but is limited — drop it from unlimitedWrites", key)
			}
			continue
		}
		if !rt.Limited {
			t.Errorf("%s changes state but is not behind the write rate limiter — move it into the limited group, or name it in unlimitedWrites with the reason", key)
		}
	}
}

// docs/api-routes.md is generated from the router; the test fails when it is out of
// date (regenerate with UPDATE_ROUTES=1 go test -run TestRouteInventory).
func TestRouteInventoryIsUpToDate(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Vinaire — HTTP routes\n\n")
	b.WriteString("Generated from the router by `go test -run TestRouteInventory` (`UPDATE_ROUTES=1` rewrites it) — do not edit by hand.\n")
	b.WriteString("A route that is not listed here does not exist; a route that changes state and is not rate limited has to be\n")
	b.WriteString("named, with the reason, in `backend/router_test.go`.\n\n")
	b.WriteString("| Method | Path | Sign-in needed | Write rate limit |\n|---|---|---|---|\n")
	yes := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}
	for _, rt := range walk(t) {
		limited := "—"
		if rt.Method != http.MethodGet && rt.Method != http.MethodHead && rt.Method != http.MethodOptions {
			limited = yes(rt.Limited)
		}
		b.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n", rt.Method, rt.Path, yes(rt.Session), limited))
	}
	want := b.String()
	const file = "../docs/api-routes.md"
	if os.Getenv("UPDATE_ROUTES") == "1" {
		if err := os.WriteFile(file, []byte(want), 0o644); err != nil { // #nosec G306 -- a public document
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v — run UPDATE_ROUTES=1 go test -run TestRouteInventory", err)
	}
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Error("docs/api-routes.md is out of date — run UPDATE_ROUTES=1 go test -run TestRouteInventory and commit the result")
	}
}
