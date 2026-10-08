package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/shiraku/vinaire/backend/internal/clusters"
)

type memClusterStore struct {
	mu   sync.Mutex
	list []clusters.StoredCluster
}

func (m *memClusterStore) Load(context.Context) ([]clusters.StoredCluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]clusters.StoredCluster(nil), m.list...), nil
}
func (m *memClusterStore) Save(_ context.Context, l []clusters.StoredCluster) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list = append([]clusters.StoredCluster(nil), l...)
	return nil
}
func (m *memClusterStore) Kind() string    { return "memory" }
func (m *memClusterStore) Encrypted() bool { return true }

func (hs *harness) rotate(token, cluster string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("PUT", "/x", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", cluster)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	w := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(hs.h.RotateClusterCredentials)).ServeHTTP(w, r)
	return w
}

func TestRotatingACredentialReplacesTheTokenEverywhereWithoutEchoingIt(t *testing.T) {
	hs := newHarness(t, "token")
	hs.h.ProvisionAllowInsecure = true
	store := &memClusterStore{}
	hs.h.ClusterStore = store
	hs.api.grant("admin", "vinaire.io/clusters/update")
	hs.api.grant("viewer", "/pods/list")

	sc := clusters.StoredCluster{Name: "edge", Server: hs.api.srv.URL, Token: "OLD-TOKEN-VALUE", Origin: clusters.OriginToken}
	entry, err := clusters.EntryFromStored(sc)
	if err != nil {
		t.Fatal(err)
	}
	hs.h.Clusters.AddDynamic(entry)
	_ = store.Save(context.Background(), []clusters.StoredCluster{sc})

	if w := hs.rotate("viewer", "edge", map[string]string{"token": "NEW-TOKEN-VALUE"}); w.Code != http.StatusForbidden {
		t.Errorf("an ordinary user: %d", w.Code)
	}
	if w := hs.rotate("admin", "nope", map[string]string{"token": "x"}); w.Code != http.StatusNotFound {
		t.Errorf("unknown cluster: %d", w.Code)
	}
	if w := hs.rotate("admin", "edge", map[string]string{"token": " "}); w.Code != http.StatusBadRequest {
		t.Errorf("empty token: %d", w.Code)
	}

	w := hs.rotate("admin", "edge", map[string]string{"token": "NEW-TOKEN-VALUE"})
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "TOKEN-VALUE") {
		t.Errorf("the response carries a token: %s", w.Body)
	}
	saved, _ := store.Load(context.Background())
	if len(saved) != 1 || saved[0].Token != "NEW-TOKEN-VALUE" || saved[0].Server != sc.Server {
		t.Errorf("stored: %+v", saved)
	}
	live, _ := hs.h.Clusters.Get("edge")
	if live == nil || live == entry || live.RESTConfig.BearerToken != "NEW-TOKEN-VALUE" {
		t.Errorf("the live cluster still uses the old credentials")
	}

	// a kubeconfig-based cluster is not handled here
	kc := clusters.StoredCluster{Name: "kc", Server: hs.api.srv.URL, Kubeconfig: "apiVersion: v1", Origin: clusters.OriginKubeconfig}
	kentry, _ := clusters.EntryFromStored(clusters.StoredCluster{Name: "kc", Server: hs.api.srv.URL, Token: "t"})
	hs.h.Clusters.AddDynamic(kentry)
	_ = store.Save(context.Background(), []clusters.StoredCluster{saved[0], kc})
	if w := hs.rotate("admin", "kc", map[string]string{"token": "z"}); w.Code != http.StatusConflict {
		t.Errorf("kubeconfig cluster: %d %s", w.Code, w.Body)
	}
}
