package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/clusters"
)

// ---- OriginGuard -----------------------------------------------------------------

func TestOriginGuard(t *testing.T) {
	reached := false
	h := OriginGuard([]string{"https://vinaire.example.com", "http://localhost:3100"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
	}))
	cases := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		host    string
		want    int
	}{
		{"cross-site POST", "POST", "/api/v1/clusters", map[string]string{"Origin": "https://evil.example"}, "vinaire.example.com", 403},
		{"cross-site DELETE", "DELETE", "/api/v1/clusters/x", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}, "vinaire.example.com", 403},
		{"null origin", "POST", "/api/v1/auth/login", map[string]string{"Origin": "null"}, "vinaire.example.com", 403},
		{"cross-site, no Origin", "POST", "/api/v1/x", map[string]string{"Sec-Fetch-Site": "cross-site"}, "vinaire.example.com", 403},
		{"same origin", "POST", "/api/v1/x", map[string]string{"Origin": "https://vinaire.example.com"}, "vinaire.example.com", 200},
		{"same origin by host", "PUT", "/api/v1/x", map[string]string{"Origin": "http://10.0.0.5:8080"}, "10.0.0.5:8080", 200},
		{"allowed origin", "POST", "/api/v1/x", map[string]string{"Origin": "http://localhost:3100"}, "localhost:8080", 200},
		{"allowed origin with a trailing slash", "POST", "/api/v1/x", map[string]string{"Origin": "http://localhost:3100/"}, "localhost:8080", 200},
		{"curl: no browser headers", "POST", "/api/v1/x", nil, "vinaire.example.com", 200},
		{"safe method is never checked", "GET", "/api/v1/x", map[string]string{"Origin": "https://evil.example"}, "vinaire.example.com", 200},
		{"preflight", "OPTIONS", "/api/v1/x", map[string]string{"Origin": "https://evil.example"}, "vinaire.example.com", 200},
		{"port-forward proxy is exempt", "POST", "/proxy/c/ns/pod/8080/save", map[string]string{"Origin": "null"}, "vinaire.example.com", 200},
		{"garbage origin", "POST", "/api/v1/x", map[string]string{"Origin": "::::"}, "vinaire.example.com", 403},
	}
	for _, c := range cases {
		reached = false
		r := httptest.NewRequest(c.method, c.path, nil)
		r.Host = c.host
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want || reached != (c.want == 200) {
			t.Errorf("%s: status %d (handler reached=%v), want %d", c.name, w.Code, reached, c.want)
		}
	}
}

// ---- a fake Kubernetes API for the permission gate and caller-identity tests ------

type fakeAPI struct {
	srv *httptest.Server
	mu  sync.Mutex
	// allowed: bearer token -> set of "group/resource/verb" it may do
	allowed map[string]map[string]bool
	seen    []string       // Authorization headers of every request
	canned  map[string]any // path -> JSON answered to a GET (tests that need real objects)
	asked   []string       // "group/resource/verb" of every SelfSubjectAccessReview
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{allowed: map[string]map[string]bool{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		auth := r.Header.Get("Authorization")
		f.seen = append(f.seen, auth)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/selfsubjectaccessreviews") {
			body, _ := io.ReadAll(r.Body)
			var review struct {
				Spec struct {
					ResourceAttributes struct{ Group, Resource, Verb string } `json:"resourceAttributes"`
				} `json:"spec"`
			}
			_ = json.Unmarshal(body, &review)
			ra := review.Spec.ResourceAttributes
			key := ra.Group + "/" + ra.Resource + "/" + ra.Verb
			f.asked = append(f.asked, key)
			token := strings.TrimPrefix(auth, "Bearer ")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"kind": "SelfSubjectAccessReview", "apiVersion": "authorization.k8s.io/v1",
				"status": map[string]any{"allowed": f.allowed[token][key]},
			})
			return
		}
		if v, ok := f.canned[r.URL.Path]; ok && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(v)
			return
		}
		// anything else (a metrics or gateway call): an empty list
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "List", "apiVersion": "v1", "items": []any{}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) grant(token string, rights ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowed[token] = map[string]bool{}
	for _, r := range rights {
		f.allowed[token][r] = true
	}
}

type harness struct {
	api  *fakeAPI
	h    *Handler
	mgr  *authn.Manager
	home *clusters.Entry
}

