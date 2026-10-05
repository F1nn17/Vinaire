package helm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shiraku/vinaire/backend/internal/netguard"
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
	maxSearchRows = 600
	// A search waits this long for slow repositories, then answers with what it
	// has and lists the rest as still loading — they keep loading in the background.
	searchWait = 12 * time.Second
	// One whole index load (an OCI source makes several requests).
	loadTimeout = 2 * time.Minute
	// At most this many indexes are fetched and parsed at once: some are several
	// MiB of YAML, and starting forty together would spike memory.
	maxParallelLoads = 4
)

// Catalog browses the configured repositories. It is read-only and holds no
// Kubernetes credentials; installing is done by Releases with the caller's own.
type Catalog struct {
	cfg    Config
	client *http.Client
	now    func() time.Time
	wait   time.Duration // how long Search waits for slow repositories

	sem     chan struct{} // bounds concurrent index loads
	mu      sync.Mutex
	indexes map[string]*indexEntry
	// verified archives by "<url>@<sha256>" — immutable, so safe to keep. Bytes,
	// not *chart.Chart: Helm mutates a chart while installing it, so every use
	// loads its own copy.
	charts map[string][]byte
	order  []string // insertion order, for eviction
	icons  iconCache
}

type indexEntry struct {
	mu      sync.Mutex
	idx     *repo.IndexFile
	err     error
	fetched time.Time
	loading chan struct{} // non-nil, and closed when done, while a load runs
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
		wait:    searchWait,
		sem:     make(chan struct{}, maxParallelLoads),
		indexes: map[string]*indexEntry{},
		charts:  map[string][]byte{},
	}
}

// Repos is the configured repository list.
func (c *Catalog) Repos() []Repo { return c.cfg.Repos }

// safeClient is the HTTP client for repositories: the shared netguard client
// (no loopback, link-local or multicast addresses; https-only redirects).
// allowLoopback exists for tests against httptest servers only.
func safeClient(allowLoopback bool) *http.Client {
	return netguard.Client(netguard.Options{AllowLoopback: allowLoopback, Timeout: fetchTimeout})
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
	reportProgress(ctx, 0, resp.ContentLength)
	body, err := io.ReadAll(&countingReader{ctx: ctx, r: io.LimitReader(resp.Body, max+1), total: resp.ContentLength})
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

// index returns the (cached) index of one repository. A load runs detached from
// the request that triggered it — so it isn't cancelled when that request gives
// up, and concurrent requests share it — and the caller just waits for it.
func (c *Catalog) index(ctx context.Context, r Repo) (*repo.IndexFile, error) {
	c.mu.Lock()
	e := c.indexes[r.Name]
	if e == nil {
		e = &indexEntry{}
		c.indexes[r.Name] = e
	}
	c.mu.Unlock()

	e.mu.Lock()
	age := c.now().Sub(e.fetched)
	if !e.fetched.IsZero() && ((e.err == nil && age < indexTTL) || (e.err != nil && age < indexErrTTL)) {
		idx, err := e.idx, e.err
		e.mu.Unlock()
		return idx, err
	}
	if e.loading == nil {
		e.loading = make(chan struct{})
		go c.refresh(r, e) // #nosec G118 -- the load is shared between requests, so it must outlive the one that started it
	}
	done := e.loading
	e.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.idx, e.err
}

func (c *Catalog) refresh(r Repo, e *indexEntry) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	idx, err := c.loadIndex(ctx, r)

	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil && e.idx != nil {
		// keep serving the previous index rather than blank the repository, and
		// try again soon
		e.err = nil
		e.fetched = c.now().Add(indexErrTTL - indexTTL)
	} else {
		e.idx, e.err, e.fetched = idx, err, c.now()
	}
	close(e.loading)
	e.loading = nil
}

// Warm starts loading every repository's index in the background, so the first
// search after startup isn't the one that pays for it.
func (c *Catalog) Warm() {
	for _, r := range c.cfg.Repos {
		go func() { _, _ = c.index(context.Background(), r) }()
	}
}

