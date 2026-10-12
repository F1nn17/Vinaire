package helm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/f1nn17/vinaire/backend/internal/netguard"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

func TestDefaultConfigIsValid(t *testing.T) {
	c := DefaultConfig()
	if len(c.Repos) == 0 {
		t.Fatal("no repositories")
	}
	if _, ok := c.Repo("ingress-nginx"); !ok {
		t.Error("ingress-nginx should be in the default catalog")
	}
}

func TestConfigValidateRejects(t *testing.T) {
	cases := map[string]Config{
		"empty":        {},
		"http":         {Repos: []Repo{{Name: "a", URL: "http://example.com/charts"}}},
		"no host":      {Repos: []Repo{{Name: "a", URL: "https://"}}},
		"credentials":  {Repos: []Repo{{Name: "a", URL: "https://user:pw@example.com"}}},
		"query":        {Repos: []Repo{{Name: "a", URL: "https://example.com/?token=x"}}},
		"bad name":     {Repos: []Repo{{Name: "A_b", URL: "https://example.com"}}},
		"duplicate":    {Repos: []Repo{{Name: "a", URL: "https://example.com"}, {Name: "a", URL: "https://example.org"}}},
		"file scheme":  {Repos: []Repo{{Name: "a", URL: "file:///etc/passwd"}}},
		"empty string": {Repos: []Repo{{Name: "", URL: "https://example.com"}}},
	}
	for name, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseConfigRejectsUnknownFields(t *testing.T) {
	if _, err := parseConfig([]byte(`{"repos":[{"name":"a","url":"https://example.com","insecure":true}]}`)); err == nil {
		t.Error("an unknown field was silently accepted")
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	path := t.TempDir() + "/c.json"
	if err := os.WriteFile(path, []byte(`{"repos":[{"name":"mine","url":"https://charts.example.com"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil || len(c.Repos) != 1 || c.Repos[0].Name != "mine" {
		t.Fatalf("LoadConfig = %v, %v", c, err)
	}
}

func TestForbiddenAddr(t *testing.T) {
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "169.254.169.254": true, "0.0.0.0": true,
		"224.0.0.1": true, "fe80::1": true,
		"10.0.0.5": false, "192.168.1.1": false, "93.184.216.34": false, "2606:4700::1": false,
	} {
		if got := netguard.Forbidden(net.ParseIP(ip)); got != want {
			t.Errorf("Forbidden(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestSafeClientRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "secret") }))
	defer srv.Close()
	c := newCatalog(DefaultConfig(), safeClient(false))
	_, err := c.fetchLimited(context.Background(), srv.URL, 1024)
	if err == nil || !strings.Contains(err.Error(), "refusing to connect") {
		t.Fatalf("a loopback address was reachable: %v", err)
	}
}

// --- a fake repository -------------------------------------------------------

type fakeRepo struct {
	srv       *httptest.Server
	chartSum  string // digest advertised in the index
	chartData []byte
}

func buildChart(t *testing.T, name, version string, values map[string]any, schema bool) []byte {
	t.Helper()
	raw, err := yaml.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	ch := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: name, Version: version, Description: "a test chart", Type: "application"},
		Values:   values,
		Raw:      []*chart.File{{Name: "values.yaml", Data: raw}},
		Files:    []*chart.File{{Name: "README.md", Data: []byte("# readme")}},
		Templates: []*chart.File{
			{Name: "templates/cm.yaml", Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n")},
		},
	}
	if schema {
		ch.Schema = []byte(`{"type":"object"}`)
	}
	path, err := chartutil.Save(ch, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newFakeRepo(t *testing.T, values map[string]any, mutate func(*fakeRepo)) *fakeRepo {
	t.Helper()
	f := &fakeRepo{chartData: buildChart(t, "demo", "1.2.3", values, true)}
	sum := sha256.Sum256(f.chartData)
	f.chartSum = hex.EncodeToString(sum[:])
	if mutate != nil {
		mutate(f)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - name: demo
      version: 1.2.3
      appVersion: "9.9"
      description: A demo application
      keywords: [database, demo]
      digest: %q
      urls: [demo-1.2.3.tgz]
    - name: demo
      version: 1.0.0
      digest: %q
      urls: [demo-1.2.3.tgz]
  other:
    - name: other
      version: 0.1.0
      description: something else
      urls: [other-0.1.0.tgz]
`, f.chartSum, f.chartSum)
	})
	mux.HandleFunc("/demo-1.2.3.tgz", func(w http.ResponseWriter, _ *http.Request) { w.Write(f.chartData) })
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRepo) catalog() *Catalog {
	return newCatalog(Config{Repos: []Repo{{Name: "fake", URL: f.srv.URL}}}, f.srv.Client())
}

func TestSearchAndDetails(t *testing.T) {
	f := newFakeRepo(t, map[string]any{"replicas": 1, "nodeSelector": map[string]any{}}, nil)
	c := f.catalog()

	res := c.Search(context.Background(), "", "", "")
	if len(res.Charts) != 2 || res.Errors != nil {
		t.Fatalf("search all = %+v", res)
	}
	res = c.Search(context.Background(), "DATABASE", "", "")
	if len(res.Charts) != 1 || res.Charts[0].Name != "demo" || res.Charts[0].Version != "1.2.3" || res.Charts[0].Repo != "fake" {
		t.Fatalf("search by keyword = %+v", res)
	}
	if got := c.Search(context.Background(), "demo", "nope", ""); len(got.Charts) != 0 {
		t.Errorf("an unknown repository filter returned results: %+v", got)
	}

	d, err := c.Details(context.Background(), "fake", "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Schema != `{"type":"object"}` {
		t.Errorf("the chart's values.schema.json wasn't returned for the form: %q", d.Schema)
	}
	if d.Version != "1.2.3" || len(d.Versions) != 2 || d.Readme != "# readme" || !d.HasSchema || !d.SupportsNodeSelector {
		t.Errorf("details = %+v", d)
	}
	if !strings.Contains(d.Values, "replicas: 1") {
		t.Errorf("default values missing: %q", d.Values)
	}
	if _, err := c.Details(context.Background(), "fake", "demo", "9.9.9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown version: %v", err)
	}
	if _, err := c.Details(context.Background(), "fake", "missing", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown chart: %v", err)
	}
	if _, err := c.Details(context.Background(), "ghost", "demo", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo: %v", err)
	}
}

