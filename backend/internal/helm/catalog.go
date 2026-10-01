package helm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

const (
	maxIndexBytes = 48 << 20 // a repository index (bitnami's is ~20 MiB)
	maxChartBytes = 32 << 20 // a packaged chart
	maxReadme     = 96 << 10 // README shown in the UI
	indexTTL      = 15 * time.Minute
	indexErrTTL   = time.Minute // a failing repository isn't retried on every keystroke
	fetchTimeout  = 30 * time.Second
	maxChartCache = 8
	maxSearchRows = 200
)

// Catalog browses the configured repositories. It is read-only and holds no
// Kubernetes credentials; installing is done by Releases with the caller's own.
type Catalog struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	indexes map[string]*indexEntry
	// verified archives by "<url>@<sha256>" — immutable, so safe to keep. Bytes,
	// not *chart.Chart: Helm mutates a chart while installing it, so every use
	// loads its own copy.
	charts map[string][]byte
	order  []string // insertion order, for eviction
}

type indexEntry struct {
	mu      sync.Mutex
	idx     *repo.IndexFile
	err     error
	fetched time.Time
}

// NewCatalog builds a catalog whose HTTP client refuses to connect to loopback,
// link-local (cloud metadata) and other non-routable addresses and to follow a
// redirect out of https. Repository URLs come only from configuration, but an
// index can name any chart URL, so the same guard covers both.
func NewCatalog(cfg Config) *Catalog {
	return newCatalog(cfg, safeClient(false))
}

func newCatalog(cfg Config, client *http.Client) *Catalog {
	return &Catalog{
		cfg:     cfg,
		client:  client,
		now:     time.Now,
		indexes: map[string]*indexEntry{},
		charts:  map[string][]byte{},
	}
}

// Repos is the configured repository list.
func (c *Catalog) Repos() []Repo { return c.cfg.Repos }

// forbiddenAddr reports addresses a catalog fetch must never reach.
func forbiddenAddr(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast()
}

// safeClient is the HTTP client for repositories. The check runs in the dialer's
// Control hook, on the address actually being connected to, so a hostname that
// resolves somewhere forbidden (or changes between lookups) can't get past it.
// allowLoopback exists for tests against httptest servers only.
func safeClient(allowLoopback bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("refusing to connect to %q", host)
			}
			if forbiddenAddr(ip) && !(allowLoopback && ip.IsLoopback()) {
				return fmt.Errorf("refusing to connect to %s", ip)
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		DisableCompression:    false,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && !allowLoopback {
				return errors.New("refusing to follow a redirect away from https")
			}
			return nil
		},
	}
}

// fetchLimited GETs rawURL and returns at most max bytes of its body; a larger
// body is an error rather than a silent truncation.
func (c *Catalog) fetchLimited(ctx context.Context, rawURL string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vinaire-catalog")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", redactURL(rawURL), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", redactURL(rawURL), resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", redactURL(rawURL), err)
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("%s is larger than %d MiB", redactURL(rawURL), max>>20)
	}
	return body, nil
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "the repository"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// index returns the (cached) index of one repository.
func (c *Catalog) index(ctx context.Context, r Repo) (*repo.IndexFile, error) {
	c.mu.Lock()
	e := c.indexes[r.Name]
	if e == nil {
		e = &indexEntry{}
		c.indexes[r.Name] = e
	}
	c.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	age := c.now().Sub(e.fetched)
	if !e.fetched.IsZero() && ((e.err == nil && age < indexTTL) || (e.err != nil && age < indexErrTTL)) {
		return e.idx, e.err
	}
	idx, err := c.loadIndex(ctx, r)
	e.idx, e.err, e.fetched = idx, err, c.now()
	return idx, err
}

func (c *Catalog) loadIndex(ctx context.Context, r Repo) (*repo.IndexFile, error) {
	data, err := c.fetchLimited(ctx, strings.TrimRight(r.URL, "/")+"/index.yaml", maxIndexBytes)
	if err != nil {
		return nil, err
	}
	var idx repo.IndexFile
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("%s: not a valid Helm repository index: %w", r.Name, err)
	}
	if idx.APIVersion == "" {
		return nil, fmt.Errorf("%s: not a valid Helm repository index (no apiVersion)", r.Name)
	}
	idx.SortEntries()
	return &idx, nil
}

