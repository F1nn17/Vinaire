package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/shiraku/vinaire/backend/internal/helm"
)

func TestRespondHelmErrorStatuses(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "sh.helm.release.v1.x.v1", fmt.Errorf("no"))
	cases := []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w: bad name", helm.ErrInvalid), http.StatusBadRequest},
		{fmt.Errorf("chart x: %w", helm.ErrNotFound), http.StatusNotFound},
		{fmt.Errorf("%w: exists", helm.ErrConflict), http.StatusConflict},
		{fmt.Errorf("%w: nope", helm.ErrForbidden), http.StatusForbidden},
		{fmt.Errorf("installing: %w", forbidden), http.StatusForbidden},
		{fmt.Errorf("slow: %w", context.DeadlineExceeded), http.StatusGatewayTimeout},
		{fmt.Errorf("repository unreachable"), http.StatusBadGateway},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		respondHelmError(w, c.err)
		if w.Code != c.want {
			t.Errorf("%v → %d, want %d", c.err, w.Code, c.want)
		}
	}
}

func TestCatalogEndpointsAnswer503WhenNotConfigured(t *testing.T) {
	h := &Handler{}
	for name, fn := range map[string]http.HandlerFunc{
		"repos":   h.CatalogRepos,
		"search":  h.CatalogSearch,
		"chart":   h.CatalogChart,
		"install": h.CatalogInstall,
	} {
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503", name, w.Code)
		}
	}
}

func TestCatalogSearchRejectsOversizeQuery(t *testing.T) {
	cat := helm.NewCatalog(helm.DefaultConfig())
	h := &Handler{Catalog: cat, Releases: &helm.Releases{Catalog: cat}}
	w := httptest.NewRecorder()
	h.CatalogSearch(w, httptest.NewRequest(http.MethodGet, "/?search="+strings.Repeat("a", 101), nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("oversize query → %d, want 400", w.Code)
	}
}
