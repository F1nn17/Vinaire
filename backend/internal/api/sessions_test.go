package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/f1nn17/vinaire/backend/internal/authn"
)

func (hs *harness) revoke(token, ref string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("DELETE", "/x", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ref", ref)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	w := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(hs.h.RevokeSession)).ServeHTTP(w, r)
	return w
}

func TestSessionsListAndRevoke(t *testing.T) {
	hs := newHarness(t, "token")
	hs.api.grant("admin", "vinaire.io/auth/get", "vinaire.io/auth/delete")
	hs.api.grant("viewer", "/pods/list")

	if w := hs.get(hs.h.Sessions, "/x", "viewer"); w.Code != http.StatusForbidden {
		t.Errorf("an ordinary user: %d", w.Code)
	}
	w := hs.get(hs.h.Sessions, "/x", "admin")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"current":true`) || !strings.Contains(w.Body.String(), authn.Ref("sess-viewer")) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	// neither the cookie value nor the token is in the list
	for _, leak := range []string{"sess-admin", "sess-viewer", `"admin"`, `"viewer"`} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("the list carries %s", leak)
		}
	}
	if w := hs.revoke("viewer", authn.Ref("sess-viewer")); w.Code != http.StatusForbidden {
		t.Errorf("an ordinary user revoking: %d", w.Code)
	}
	if w := hs.revoke("admin", "nonexistent-ref"); w.Code != http.StatusNotFound {
		t.Errorf("unknown ref: %d", w.Code)
	}
	if w := hs.revoke("admin", authn.Ref("sess-viewer")); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if w := hs.get(hs.h.MetricsStatus, "/x", "viewer"); w.Code != http.StatusUnauthorized {
		t.Errorf("a revoked session still works: %d", w.Code)
	}
}
