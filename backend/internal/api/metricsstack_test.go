package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (hs *harness) get(handler http.HandlerFunc, target, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	}
	w := httptest.NewRecorder()
	hs.mgr.Middleware(handler).ServeHTTP(w, r)
	return w
}

// Metrics are read as the caller, never with Vinaire's own identity: the service
// lookup (and, later, the proxy call) carries the signed-in user's token, so the
// cluster's RBAC decides who may see them.
func TestMetricsAreReachedAsTheCaller(t *testing.T) {
	hs := newHarness(t, "token")
	w := hs.get(hs.h.MetricsQuery, "/x?query=up", "viewer")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":false`) || !strings.Contains(w.Body.String(), "no Prometheus") {
		t.Fatalf("no Prometheus in the cluster: %d %s", w.Code, w.Body)
	}
	hs.api.mu.Lock()
	defer hs.api.mu.Unlock()
	if len(hs.api.seen) == 0 {
		t.Fatal("the cluster was never asked")
	}
	for _, a := range hs.api.seen {
		if a != "Bearer viewer" {
			t.Errorf("a request carried %q instead of the caller's token", a)
		}
	}
	if w := hs.get(hs.h.MetricsQuery, "/x?query=up", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", w.Code)
	}
}

func TestMetricsStatusAndPlan(t *testing.T) {
	hs := newHarness(t, "token")
	w := hs.get(hs.h.MetricsStatus, "/x", "viewer")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"none"`) {
		t.Errorf("status of an empty cluster: %d %s", w.Code, w.Body)
	}
	w = hs.get(hs.h.MetricsPlan, "/x?namespace=monitoring&retentionDays=7&storageSize=10Gi", "viewer")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"chart":"prometheus"`) || !strings.Contains(w.Body.String(), "default storage class") {
		t.Errorf("plan: %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{"namespace=Bad_NS", "storageSize=10Gi%0Aserver:%20x", "retentionDays=999"} {
		if w := hs.get(hs.h.MetricsPlan, "/x?"+bad, "viewer"); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
}