func TestChartWithoutNodeSelectorValue(t *testing.T) {
	f := newFakeRepo(t, map[string]any{"replicas": 1}, nil)
	f.chartData = buildChart(t, "demo", "1.2.3", map[string]any{"replicas": 1}, false)
	sum := sha256.Sum256(f.chartData)
	f.chartSum = hex.EncodeToString(sum[:])
	d, err := f.catalog().Details(context.Background(), "fake", "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.SupportsNodeSelector || d.HasSchema {
		t.Errorf("details = %+v", d)
	}
}

func TestChartDigestIsVerified(t *testing.T) {
	f := newFakeRepo(t, map[string]any{"nodeSelector": map[string]any{}}, func(f *fakeRepo) {
		f.chartSum = strings.Repeat("ab", 32) // advertises a digest the download won't match
	})
	if _, err := f.catalog().LoadChart(context.Background(), "fake", "demo", ""); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("a tampered chart was accepted: %v", err)
	}
}

func TestChartWithoutDigestIsRefused(t *testing.T) {
	f := newFakeRepo(t, nil, nil)
	c := f.catalog()
	// "other" has no digest in the index
	if _, err := c.LoadChart(context.Background(), "fake", "other", ""); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("an unverifiable chart was accepted: %v", err)
	}
}

func TestChartMustBeServedOverHTTPS(t *testing.T) {
	mux := http.NewServeMux()
	var sum string
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "apiVersion: v1\nentries:\n  demo:\n    - name: demo\n      version: 1.0.0\n      digest: %s\n      urls: [\"http://127.0.0.1:1/demo.tgz\"]\n", sum)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	sum = strings.Repeat("cd", 32)
	c := newCatalog(Config{Repos: []Repo{{Name: "fake", URL: srv.URL}}}, srv.Client())
	if _, err := c.LoadChart(context.Background(), "fake", "demo", ""); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("a plain-http chart URL was accepted: %v", err)
	}
}

