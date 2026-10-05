package helm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"helm.sh/helm/v3/pkg/chart"
)

func TestDeprecatedChartsAreHiddenUnlessAsked(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`apiVersion: v1
entries:
  fine:
    - {name: fine, version: 1.0.0, urls: [fine-1.0.0.tgz]}
  old:
    - {name: old, version: 1.0.0, deprecated: true, urls: [old-1.0.0.tgz]}
`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	c := newCatalog(Config{Repos: []Repo{{Name: "fake", URL: srv.URL}}}, srv.Client())

	res := c.Search(context.Background(), "", "", "")
	if len(res.Charts) != 1 || res.Charts[0].Name != "fine" || res.HiddenDeprecated != 1 {
		t.Errorf("default search = %+v, want only 'fine' and one hidden", res)
	}
	res = c.SearchWith(context.Background(), "", "", "", true)
	if len(res.Charts) != 2 || res.HiddenDeprecated != 0 {
		t.Errorf("search including deprecated = %+v", res)
	}
}

func TestInstallRefusesADeprecatedChartWithoutConsent(t *testing.T) {
	ch := &chart.Chart{Metadata: &chart.Metadata{Name: "old", Deprecated: true}}
	if err := refuseDeprecated(ch, false); !errors.Is(err, ErrInvalid) {
		t.Errorf("a deprecated chart wasn't refused: %v", err)
	}
	if err := refuseDeprecated(ch, true); err != nil {
		t.Errorf("explicit consent was ignored: %v", err)
	}
	if err := refuseDeprecated(&chart.Chart{Metadata: &chart.Metadata{Name: "fine"}}, false); err != nil {
		t.Errorf("a healthy chart was refused: %v", err)
	}
}
