package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func (hs *harness) deleteSecret(token, namespace, name, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("DELETE", "/x", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("name", name)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	w := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(hs.h.DeleteSecret)).ServeHTTP(w, r)
	return w
}

func secretObj(ns, name, typ string) map[string]any {
	return map[string]any{"kind": "Secret", "apiVersion": "v1", "type": typ,
		"metadata": map[string]any{"name": name, "namespace": ns}}
}

// Owner's decision of 2026-10-09: a Secret an uninstalled chart left behind can be deleted — with
// the name typed, never in a system namespace, never a Helm release record.
func TestDeleteSecretGuardRails(t *testing.T) {
	hs := newHarness(t, "token")
	hs.api.canned = map[string]any{
		"/api/v1/namespaces/app/secrets/db":      secretObj("app", "db", "Opaque"),
		"/api/v1/namespaces/app/secrets/sh.helm": secretObj("app", "sh.helm", "helm.sh/release.v1"),
	}
	deleted := func() int { hs.api.mu.Lock(); defer hs.api.mu.Unlock(); return len(hs.api.deleted) }

	for name, c := range map[string]struct {
		ns, secret, body string
		want             int
	}{
		"no confirmation":    {"app", "db", `{}`, 400},
		"wrong confirmation": {"app", "db", `{"confirmName":"nope"}`, 400},
		"a system namespace": {"kube-system", "db", `{"confirmName":"db"}`, 400},
		"a malformed name":   {"app", "Bad_Name", `{"confirmName":"Bad_Name"}`, 400},
		"a helm release":     {"app", "sh.helm", `{"confirmName":"sh.helm"}`, 409},
	} {
		if w := hs.deleteSecret("viewer", c.ns, c.secret, c.body); w.Code != c.want {
			t.Errorf("%s: %d %s, want %d", name, w.Code, w.Body, c.want)
		}
		if deleted() != 0 {
			t.Fatalf("%s: something was deleted: %v", name, hs.api.deleted)
		}
	}
	if w := hs.deleteSecret("viewer", "app", "db", `{"confirmName":"db"}`); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if deleted() != 1 || hs.api.deleted[0] != "/api/v1/namespaces/app/secrets/db" {
		t.Errorf("deleted = %v", hs.api.deleted)
	}
	for _, auth := range hs.api.seen {
		if strings.Contains(auth, "backend-token") {
			t.Fatal("a call used Vinaire's own identity instead of the caller's")
		}
	}
}