func TestBrokenRepositoryIsReportedNotFatal(t *testing.T) {
	f := newFakeRepo(t, map[string]any{"nodeSelector": map[string]any{}}, nil)
	dead := httptest.NewTLSServer(http.NotFoundHandler())
	defer dead.Close()
	c := newCatalog(Config{Repos: []Repo{{Name: "fake", URL: f.srv.URL}, {Name: "dead", URL: dead.URL}}}, f.srv.Client())
	// both test servers use different CAs; trust both
	res := c.Search(context.Background(), "demo", "", "")
	if len(res.Charts) == 0 && res.Errors["dead"] == "" {
		t.Fatalf("neither results nor an error reported: %+v", res)
	}
	if res.Errors["dead"] == "" {
		t.Errorf("the dead repository wasn't reported: %+v", res)
	}
}

func TestIndexIsCachedAndExpires(t *testing.T) {
	hits := 0
	var mu sync.Mutex
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		fmt.Fprint(w, "apiVersion: v1\nentries: {}\n")
	}))
	defer srv.Close()
	c := newCatalog(Config{Repos: []Repo{{Name: "fake", URL: srv.URL}}}, srv.Client())
	now := time.Now()
	c.now = func() time.Time { return now }
	for range 3 {
		c.Search(context.Background(), "", "", "")
	}
	if hits != 1 {
		t.Fatalf("index fetched %d times, want 1", hits)
	}
	now = now.Add(indexTTL + time.Second)
	c.Search(context.Background(), "", "", "")
	// the stale index is served at once and refreshed in the background
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := hits
		mu.Unlock()
		if n == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("index fetched %d times after expiry, want 2", hits)
}

func TestAStaleIndexIsServedWithoutWaitingForTheRefresh(t *testing.T) {
	release := make(chan struct{})
	var calls int
	var cmu sync.Mutex
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cmu.Lock()
		calls++
		n := calls
		cmu.Unlock()
		if n > 1 {
			<-release // the refresh is slow
		}
		fmt.Fprint(w, "apiVersion: v1\nentries:\n  demo:\n    - {name: demo, version: 1.0.0}\n")
	}))
	defer srv.Close()
	defer close(release)
	c := newCatalog(Config{Repos: []Repo{{Name: "fake", URL: srv.URL}}}, srv.Client())
	now := time.Now()
	c.now = func() time.Time { return now }
	if res := c.Search(context.Background(), "", "", ""); len(res.Charts) != 1 {
		t.Fatalf("first search: %+v", res)
	}
	now = now.Add(indexTTL + time.Second)
	start := time.Now()
	res := c.Search(context.Background(), "", "", "")
	if time.Since(start) > 2*time.Second || len(res.Charts) != 1 || len(res.Pending) != 0 {
		t.Errorf("the stale index should answer at once, took %v with %+v", time.Since(start), res)
	}
}

func TestUnmaintainedRepositoriesAreLeftOutUnlessAskedFor(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "apiVersion: v1\nentries:\n  demo:\n    - {name: demo, version: 1.0.0}\n")
	}))
	defer srv.Close()
	c := newCatalog(Config{Repos: []Repo{{Name: "good", URL: srv.URL}, {Name: "old", URL: srv.URL, Unmaintained: true}}}, srv.Client())
	res := c.SearchOpts(context.Background(), "", "", "", SearchOptions{})
	if len(res.Charts) != 1 || res.Charts[0].Repo != "good" || res.HiddenUnmaintained != 1 {
		t.Errorf("default search: %+v", res)
	}
	if res := c.SearchOpts(context.Background(), "", "", "", SearchOptions{IncludeUnmaintained: true}); len(res.Charts) != 2 {
		t.Errorf("asked for: %+v", res)
	}
	// naming the repository is an explicit choice
	res = c.SearchOpts(context.Background(), "", "old", "", SearchOptions{})
	if len(res.Charts) != 1 || !res.Charts[0].Unmaintained || res.HiddenUnmaintained != 0 {
		t.Errorf("named repository: %+v", res)
	}
}

func TestFetchLimitedRefusesOversizeBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 5000))
	}))
	defer srv.Close()
	c := newCatalog(DefaultConfig(), srv.Client())
	if _, err := c.fetchLimited(context.Background(), srv.URL, 1000); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("oversize body: %v", err)
	}
	if b, err := c.fetchLimited(context.Background(), srv.URL, 5000); err != nil || len(b) != 5000 {
		t.Fatalf("exact-size body: %d bytes, %v", len(b), err)
	}
}

