package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (hs *harness) logsSearch(token, ns, pod string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/x?query=boom&since=1h", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", ns)
	rctx.URLParams.Add("name", pod)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: "sess-" + token})
	}
	w := httptest.NewRecorder()
	hs.mgr.Middleware(http.HandlerFunc(hs.h.LogsSearch)).ServeHTTP(w, r)
	return w
}

// The history search reads from Loki, which holds every pod's logs and doesn't know
// who is asking. Without the check made here, anyone signed in could read any
// namespace's logs. This is the property that was missing.
func TestLogSearchRequiresPermissionToReadThatPodsLogs(t *testing.T) {
	hs := newHarness(t, "token")
	var lokiHits atomic.Int32
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lokiHits.Add(1)
		_, _ = w.Write([]byte(`{"data":{"result":[]}}`))
	}))
	t.Cleanup(loki.Close)
	hs.h.LokiURL = loki.URL
	hs.api.grant("admin", "/pods/get")
	hs.api.grant("viewer", "/pods/list") // can list pods, may not read their logs

	w := hs.logsSearch("viewer", "secret-ns", "db-0")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "pods/log") {
		t.Errorf("viewer: %d %s", w.Code, w.Body)
	}
	if lokiHits.Load() != 0 {
		t.Errorf("Loki was queried %d time(s) for someone who may not read these logs", lokiHits.Load())
	}
	if w := hs.logsSearch("", "secret-ns", "db-0"); w.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", w.Code)
	}
	w = hs.logsSearch("admin", "secret-ns", "db-0")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":true`) || lokiHits.Load() != 1 {
		t.Errorf("admin: %d %s (loki hits %d)", w.Code, w.Body, lokiHits.Load())
	}
	// parameters that end up in a LogQL selector are validated
	for _, bad := range [][2]string{{"../x", "p"}, {"ns", "a b"}, {"ns", `p"} or {x="`}} {
		if w := hs.logsSearch("admin", bad[0], bad[1]); w.Code != http.StatusBadRequest {
			t.Errorf("namespace %q pod %q: %d", bad[0], bad[1], w.Code)
		}
	}
}

func TestStorageClassHelpers(t *testing.T) {
	def := storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "local-path", Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}}}
	other := storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}}
	if !hasDefaultStorageClass([]storagev1.StorageClass{other, def}) || hasDefaultStorageClass([]storagev1.StorageClass{other}) {
		t.Error("default storage class detection")
	}
	if !hasStorageClass([]storagev1.StorageClass{other, def}, "fast") || hasStorageClass([]storagev1.StorageClass{other}, "nope") {
		t.Error("storage class lookup")
	}
}