func (c *Catalog) loadIndex(ctx context.Context, r Repo) (*repo.IndexFile, error) {
	if r.IsOCI() {
		return c.loadOCIIndex(ctx, r)
	}
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
	// Keep memory bounded: the oldest of hundreds of versions are never wanted.
	for name, versions := range idx.Entries {
		if len(versions) > maxVersionsPerChart {
			idx.Entries[name] = versions[:maxVersionsPerChart]
		}
	}
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
	Category    string   `json:"category,omitempty"`
	// HasIcon: the chart names an icon, served by the backend at
	// /api/v1/catalog/icons/{repo}/{chart} (never fetched by the browser).
	HasIcon bool `json:"hasIcon,omitempty"`
}

func summarize(r Repo, name string, cv *repo.ChartVersion) ChartSummary {
	return ChartSummary{
		Repo: r.Name, Name: name, Version: cv.Version, AppVersion: cv.AppVersion,
		Description: cv.Description, Keywords: cv.Keywords, Deprecated: cv.Deprecated,
		Category: r.Category, HasIcon: iconURL(r, cv.Icon) != "",
	}
}

// SearchResult is the catalog search response: matches, the repositories that
// could not be reached (so one broken repository doesn't blank the page) and the
// ones still loading (a first search can outrun a slow repository).
type SearchResult struct {
	Charts  []ChartSummary    `json:"charts"`
	Total   int               `json:"total"` // matches, before the list is cut at maxSearchRows
	Errors  map[string]string `json:"errors,omitempty"`
	Pending []string          `json:"pending,omitempty"`
	// HiddenDeprecated counts matches left out because the chart is deprecated
	// (unmaintained, and possibly unsafe); ask again with includeDeprecated to see them.
	HiddenDeprecated int `json:"hiddenDeprecated,omitempty"`
}

// Search lists charts across the configured repositories whose name,
// description, keywords or repository tags contain query (case-insensitive),
// optionally limited to one repository and/or category.
// Deprecated charts are left out.
func (c *Catalog) Search(ctx context.Context, query, repoName, category string) SearchResult {
	return c.SearchWith(ctx, query, repoName, category, false)
}

// SearchWith is Search with a choice about deprecated charts, which are
// unmaintained and so hidden unless includeDeprecated is set.
func (c *Catalog) SearchWith(ctx context.Context, query, repoName, category string, includeDeprecated bool) SearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	res := SearchResult{Charts: []ChartSummary{}}

	type got struct {
		r   Repo
		idx *repo.IndexFile
		err error
	}
	var repos []Repo
	for _, r := range c.cfg.Repos {
		if (repoName == "" || r.Name == repoName) && (category == "" || strings.EqualFold(r.Category, category)) {
			repos = append(repos, r)
		}
	}
	results := make(chan got, len(repos)) // buffered: a late finisher never blocks
	for _, r := range repos {
		go func() {
			idx, err := c.index(ctx, r)
			results <- got{r, idx, err}
		}()
	}

	timer := time.NewTimer(c.wait)
	defer timer.Stop()
	finished := map[string]bool{}
	scores := map[string]int{}
collect:
	for range repos {
		select {
		case g := <-results:
			finished[g.r.Name] = true
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
				sc := 0
				if query != "" {
					if sc = score(query, name, cv, g.r); sc == 0 {
						continue
					}
				}
				if cv.Deprecated && !includeDeprecated {
					res.HiddenDeprecated++
					continue
				}
				res.Charts = append(res.Charts, summarize(g.r, name, cv))
				scores[g.r.Name+"/"+name] = sc
			}
		case <-timer.C:
			break collect
		case <-ctx.Done():
			break collect
		}
	}
	for _, r := range repos {
		if !finished[r.Name] {
			res.Pending = append(res.Pending, r.Name)
		}
	}

	// Most relevant first; with no query (or equal scores) alphabetical.
	sort.Slice(res.Charts, func(i, j int) bool {
		a, b := res.Charts[i], res.Charts[j]
		if sa, sb := scores[a.Repo+"/"+a.Name], scores[b.Repo+"/"+b.Name]; sa != sb {
			return sa > sb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Repo < b.Repo
	})
	res.Total = len(res.Charts)
	if len(res.Charts) > maxSearchRows {
		res.Charts = res.Charts[:maxSearchRows]
	}
	return res
}