// --- values and releases -----------------------------------------------------

func TestParseValues(t *testing.T) {
	v, err := parseValues("a: 1\nb:\n  c: [x, y]\n")
	if err != nil || v["a"] != float64(1) {
		t.Fatalf("parseValues = %v, %v", v, err)
	}
	if v, err := parseValues("  \n"); err != nil || len(v) != 0 {
		t.Errorf("blank values = %v, %v", v, err)
	}
	if _, err := parseValues("a: [unclosed"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad YAML: %v", err)
	}
	if _, err := parseValues("- a\n- b\n"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a YAML list is not a values map: %v", err)
	}
	if _, err := parseValues(strings.Repeat("a: 1\n", maxValuesBytes)); !errors.Is(err, ErrInvalid) {
		t.Errorf("oversize values: %v", err)
	}
}

func TestPinToNode(t *testing.T) {
	withSel := &chart.Chart{Values: map[string]any{"nodeSelector": map[string]any{}}}
	without := &chart.Chart{Values: map[string]any{"replicas": 1}}

	vals := map[string]any{}
	if err := pinToNode(vals, withSel, "worker-1"); err != nil {
		t.Fatal(err)
	}
	if vals["nodeSelector"].(map[string]any)["kubernetes.io/hostname"] != "worker-1" {
		t.Errorf("vals = %v", vals)
	}

	// existing selector entries are kept
	vals = map[string]any{"nodeSelector": map[string]any{"disk": "ssd"}}
	if err := pinToNode(vals, withSel, "worker-1"); err != nil {
		t.Fatal(err)
	}
	sel := vals["nodeSelector"].(map[string]any)
	if sel["disk"] != "ssd" || sel["kubernetes.io/hostname"] != "worker-1" {
		t.Errorf("selector = %v", sel)
	}

	if err := pinToNode(map[string]any{}, without, "worker-1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a chart with no nodeSelector value should be refused, got %v", err)
	}
	if err := pinToNode(map[string]any{"nodeSelector": "oops"}, withSel, "worker-1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("non-map nodeSelector: %v", err)
	}
	for _, bad := range []string{"", "Node_1", "a b", "x;rm", strings.Repeat("a", 300)} {
		if err := pinToNode(map[string]any{}, withSel, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("node name %q accepted", bad)
		}
	}
}

func TestInstallValidatesBeforeTouchingAnything(t *testing.T) {
	rl := &Releases{Catalog: newCatalog(DefaultConfig(), safeClient(false))}
	cfg := &rest.Config{Host: "https://127.0.0.1:1"}
	cases := []InstallRequest{
		{Repo: "ingress-nginx", Chart: "ingress-nginx", Namespace: "ns", Release: "Bad_Name"},
		{Repo: "ingress-nginx", Chart: "ingress-nginx", Namespace: "NS", Release: "ok"},
		{Repo: "ingress-nginx", Chart: "ingress-nginx", Namespace: "ns", Release: "ok", ValuesYAML: "a: [oops"},
		{Repo: "ingress-nginx", Chart: "ingress-nginx", Namespace: "ns", Release: strings.Repeat("a", 60)},
	}
	for _, req := range cases {
		if _, err := rl.Install(context.Background(), cfg, req); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: want ErrInvalid, got %v", req, err)
		}
	}
	// the other actions validate names too
	if err := rl.Uninstall(context.Background(), cfg, "ns", "Bad Name"); !errors.Is(err, ErrInvalid) {
		t.Errorf("uninstall: %v", err)
	}
	if err := rl.Rollback(context.Background(), cfg, "ns", "ok", 0); !errors.Is(err, ErrInvalid) {
		t.Errorf("rollback revision 0: %v", err)
	}
	if _, err := rl.List(context.Background(), cfg, "Bad NS"); !errors.Is(err, ErrInvalid) {
		t.Errorf("list: %v", err)
	}
}

