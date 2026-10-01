package helm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// fakeRegistry is just enough of the OCI distribution API (tags, manifests,
// blobs) to serve Helm charts, over TLS, for one repository path.
type fakeRegistry struct {
	srv       *httptest.Server
	path      string // repository path on the registry, e.g. "charts"
	blobHits  atomic.Int32
	blobs     map[string][]byte // by digest string
	manifests map[string][]byte // by tag
	tags      map[string][]string
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{path: "charts", blobs: map[string][]byte{}, manifests: map[string][]byte{}, tags: map[string][]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/v2/")
		switch {
		case p == "":
			w.WriteHeader(http.StatusOK)
		case strings.Contains(p, "/tags/list"):
			name := strings.TrimSuffix(strings.TrimPrefix(p, f.path+"/"), "/tags/list")
			json.NewEncoder(w).Encode(map[string]any{"name": f.path + "/" + name, "tags": f.tags[name]})
		case strings.Contains(p, "/manifests/"):
			i := strings.Index(p, "/manifests/")
			chartName := strings.TrimPrefix(p[:i], f.path+"/")
			data, ok := f.manifests[chartName+":"+p[i+len("/manifests/"):]]
			if !ok {
				http.Error(w, `{"errors":[{"code":"MANIFEST_UNKNOWN"}]}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
			w.Header().Set("Docker-Content-Digest", digest.FromBytes(data).String())
			w.Write(data)
		case strings.Contains(p, "/blobs/"):
			f.blobHits.Add(1)
			i := strings.Index(p, "/blobs/")
			data, ok := f.blobs[p[i+len("/blobs/"):]]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Docker-Content-Digest", p[i+len("/blobs/"):])
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRegistry) host() string { return strings.TrimPrefix(f.srv.URL, "https://") }

// publish stores one chart version. layerSize/mediaType let a test lie.
func (f *fakeRegistry) publish(t *testing.T, name, version string, archive []byte, layerMediaType string, layerSize int64) {
	t.Helper()
	cfg, _ := json.Marshal(map[string]any{
		"apiVersion": "v2", "name": name, "version": version, "description": "an OCI demo chart",
		"appVersion": "7.7", "keywords": []string{"demo"}, "type": "application",
	})
	cfgDesc := ocispec.Descriptor{MediaType: "application/vnd.cncf.helm.config.v1+json", Digest: digest.FromBytes(cfg), Size: int64(len(cfg))}
	layerDesc := ocispec.Descriptor{MediaType: layerMediaType, Digest: digest.FromBytes(archive), Size: layerSize}
	f.blobs[cfgDesc.Digest.String()] = cfg
	f.blobs[layerDesc.Digest.String()] = archive
	m, _ := json.Marshal(ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest, Config: cfgDesc, Layers: []ocispec.Descriptor{layerDesc},
	})
	m = []byte(strings.Replace(string(m), `"schemaVersion":0`, `"schemaVersion":2`, 1))
	tag := strings.ReplaceAll(version, "+", "_")
	f.manifests[name+":"+tag] = m
	f.tags[name] = append(f.tags[name], tag)
}

func (f *fakeRegistry) catalog(charts ...string) *Catalog {
	return newCatalog(Config{Repos: []Repo{{Name: "reg", URL: "oci://" + f.host() + "/" + f.path, Charts: charts}}}, f.srv.Client())
}

func TestOCISearchDetailsAndLoad(t *testing.T) {
	f := newFakeRegistry(t)
	arch := buildChart(t, "demo", "1.2.3", map[string]any{"nodeSelector": map[string]any{}}, false)
	f.publish(t, "demo", "1.0.0", arch, chartLayerMediaType, int64(len(arch)))
	f.publish(t, "demo", "1.2.3", arch, chartLayerMediaType, int64(len(arch)))
	f.tags["demo"] = append(f.tags["demo"], "latest", "sha-abc123") // not versions: ignored
	c := f.catalog("demo")

	res := c.Search(context.Background(), "demo", "", "")
	if len(res.Charts) != 1 || res.Charts[0].Version != "1.2.3" || res.Charts[0].Description != "an OCI demo chart" || res.Charts[0].AppVersion != "7.7" {
		t.Fatalf("search = %+v", res)
	}
	d, err := c.Details(context.Background(), "reg", "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Versions) != 2 || d.Versions[0].Version != "1.2.3" || !d.SupportsNodeSelector {
		t.Errorf("details = %+v", d)
	}
	ch, err := c.LoadChart(context.Background(), "reg", "demo", "1.0.0")
	if err != nil || ch.Metadata.Name != "demo" {
		t.Fatalf("LoadChart = %v, %v", ch, err)
	}
	hits := f.blobHits.Load()
	if _, err := c.LoadChart(context.Background(), "reg", "demo", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if got := f.blobHits.Load(); got != hits {
		t.Errorf("a verified chart wasn't served from the cache (%d -> %d blob requests)", hits, got)
	}
	if _, err := c.LoadChart(context.Background(), "reg", "demo", "9.9.9"); err == nil {
		t.Error("an unknown version loaded")
	}
}

func TestOCIRefusesOversizeLayerBeforeDownloading(t *testing.T) {
	f := newFakeRegistry(t)
	arch := buildChart(t, "demo", "1.0.0", map[string]any{}, false)
	f.publish(t, "demo", "1.0.0", arch, chartLayerMediaType, maxChartBytes+1)
	c := f.catalog("demo")
	// The manifest alone is enough to refuse: no layer bytes may be requested.
	before := f.blobHits.Load()
	_, err := c.fetchChartForTest(t, "demo", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("an oversize chart was accepted: %v", err)
	}
	if f.blobHits.Load() != before {
		t.Error("the layer was downloaded although its declared size is over the limit")
	}
}

func (c *Catalog) fetchChartForTest(t *testing.T, name, version string) ([]byte, error) {
	t.Helper()
	r, _ := c.cfg.Repo("reg")
	data, _, err := c.fetchOCIChart(context.Background(), r, name, version)
	return data, err
}

func TestOCIRefusesNonChartAndTamperedContent(t *testing.T) {
	f := newFakeRegistry(t)
	arch := buildChart(t, "demo", "1.0.0", map[string]any{}, false)
	f.publish(t, "image", "1.0.0", arch, "application/vnd.oci.image.layer.v1.tar+gzip", int64(len(arch)))
	f.publish(t, "demo", "1.0.0", arch, chartLayerMediaType, int64(len(arch)))
	// replace the stored layer with different bytes of the same declared size
	sum := digest.FromBytes(arch).String()
	f.blobs[sum] = append([]byte("X"), arch[1:]...)
	c := f.catalog("demo", "image")

	if _, err := c.fetchChartForTest(t, "image", "1.0.0"); err == nil || !strings.Contains(err.Error(), "not a Helm chart") {
		t.Errorf("a non-chart artifact was accepted: %v", err)
	}
	if _, err := c.fetchChartForTest(t, "demo", "1.0.0"); err == nil {
		t.Error("content that doesn't match the manifest's digest was accepted")
	}
	_ = sha256.Size
}

func TestOCIAnonymousOnly(t *testing.T) {
	// No request may carry credentials: the client is anonymous by construction.
	var sawAuth atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth.Store(true)
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	t.Setenv("DOCKER_CONFIG", t.TempDir()) // even with a docker config around
	c := newCatalog(Config{Repos: []Repo{{Name: "reg", URL: "oci://" + strings.TrimPrefix(srv.URL, "https://") + "/charts", Charts: []string{"demo"}}}}, srv.Client())
	c.Search(context.Background(), "", "", "")
	if sawAuth.Load() {
		t.Error("a request carried an Authorization header")
	}
}

func TestOCIConfigValidation(t *testing.T) {
	ok := Repo{Name: "a", URL: "oci://registry-1.docker.io/bitnamicharts", Charts: []string{"postgresql", "kafka"}}
	if err := (Config{Repos: []Repo{ok}}).Validate(); err != nil {
		t.Fatalf("a valid oci source was rejected: %v", err)
	}
	bad := map[string]Repo{
		"no charts":       {Name: "a", URL: "oci://ghcr.io/x/y"},
		"uppercase":       {Name: "a", URL: "oci://ghcr.io/X/y", Charts: []string{"c"}},
		"credentials":     {Name: "a", URL: "oci://user:pw@ghcr.io/x", Charts: []string{"c"}},
		"query":           {Name: "a", URL: "oci://ghcr.io/x?plain=1", Charts: []string{"c"}},
		"no path":         {Name: "a", URL: "oci://ghcr.io", Charts: []string{"c"}},
		"scheme in path":  {Name: "a", URL: "oci://ghcr.io/http://evil", Charts: []string{"c"}},
		"bad chart":       {Name: "a", URL: "oci://ghcr.io/x", Charts: []string{"../etc"}},
		"dup chart":       {Name: "a", URL: "oci://ghcr.io/x", Charts: []string{"c", "c"}},
		"charts on https": {Name: "a", URL: "https://example.com", Charts: []string{"c"}},
		"bad category":    {Name: "a", URL: "https://example.com", Category: "<script>"},
		"bad tag":         {Name: "a", URL: "https://example.com", ChartTags: map[string][]string{"c": {"Has Space"}}},
		"bad tag key":     {Name: "a", URL: "https://example.com", ChartTags: map[string][]string{"../x": {"ok"}}},
	}
	for name, r := range bad {
		if err := (Config{Repos: []Repo{r}}).Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSearchByCategoryAndChartTags(t *testing.T) {
	f := newFakeRepo(t, map[string]any{"nodeSelector": map[string]any{}}, nil)
	c := newCatalog(Config{Repos: []Repo{{
		Name: "fake", URL: f.srv.URL, Category: "Databases",
		ChartTags: map[string][]string{"other": {"postgres"}},
	}}}, f.srv.Client())
	// "other" doesn't mention postgres itself; its configured tag does — and the
	// repository's other chart, "demo", is not dragged in by it.
	res := c.Search(context.Background(), "postgres", "", "")
	if len(res.Charts) != 1 || res.Charts[0].Name != "other" {
		t.Errorf("per-chart tags: %+v", res)
	}
	if res := c.Search(context.Background(), "", "", "databases"); len(res.Charts) != 2 || res.Charts[0].Category != "Databases" {
		t.Errorf("category filter = %+v", res)
	}
	if res := c.Search(context.Background(), "", "", "Messaging"); len(res.Charts) != 0 {
		t.Errorf("another category matched: %+v", res)
	}
}

func TestSlowRepositoryIsReportedPendingNotAwaited(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		fmt.Fprint(w, "apiVersion: v1\nentries:\n  slow:\n    - name: slow\n      version: 1.0.0\n")
	}))
	defer srv.Close()
	defer close(release)
	c := newCatalog(Config{Repos: []Repo{{Name: "slow", URL: srv.URL}}}, srv.Client())
	c.wait = 100 * time.Millisecond
	start := time.Now()
	res := c.Search(context.Background(), "", "", "")
	if time.Since(start) > 3*time.Second {
		t.Fatal("search waited for the slow repository")
	}
	if len(res.Pending) != 1 || res.Pending[0] != "slow" {
		t.Errorf("pending = %v", res.Pending)
	}
}

func TestStaleIndexIsKeptWhenRefreshFails(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "apiVersion: v1\nentries:\n  demo:\n    - name: demo\n      version: 1.0.0\n")
	}))
	defer srv.Close()
	c := newCatalog(Config{Repos: []Repo{{Name: "r", URL: srv.URL}}}, srv.Client())
	now := time.Now()
	c.now = func() time.Time { return now }
	if res := c.Search(context.Background(), "", "", ""); len(res.Charts) != 1 {
		t.Fatalf("first search = %+v", res)
	}
	fail.Store(true)
	now = now.Add(indexTTL + time.Second)
	res := c.Search(context.Background(), "", "", "")
	if len(res.Charts) != 1 || res.Errors != nil {
		t.Errorf("a failed refresh blanked the repository: %+v", res)
	}
}
