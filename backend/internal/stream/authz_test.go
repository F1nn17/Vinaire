package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/rest"

	"github.com/f1nn17/vinaire/backend/internal/api"
	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
)

func podEvent(ns, name string) Event {
	return Event{Type: "modified", Kind: "pods", Row: api.WorkloadRow{Name: name, Namespace: ns}}
}

func TestEventAuthorizerFailsClosedAndCaches(t *testing.T) {
	var calls atomic.Int32
	failing := false
	a := newEventAuthorizer(func(_ context.Context, group, resource, ns string) (bool, error) {
		calls.Add(1)
		if failing {
			return false, errors.New("api down")
		}
		return resource == "pods" && ns == "team-a", nil
	})
	now := time.Now()
	a.now = func() time.Time { return now }
	ctx := context.Background()

	if !a.allow(ctx, podEvent("team-a", "p1")) {
		t.Error("an allowed namespace was dropped")
	}
	if a.allow(ctx, podEvent("team-b", "p1")) {
		t.Error("a namespace the user can't list was sent")
	}
	for range 50 {
		a.allow(ctx, podEvent("team-a", "again"))
		a.allow(ctx, podEvent("team-b", "again"))
	}
	if calls.Load() != 2 {
		t.Errorf("%d access checks for two (kind, namespace) pairs — answers aren't cached", calls.Load())
	}

	// the answer expires, so a revoked permission takes effect within seconds
	now = now.Add(authzCacheTTL + time.Second)
	a.allow(ctx, podEvent("team-a", "p1"))
	if calls.Load() != 3 {
		t.Errorf("an expired answer wasn't re-checked (%d calls)", calls.Load())
	}

	// unknown kinds and unknown row shapes are refused, not guessed
	if a.allow(ctx, Event{Kind: "secrets", Row: api.WorkloadRow{Namespace: "team-a"}}) {
		t.Error("an unknown kind was allowed")
	}
	if a.allow(ctx, Event{Kind: "pods", Row: "not a row"}) {
		t.Error("an unknown row shape was allowed")
	}

	// when the check itself fails the event is dropped, and the failure isn't cached
	failing = true
	if a.allow(ctx, podEvent("team-c", "p")) {
		t.Error("an event was sent although the access check failed")
	}
	failing = false
	if a.allow(ctx, podEvent("team-c", "p")) {
		t.Error("team-c isn't granted")
	}
	if calls.Load() < 5 {
		t.Error("a failed check was cached")
	}
}

// fakeAPIServer answers SelfSubjectAccessReviews: the token "scoped" may list pods in
// team-a only; "admin" may list everything.
func fakeAPIServer(t *testing.T, asked *atomic.Int32) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/selfsubjectaccessreviews") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		asked.Add(1)
		body, _ := io.ReadAll(r.Body)
		var review struct {
			Spec struct {
				ResourceAttributes struct{ Namespace, Verb, Resource string } `json:"resourceAttributes"`
			} `json:"spec"`
		}
		_ = json.Unmarshal(body, &review)
		ra := review.Spec.ResourceAttributes
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		allowed := token == "admin" || (token == "scoped" && ra.Verb == "list" && ra.Resource == "pods" && ra.Namespace == "team-a")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind": "SelfSubjectAccessReview", "apiVersion": "authorization.k8s.io/v1",
			"status": map[string]any{"allowed": allowed},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readEvents(t *testing.T, c *websocket.Conn, want int, within time.Duration) []Event {
	t.Helper()
	var got []Event
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var e Event
		if err := c.ReadJSON(&e); err != nil {
			if len(got) >= want {
				break
			}
			continue
		}
		got = append(got, e)
	}
	return got
}

// The Definition of Done for this item: a token that may only read pods in one
// namespace sees only that namespace through the live stream, even though the
// informers (and so the hub) carry the whole cluster.
func TestStreamIsScopedToWhatTheCallerMayList(t *testing.T) {
	var asked atomic.Int32
	fake := fakeAPIServer(t, &asked)
	cfg := &rest.Config{Host: fake.URL, BearerToken: "backend-token", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}
	cfg.ContentType = "application/json"
	entry, err := clusters.EntryFromRESTConfig("home", cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	reg := clusters.NewRegistry()
	reg.Add(entry)
	mgr, err := authn.NewManager(context.Background(), authn.ManagerConfig{AuthMode: "token"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"scoped", "admin"} {
		_ = mgr.Sessions.Create(&authn.Session{ID: "s-" + tok, K8sToken: tok, ExpiresAt: time.Now().Add(time.Hour)})
	}
	hub := NewHub()
	hubs := NewHubRegistry()
	hubs.Set("home", hub)
	srv := httptest.NewServer(ServeWS(hubs, reg, "home", nil, mgr))
	defer srv.Close()

	dial := func(token string) *websocket.Conn {
		h := http.Header{}
		if token != "" {
			h.Add("Cookie", "vinaire_session=s-"+token)
		}
		c, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), h)
		if err != nil {
			if token == "" && resp != nil && resp.StatusCode == http.StatusUnauthorized {
				return nil
			}
			t.Fatalf("dial as %q: %v", token, err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	if dial("") != nil {
		t.Error("an unauthenticated WebSocket was accepted")
	}
	scoped, admin := dial("scoped"), dial("admin")
	time.Sleep(200 * time.Millisecond) // both subscribed

	hub.Broadcast(podEvent("team-b", "secret-pod"))
	hub.Broadcast(Event{Type: "added", Kind: "deployments", Row: workloadRow("team-a", "dep")})
	hub.Broadcast(Event{Type: "added", Kind: "nodes", Row: nodeRowFor("node-1")})
	hub.Broadcast(podEvent("team-a", "visible-pod"))

	got := readEvents(t, scoped, 1, 3*time.Second)
	if len(got) != 1 || got[0].Kind != "pods" {
		t.Fatalf("the scoped user received %+v, want exactly the team-a pod", got)
	}
	if row, _ := json.Marshal(got[0].Row); !strings.Contains(string(row), "visible-pod") {
		t.Errorf("wrong event: %s", row)
	}

	all := readEvents(t, admin, 4, 3*time.Second)
	if len(all) != 4 {
		t.Errorf("the admin should receive all 4 events, got %d", len(all))
	}
	if asked.Load() == 0 {
		t.Error("the cluster was never asked")
	}
}

func workloadRow(ns, name string) api.WorkloadRow { return api.WorkloadRow{Name: name, Namespace: ns} }

func nodeRowFor(name string) api.NodeRow { return api.NodeRow{Name: name} }