func TestClassify(t *testing.T) {
	if classify(nil) != nil {
		t.Error("nil should stay nil")
	}
	if err := classify(driver.ErrReleaseNotFound); !errors.Is(err, ErrNotFound) {
		t.Errorf("release not found: %v", err)
	}
	if err := classify(errors.New("cannot re-use a name that is still in use")); !errors.Is(err, ErrConflict) {
		t.Errorf("name in use: %v", err)
	}
	err := classify(errors.New(`secrets is forbidden: User "x" cannot create resource "secrets"`))
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "cannot create") {
		t.Errorf("forbidden: %v", err)
	}
	err = classify(errors.New(`resource mapping not found: no matches for kind "ServiceMonitor" in version "monitoring.coreos.com/v1"`))
	if err == nil || !strings.Contains(err.Error(), "CRDs") || !strings.Contains(err.Error(), "ServiceMonitor") {
		t.Errorf("CRD failure should carry an explicit hint, got %v", err)
	}
	plain := errors.New("something else")
	if classify(plain) != plain {
		t.Error("unrecognised errors should pass through unchanged")
	}
}

func TestRESTGetterCarriesIdentityAndNamespace(t *testing.T) {
	cfg := &rest.Config{Host: "https://k8s.example:6443", BearerToken: "caller-token"}
	cfg.TLSClientConfig.CAData = []byte("ca")
	g := restGetter{cfg: cfg, namespace: "apps"}

	loader := g.ToRawKubeConfigLoader()
	if ns, _, err := loader.Namespace(); err != nil || ns != "apps" {
		t.Errorf("namespace = %q, %v", ns, err)
	}
	rc, err := loader.ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	if rc.Host != "https://k8s.example:6443" || rc.BearerToken != "caller-token" || string(rc.TLSClientConfig.CAData) != "ca" {
		t.Errorf("identity lost: %+v", rc)
	}
	// the copy returned for use can't be used to change the original
	out, _ := g.ToRESTConfig()
	out.BearerToken = "changed"
	if cfg.BearerToken != "caller-token" {
		t.Error("ToRESTConfig exposed the shared config")
	}
}

func TestRolesTellOperatorsAndClustersFromSingleInstances(t *testing.T) {
	cnpg := Repo{Name: "cnpg", Roles: map[string]string{"cloudnative-pg": "operator", "cluster": "cluster"}}
	for repo, c := range map[string]struct{ chart, want string }{
		"cnpg-operator":  {"cloudnative-pg", "operator"},
		"cnpg-cluster":   {"cluster", "cluster"},
		"name operator":  {"pg-operator", "operator"},
		"name ha":        {"postgresql-ha", "cluster"},
		"percona db":     {"pg-db", "cluster"},
		"plain database": {"postgresql", ""},
		"plain redis":    {"redis", ""},
	} {
		r := Repo{}
		if strings.HasPrefix(repo, "cnpg") {
			r = cnpg
		}
		if got := Role(r, c.chart); got != c.want {
			t.Errorf("%s: Role(%q) = %q, want %q", repo, c.chart, got, c.want)
		}
	}
	if !roleMatches("single", "") || roleMatches("single", "operator") || !roleMatches("", "cluster") || !roleMatches("operator", "operator") {
		t.Error("roleMatches")
	}
}

func TestNotesLoseTheirColourCodes(t *testing.T) {
	esc := string(rune(27))
	if got := ansiRe.ReplaceAllString(esc+"[0;34mImage"+esc+"[0m  "+esc+"[0;31mDisabled"+esc+"[0m", ""); got != "Image  Disabled" {
		t.Errorf("got %q", got)
	}
}

// A chart download may take much longer than an index request: a slow link to the chart's host must
// not turn an upgrade into "context deadline exceeded" after 30 seconds.
func TestChartDownloadsHaveALongerTimeCapThanIndexes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte("chart bytes"))
	}))
	defer srv.Close()

	client := srv.Client()
	client.Timeout = 100 * time.Millisecond // the short cap of an index request
	c := newCatalog(Config{}, client)

	if _, err := c.fetchLimited(context.Background(), srv.URL, 1<<10); err == nil {
		t.Fatal("an index-style fetch should have hit the short cap")
	}

	old := chartTimeout
	chartTimeout = 5 * time.Second
	defer func() { chartTimeout = old }()
	got, err := c.fetchWithin(context.Background(), srv.URL, 1<<10, c.chartClient())
	if err != nil || string(got) != "chart bytes" {
		t.Fatalf("a chart download must be given longer: %q %v", got, err)
	}
	if client.Timeout != 100*time.Millisecond {
		t.Errorf("the shared client's own cap must not change: %v", client.Timeout)
	}
}
