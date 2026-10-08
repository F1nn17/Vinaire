package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"context"
	"github.com/go-chi/chi/v5"
)

func nsObj(name, project string) map[string]any {
	labels := map[string]any{}
	if project != "" {
		labels["vinaire.io/project"] = project
	}
	return map[string]any{"metadata": map[string]any{"name": name, "labels": labels, "creationTimestamp": "2026-01-01T00:00:00Z"}}
}

func (hs *harness) project(token, name string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/x", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	w := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(hs.h.ProjectDetail)).ServeHTTP(w, r)
	return w
}

func TestProjectsAreNamespacesWithALabel(t *testing.T) {
	hs := newHarness(t, "token")
	hs.api.canned = map[string]any{
		"/api/v1/namespaces": map[string]any{"kind": "NamespaceList", "apiVersion": "v1", "items": []any{
			nsObj("shop-dev", "shop"), nsObj("shop-prod", "shop"), nsObj("blog", "blog"), nsObj("kube-system", ""), nsObj("bad", "Not A Name!"),
		}},
		"/apis/rbac.authorization.k8s.io/v1/rolebindings": map[string]any{"kind": "RoleBindingList", "apiVersion": "rbac.authorization.k8s.io/v1", "items": []any{
			map[string]any{"metadata": map[string]any{"name": "alice-edit", "namespace": "shop-dev"},
				"roleRef":  map[string]any{"kind": "ClusterRole", "name": "edit", "apiGroup": "rbac.authorization.k8s.io"},
				"subjects": []any{map[string]any{"kind": "User", "name": "alice", "apiGroup": "rbac.authorization.k8s.io"}}},
			map[string]any{"metadata": map[string]any{"name": "alice-view", "namespace": "shop-prod"},
				"roleRef":  map[string]any{"kind": "ClusterRole", "name": "view", "apiGroup": "rbac.authorization.k8s.io"},
				"subjects": []any{map[string]any{"kind": "User", "name": "alice", "apiGroup": "rbac.authorization.k8s.io"}}},
			map[string]any{"metadata": map[string]any{"name": "other", "namespace": "blog"},
				"roleRef":  map[string]any{"kind": "ClusterRole", "name": "view", "apiGroup": "rbac.authorization.k8s.io"},
				"subjects": []any{map[string]any{"kind": "Group", "name": "writers", "apiGroup": "rbac.authorization.k8s.io"}}},
		}},
	}

	// the list groups namespaces by the label and ignores a value that isn't a valid name
	w := hs.get(hs.h.Projects, "/x", "viewer")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	var rows []ProjectRow
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "blog" || rows[1].Name != "shop" {
		t.Fatalf("projects: %+v", rows)
	}
	if got := strings.Join(rows[1].Namespaces, ","); got != "shop-dev,shop-prod" {
		t.Errorf("shop namespaces: %s", got)
	}
	if rows[1].Members != 1 || rows[0].Members != 1 {
		t.Errorf("members: shop=%d blog=%d (alice holds two bindings in shop and counts once)", rows[1].Members, rows[0].Members)
	}

	// the detail: members with their grants, only inside the project
	w = hs.project("viewer", "shop")
	var d ProjectDetail
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if w.Code != 200 || len(d.Namespaces) != 2 || !d.MembersKnown || len(d.Members) != 1 || len(d.Members[0].Grants) != 2 {
		t.Fatalf("detail: %d %s", w.Code, w.Body)
	}
	if d.Members[0].Name != "alice" || strings.Contains(w.Body.String(), "writers") {
		t.Errorf("a member of another project leaked in: %s", w.Body)
	}
	if w := hs.project("viewer", "nope"); w.Code != http.StatusNotFound {
		t.Errorf("unknown project: %d", w.Code)
	}
	for _, bad := range []string{"Bad Name", "../x", "a_b", strings.Repeat("a", 60)} {
		if w := hs.project("viewer", bad); w.Code != http.StatusBadRequest {
			t.Errorf("%q: %d", bad, w.Code)
		}
	}
	// reads are made as the caller
	hs.api.mu.Lock()
	defer hs.api.mu.Unlock()
	for _, a := range hs.api.seen {
		if a != "Bearer viewer" {
			t.Errorf("a request carried %q instead of the caller's token", a)
		}
	}
}
