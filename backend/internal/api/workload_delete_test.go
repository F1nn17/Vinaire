package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func (hs *harness) deleteWorkloadCall(handler http.HandlerFunc, token, namespace, name string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("DELETE", "/x", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("name", name)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	}
	w := httptest.NewRecorder()
	hs.mgr.Middleware(handler).ServeHTTP(w, r)
	return w
}

func TestDeleteStatefulSetDaemonSetAndJob(t *testing.T) {
	hs := newHarness(t, "token")
	cases := map[string]struct {
		handler http.HandlerFunc
		path    string
	}{
		"statefulset": {hs.h.DeleteStatefulSet, "/apis/apps/v1/namespaces/app/statefulsets/db"},
		"daemonset":   {hs.h.DeleteDaemonSet, "/apis/apps/v1/namespaces/app/daemonsets/db"},
		"job":         {hs.h.DeleteJob, "/apis/batch/v1/namespaces/app/jobs/db"},
	}
	n := 0
	for name, c := range cases {
		if w := hs.deleteWorkloadCall(c.handler, "", "app", "db"); w.Code != http.StatusUnauthorized {
			t.Errorf("%s with no session: %d", name, w.Code)
		}
		if w := hs.deleteWorkloadCall(c.handler, "viewer", "app", "Bad_Name"); w.Code != http.StatusBadRequest {
			t.Errorf("%s with a malformed name: %d", name, w.Code)
		}
		if len(hs.api.deleted) != n {
			t.Fatalf("%s: something was deleted without a valid request: %v", name, hs.api.deleted)
		}
		if w := hs.deleteWorkloadCall(c.handler, "viewer", "app", "db"); w.Code != 200 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
		n++
		if len(hs.api.deleted) != n || hs.api.deleted[n-1] != c.path {
			t.Errorf("%s: deleted = %v, want %s last", name, hs.api.deleted, c.path)
		}
	}
	for _, auth := range hs.api.seen {
		if strings.Contains(auth, "backend-token") {
			t.Fatal("a call used Vinaire's own identity instead of the caller's")
		}
	}
}