// ChartSummary is one chart (its newest version) as a catalog card.
type ChartSummary struct {
	Repo        string   `json:"repo"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	AppVersion  string   `json:"appVersion,omitempty"`
	Description string   `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Deprecated  bool     `json:"deprecated,omitempty"`
}

func summarize(repoName, name string, cv *repo.ChartVersion) ChartSummary {
	return ChartSummary{
		Repo: repoName, Name: name, Version: cv.Version, AppVersion: cv.AppVersion,
		Description: cv.Description, Keywords: cv.Keywords, Deprecated: cv.Deprecated,
	}
}

// SearchResult is the catalog search response: matches, plus the repositories
// that could not be reached (so one broken repository doesn't blank the page).
type SearchResult struct {
	Charts []ChartSummary    `json:"charts"`
	Errors map[string]string `json:"errors,omitempty"`
}

// Search lists charts across the configured repositories (or just repoName)
// whose name, description or keywords contain query (case-insensitive).
func (c *Catalog) Search(ctx context.Context, query, repoName string) SearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	res := SearchResult{Charts: []ChartSummary{}}

	type got struct {
		r   Repo
		idx *repo.IndexFile
		err error
	}
	var repos []Repo
	for _, r := range c.cfg.Repos {
		if repoName == "" || r.Name == repoName {
			repos = append(repos, r)
		}
	}
	out := make([]got, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx, err := c.index(ctx, r)
			out[i] = got{r, idx, err}
		}()
	}
	wg.Wait()

	for _, g := range out {
		if g.err != nil {
			if res.Errors == nil {
				res.Errors = map[string]string{}
			}
			res.Errors[g.r.Name] = g.err.Error()
			continue
		}
		for name, versions := range g.idx.Entries {
			if len(versions) == 0 {
				continue
			}
			cv := versions[0]
			if query != "" && !matches(query, name, cv) {
				continue
			}
			res.Charts = append(res.Charts, summarize(g.r.Name, name, cv))
		}
	}
	sort.Slice(res.Charts, func(i, j int) bool {
		a, b := res.Charts[i], res.Charts[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Repo < b.Repo
	})
	if len(res.Charts) > maxSearchRows {
		res.Charts = res.Charts[:maxSearchRows]
	}
	return res
}

func matches(query, name string, cv *repo.ChartVersion) bool {
	if strings.Contains(strings.ToLower(name), query) || strings.Contains(strings.ToLower(cv.Description), query) {
		return true
	}
	for _, k := range cv.Keywords {
		if strings.Contains(strings.ToLower(k), query) {
			return true
		}
	}
	return false
}

// VersionInfo is one published version of a chart.
type VersionInfo struct {
	Version    string    `json:"version"`
	AppVersion string    `json:"appVersion,omitempty"`
	Created    time.Time `json:"created,omitempty"`
	Deprecated bool      `json:"deprecated,omitempty"`
}

// ChartDetails is what the install page needs about a chart version.
type ChartDetails struct {
	ChartSummary
	Home     string        `json:"home,omitempty"`
	Versions []VersionInfo `json:"versions"`
	Readme   string        `json:"readme,omitempty"`
	// Values is the chart's default values.yaml; HasSchema says it also ships a
	// values.schema.json (Helm validates the values against it on install).
	Values    string `json:"values"`
	HasSchema bool   `json:"hasSchema"`
	// SupportsNodeSelector: the chart has a top-level `nodeSelector` value, the
	// convention the install page's "run on node" picker relies on.
	SupportsNodeSelector bool `json:"supportsNodeSelector"`
}

// ErrNotFound is returned for an unknown repository, chart or version.
var ErrNotFound = errors.New("not found")

func (c *Catalog) lookup(ctx context.Context, repoName, name, version string) (Repo, *repo.ChartVersion, []*repo.ChartVersion, error) {
	r, ok := c.cfg.Repo(repoName)
	if !ok {
		return Repo{}, nil, nil, fmt.Errorf("repository %q: %w", repoName, ErrNotFound)
	}
	idx, err := c.index(ctx, r)
	if err != nil {
		return Repo{}, nil, nil, err
	}
	versions := idx.Entries[name]
	if len(versions) == 0 {
		return Repo{}, nil, nil, fmt.Errorf("chart %s/%s: %w", repoName, name, ErrNotFound)
	}
	if version == "" {
		return r, versions[0], versions, nil
	}
	for _, v := range versions {
		if v.Version == version {
			return r, v, versions, nil
		}
	}
	return Repo{}, nil, nil, fmt.Errorf("chart %s/%s version %s: %w", repoName, name, version, ErrNotFound)
}

