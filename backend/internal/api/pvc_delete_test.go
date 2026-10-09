package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func (hs *harness) deletePVC(token, namespace, name, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("DELETE", "/x", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("name", name)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	w := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(hs.h.DeletePVC)).ServeHTTP(w, r)
	return w
}

func pvcObj(ns, name string) map[string]any {
	return map[string]any{"kind": "PersistentVolumeClaim", "apiVersion": "v1",
		"metadata": map[string]any{"name": name, "namespace": ns},
		"spec":     map[string]any{"volumeName": "pv-1", "storageClassName": "local-path"}}
}

func podUsing(ns, name, claim string) map[string]any {
	return map[string]any{"metadata": map[string]any{"name": name, "namespace": ns},
		"spec": map[string]any{"volumes": []any{map[string]any{"name": "d", "persistentVolumeClaim": map[string]any{"claimName": claim}}}}}
}

// Owner's decision of 2026-10-09: the volume claims left after an uninstall can be deleted — but
// only with the name typed, never in a system namespace, and not while a pod still mounts the claim.
func TestDeletePVCGuardRails(t *testing.T) {
	hs := newHarness(t, "token")
	hs.api.canned = map[string]any{
		"/api/v1/namespaces/app/persistentvolumeclaims/data": pvcObj("app", "data"),
		"/api/v1/namespaces/app/persistentvolumeclaims/busy": pvcObj("app", "busy"),
		"/api/v1/namespaces/app/pods": map[string]any{"kind": "PodList", "apiVersion": "v1", "items": []any{
			podUsing("app", "db-0", "busy"), podUsing("app", "other", "unrelated"),
		}},
	}
	deleted := func() int { hs.api.mu.Lock(); defer hs.api.mu.Unlock(); return len(hs.api.deleted) }

	for name, c := range map[string]struct {
		ns, claim, body string
		want            int
	}{
		"no confirmation":       {"app", "data", `{}`, 400},
		"wrong confirmation":    {"app", "data", `{"confirmName":"nope"}`, 400},
		"not json":              {"app", "data", `x`, 400},
		"a system namespace":    {"kube-system", "data", `{"confirmName":"data"}`, 400},
		"a malformed name":      {"app", "Bad_Name", `{"confirmName":"Bad_Name"}`, 400},
		"a pod still mounts it": {"app", "busy", `{"confirmName":"busy"}`, 409},
	} {
		w := hs.deletePVC("viewer", c.ns, c.claim, c.body)
		if w.Code != c.want {
			t.Errorf("%s: %d %s, want %d", name, w.Code, w.Body, c.want)
		}
		if deleted() != 0 {
			t.Fatalf("%s: something was deleted: %v", name, hs.api.deleted)
		}
	}
	if w := hs.deletePVC("viewer", "app", "busy", `{"confirmName":"busy"}`); !strings.Contains(w.Body.String(), "db-0") {
		t.Errorf("the refusal should name the pod: %s", w.Body)
	}

	// the happy path: the right name, nothing mounting it — a DELETE reaches the API, as the caller
	w := hs.deletePVC("viewer", "app", "data", `{"confirmName":"data"}`)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if deleted() != 1 || hs.api.deleted[0] != "/api/v1/namespaces/app/persistentvolumeclaims/data" {
		t.Errorf("deleted = %v", hs.api.deleted)
	}
	for _, auth := range hs.api.seen {
		if strings.Contains(auth, "backend-token") {
			t.Fatal("a call used Vinaire's own identity instead of the caller's")
		}
	}
	// no session, no deletion
	r := httptest.NewRequest("DELETE", "/x", strings.NewReader(`{"confirmName":"data"}`))
	w2 := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(hs.h.DeletePVC)).ServeHTTP(w2, r)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", w2.Code)
	}
}