func newHarness(t *testing.T, mode string) *harness {
	t.Helper()
	f := newFakeAPI(t)
	cfg := &rest.Config{Host: f.srv.URL, BearerToken: "backend-token", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}
	cfg.ContentType = "application/json" // client-go speaks protobuf by default; the fake API reads JSON
	entry, err := clusters.EntryFromRESTConfig("home", cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	reg := clusters.NewRegistry()
	reg.Add(entry)
	mgr, err := authn.NewManager(context.Background(), authn.ManagerConfig{AuthMode: mode, CookieSameSite: "strict"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"admin", "viewer"} {
		_ = mgr.Sessions.Create(&authn.Session{ID: "sess-" + tok, K8sToken: tok, ExpiresAt: time.Now().Add(time.Hour)})
	}
	return &harness{api: f, h: NewHandler(reg, mgr, ObservabilityConfig{}), mgr: mgr, home: entry}
}

// call runs handler as the logged-in `token` ("" = no cookie) through the real auth middleware.
func (hs *harness) call(handler http.HandlerFunc, method, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/x", strings.NewReader("{}"))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	}
	w := httptest.NewRecorder()
	hs.mgr.Middleware(handler).ServeHTTP(w, r)
	return w
}

// ---- the permission gate on Vinaire's own configuration -------------------------------

func TestVinaireGateAllowsOnlyThoseWhoWereGranted(t *testing.T) {
	hs := newHarness(t, "token")
	hs.api.grant("admin", "vinaire.io/clusters/create", "vinaire.io/clusters/update", "vinaire.io/clusters/delete",
		"vinaire.io/gitops/create", "vinaire.io/gitops/update", "vinaire.io/gitops/delete", "vinaire.io/audit/get",
		"vinaire.io/auth/get", "vinaire.io/auth/create", "vinaire.io/auth/update", "vinaire.io/auth/delete")
	hs.api.grant("viewer", "/pods/list") // can do things in the cluster, but not change Vinaire

	gated := map[string]http.HandlerFunc{
		"add cluster":          hs.h.AddCluster,
		"import kubeconfig":    hs.h.ImportKubeconfig,
		"discover clusters":    hs.h.DiscoverClusters,
		"add discovered":       hs.h.AddDiscoveredCluster,
		"set cluster type":     hs.h.SetClusterType,
		"remove cluster":       hs.h.RemoveCluster,
		"ssh import":           hs.h.ClusterSSHStart("import"),
		"ssh create":           hs.h.ClusterSSHStart("create"),
		"gitops mode":          hs.h.GitOpsSetMode,
		"gitops add source":    hs.h.GitOpsAddSource,
		"gitops remove source": hs.h.GitOpsRemoveSource,
		"gitops sync source":   hs.h.GitOpsSyncSource,
		"audit viewer":         hs.h.AuditLog,
		"rotate credentials":   hs.h.RotateClusterCredentials,
		"sessions list":        hs.h.Sessions,
		"session revoke":       hs.h.RevokeSession,
		"keycloak status":      hs.h.KeycloakStatus,
		"keycloak setup":       hs.h.KeycloakSetup,
		"keycloak token login": hs.h.KeycloakTokenLogin,
		"keycloak disable":     hs.h.KeycloakDisable,
	}
	for name, handler := range gated {
		w := hs.call(handler, "POST", "viewer")
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "vinaire.io") {
			t.Errorf("%s as an ordinary user: %d %s", name, w.Code, w.Body)
		}
		if w := hs.call(handler, "POST", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s with no session: %d", name, w.Code)
		}
		// the administrator gets past the gate (whatever the handler then says about the empty body)
		if w := hs.call(handler, "POST", "admin"); w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
			t.Errorf("%s as the granted administrator was refused: %d %s", name, w.Code, w.Body)
		}
	}

	// the right asked about is the specific one for the action
	hs.api.mu.Lock()
	asked := strings.Join(hs.api.asked, " ")
	hs.api.mu.Unlock()
	for _, want := range []string{"vinaire.io/clusters/create", "vinaire.io/clusters/update", "vinaire.io/clusters/delete", "vinaire.io/gitops/create", "vinaire.io/gitops/delete"} {
		if !strings.Contains(asked, want) {
			t.Errorf("never asked the cluster about %s", want)
		}
	}
	// one right isn't another: cluster-add permission doesn't open GitOps
	hs.api.grant("viewer", "vinaire.io/clusters/create")
	if w := hs.call(hs.h.GitOpsAddSource, "POST", "viewer"); w.Code != http.StatusForbidden {
		t.Errorf("clusters.create must not imply gitops.create: %d", w.Code)
	}
}

func TestVinaireGateIsOffOnlyWithoutLogin(t *testing.T) {
	hs := newHarness(t, "disabled") // local development: no caller identity at all
	r := httptest.NewRequest("POST", "/x", nil)
	w := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hs.h.requireVinaireRight(w, r, "clusters", "create") {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("disabled auth mode must pass the gate: %d", w.Code)
	}
}

// ---- optional clients act as the caller -----------------------------------------------

func TestMetricsAndGatewayClientsUseTheCallersToken(t *testing.T) {
	hs := newHarness(t, "token")
	r := httptest.NewRequest("GET", "/x", nil)
	r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-viewer"})
	var metricsOK, gatewayOK bool
	hs.mgr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mc := hs.h.metricsFor(r, hs.home)
		gc := hs.h.gatewayFor(r, hs.home)
		metricsOK, gatewayOK = mc != nil, gc != nil
		if mc != nil {
			_, _ = mc.MetricsV1beta1().NodeMetricses().List(r.Context(), metav1.ListOptions{})
		}
		if gc != nil {
			_, _ = gc.GatewayV1().Gateways("").List(r.Context(), metav1.ListOptions{})
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
	if !metricsOK || !gatewayOK {
		t.Fatalf("clients not built (metrics=%v gateway=%v)", metricsOK, gatewayOK)
	}
	hs.api.mu.Lock()
	defer hs.api.mu.Unlock()
	if len(hs.api.seen) == 0 {
		t.Fatal("no request reached the API")
	}
	for _, auth := range hs.api.seen {
		if auth != "Bearer viewer" {
			t.Errorf("a request used %q instead of the caller's own token — it would bypass the caller's RBAC", auth)
		}
	}
}