// Details returns the catalog page of one chart version ("" = the newest).
func (c *Catalog) Details(ctx context.Context, repoName, name, version string) (*ChartDetails, error) {
	r, cv, versions, err := c.lookup(ctx, repoName, name, version)
	if err != nil {
		return nil, err
	}
	ch, err := c.loadChart(ctx, r, cv)
	if err != nil {
		return nil, err
	}
	d := &ChartDetails{ChartSummary: summarize(repoName, name, cv), Home: cv.Home}
	for _, v := range versions {
		d.Versions = append(d.Versions, VersionInfo{Version: v.Version, AppVersion: v.AppVersion, Created: v.Created, Deprecated: v.Deprecated})
	}
	for _, f := range ch.Raw {
		if f.Name == "values.yaml" {
			d.Values = string(f.Data)
		}
	}
	for _, f := range ch.Files {
		if strings.EqualFold(f.Name, "README.md") {
			d.Readme = truncate(string(f.Data), maxReadme)
		}
	}
	d.HasSchema = len(ch.Schema) > 0
	_, d.SupportsNodeSelector = ch.Values["nodeSelector"]
	return d, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n\n… (truncated)"
}

// LoadChart downloads, verifies and loads one chart version ("" = the newest).
func (c *Catalog) LoadChart(ctx context.Context, repoName, name, version string) (*chart.Chart, error) {
	r, cv, _, err := c.lookup(ctx, repoName, name, version)
	if err != nil {
		return nil, err
	}
	return c.loadChart(ctx, r, cv)
}

func (c *Catalog) loadChart(ctx context.Context, r Repo, cv *repo.ChartVersion) (*chart.Chart, error) {
	if len(cv.URLs) == 0 {
		return nil, fmt.Errorf("chart %s %s lists no download URL", cv.Name, cv.Version)
	}
	// Without a digest there is nothing to check the download against, and an
	// index is just a file on someone's web server — so no digest, no install.
	want := strings.ToLower(strings.TrimPrefix(cv.Digest, "sha256:"))
	if len(want) != sha256.Size*2 {
		return nil, fmt.Errorf("chart %s %s has no sha256 digest in the repository index, so its download can't be verified", cv.Name, cv.Version)
	}
	base, err := url.Parse(strings.TrimRight(r.URL, "/") + "/")
	if err != nil {
		return nil, err
	}
	ref, err := url.Parse(cv.URLs[0])
	if err != nil {
		return nil, fmt.Errorf("chart %s %s has an invalid download URL", cv.Name, cv.Version)
	}
	chartURL := base.ResolveReference(ref)
	if chartURL.Scheme != "https" {
		return nil, fmt.Errorf("chart %s %s is not served over https", cv.Name, cv.Version)
	}

	key := chartURL.String() + "@" + want
	c.mu.Lock()
	data, cached := c.charts[key]
	c.mu.Unlock()
	if !cached {
		var err error
		data, err = c.fetchLimited(ctx, chartURL.String(), maxChartBytes)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			return nil, fmt.Errorf("chart %s %s doesn't match the digest in the repository index — refusing to use it", cv.Name, cv.Version)
		}
	}
	ch, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("loading chart %s %s: %w", cv.Name, cv.Version, err)
	}
	if err := ch.Validate(); err != nil {
		return nil, fmt.Errorf("chart %s %s is invalid: %w", cv.Name, cv.Version, err)
	}
	if ch.Metadata.Type == "library" {
		return nil, fmt.Errorf("chart %s is a library chart and can't be installed", cv.Name)
	}

	if !cached {
		c.mu.Lock()
		if len(c.order) >= maxChartCache {
			delete(c.charts, c.order[0])
			c.order = c.order[1:]
		}
		c.charts[key] = data
		c.order = append(c.order, key)
		c.mu.Unlock()
	}
	return ch, nil
}