// score ranks how well a chart answers a query; 0 means it doesn't. The chart's
// own name counts most, then its keywords and the per-chart tags from the
// configuration, then its description, and the repository's category last.
func score(query, name string, cv *repo.ChartVersion, r Repo) int {
	n := strings.ToLower(name)
	sc := 0
	switch {
	case n == query:
		sc += 100
	case strings.HasPrefix(n, query):
		sc += 70
	case strings.Contains(n, query):
		sc += 50
	}
	for _, k := range cv.Keywords {
		if strings.Contains(strings.ToLower(k), query) {
			sc += 25
			break
		}
	}
	for _, t := range r.ChartTags[name] {
		if strings.Contains(t, query) {
			sc += 25
			break
		}
	}
	if strings.Contains(strings.ToLower(cv.Description), query) {
		sc += 10
	}
	if strings.Contains(strings.ToLower(r.Category), query) {
		sc += 3
	}
	return sc
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
	d := &ChartDetails{ChartSummary: summarize(r, name, cv), Home: cv.Home}
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
	data, key, err := c.chartArchive(ctx, r, cv)
	if err != nil {
		return nil, err
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
	c.remember(key, data)
	return ch, nil
}

// remember keeps a verified archive for a while (bytes, not a *chart.Chart: Helm
// mutates a chart while installing it, so every use loads its own copy).
func (c *Catalog) remember(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.charts[key]; ok {
		return
	}
	if len(c.order) >= maxChartCache {
		delete(c.charts, c.order[0])
		c.order = c.order[1:]
	}
	c.charts[key] = data
	c.order = append(c.order, key)
}

// chartArchive returns a chart version's archive, verified, and the cache key.
func (c *Catalog) chartArchive(ctx context.Context, r Repo, cv *repo.ChartVersion) ([]byte, string, error) {
	if r.IsOCI() {
		return c.fetchOCIChart(ctx, r, cv.Name, cv.Version)
	}
	if len(cv.URLs) == 0 {
		return nil, "", fmt.Errorf("chart %s %s lists no download URL", cv.Name, cv.Version)
	}
	// Without a digest there is nothing to check the download against, and an
	// index is just a file on someone's web server — so no digest, no install.
	want := strings.ToLower(strings.TrimPrefix(cv.Digest, "sha256:"))
	if len(want) != sha256.Size*2 {
		return nil, "", fmt.Errorf("chart %s %s has no sha256 digest in the repository index, so its download can't be verified", cv.Name, cv.Version)
	}
	base, err := url.Parse(strings.TrimRight(r.URL, "/") + "/")
	if err != nil {
		return nil, "", err
	}
	ref, err := url.Parse(cv.URLs[0])
	if err != nil {
		return nil, "", fmt.Errorf("chart %s %s has an invalid download URL", cv.Name, cv.Version)
	}
	chartURL := base.ResolveReference(ref)
	if chartURL.Scheme != "https" {
		return nil, "", fmt.Errorf("chart %s %s is not served over https", cv.Name, cv.Version)
	}

	key := chartURL.String() + "@" + want
	c.mu.Lock()
	data, cached := c.charts[key]
	c.mu.Unlock()
	if cached {
		return data, key, nil
	}
	data, err = c.fetchLimited(ctx, chartURL.String(), maxChartBytes)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return nil, "", fmt.Errorf("chart %s %s doesn't match the digest in the repository index — refusing to use it", cv.Name, cv.Version)
	}
	return data, key, nil
}
