package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/repo"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// OCI sources (Roadmap Phase 28 follow-up). Charts published to a container
// registry have no index.yaml, so each OCI source names the charts it offers.
//
// This deliberately does NOT use Helm's own registry client: that one falls back
// to the Docker credentials found on the machine Vinaire runs on, and would
// authenticate to a registry as whoever set the host up. Here every request is
// anonymous (public charts only) and goes through the same address-guarding HTTP
// client as classic repositories.
//
// Integrity: a registry is content-addressed — the manifest names the chart
// layer by sha256 and oras-go verifies the bytes it downloads against that
// digest and size. Which chart a *tag* points at is the registry's say, exactly
// as which file an index points at is the repository's.

const (
	chartLayerMediaType       = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	legacyChartLayerMediaType = "application/tar+gzip"
	maxConfigBytes            = 1 << 20
	maxVersionsPerChart       = 100
)

// ociReference is "host/path/chart" for a configured chart.
func ociReference(r Repo, chartName string) string {
	return strings.TrimPrefix(r.URL, ociScheme) + "/" + chartName
}

func (c *Catalog) ociRepository(r Repo, chartName string) (*remote.Repository, error) {
	repository, err := remote.NewRepository(ociReference(r, chartName))
	if err != nil {
		return nil, fmt.Errorf("oci reference for %s/%s: %w", r.Name, chartName, err)
	}
	repository.PlainHTTP = false // never: always TLS
	repository.Client = &auth.Client{Client: c.client, Cache: auth.NewCache()}
	return repository, nil
}

// tagToVersion / versionToTag: an OCI tag can't contain "+", so Helm writes a
// chart version's build metadata with "_".
func tagToVersion(tag string) string { return strings.ReplaceAll(tag, "_", "+") }
func versionToTag(v string) string   { return strings.ReplaceAll(v, "+", "_") }

// ociManifest fetches and parses the manifest of one tag and picks out the chart
// layer, refusing anything that isn't a single Helm chart or is too large.
func ociManifest(ctx context.Context, repository *remote.Repository, tag string) (ocispec.Manifest, ocispec.Descriptor, error) {
	desc, rc, err := repository.FetchReference(ctx, tag)
	if err != nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("fetching %s:%s: %w", repository.Reference.Repository, tag, err)
	}
	defer rc.Close()
	data, err := content.ReadAll(rc, desc)
	if err != nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("reading the manifest of %s:%s: %w", repository.Reference.Repository, tag, err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("%s:%s has an invalid manifest: %w", repository.Reference.Repository, tag, err)
	}
	var layer *ocispec.Descriptor
	for i := range m.Layers {
		if m.Layers[i].MediaType == chartLayerMediaType || m.Layers[i].MediaType == legacyChartLayerMediaType {
			if layer != nil {
				return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("%s:%s has more than one chart layer", repository.Reference.Repository, tag)
			}
			layer = &m.Layers[i]
		}
	}
	if layer == nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("%s:%s is not a Helm chart", repository.Reference.Repository, tag)
	}
	if layer.Size <= 0 || layer.Size > maxChartBytes {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("the chart in %s:%s is %d bytes; the limit is %d MiB", repository.Reference.Repository, tag, layer.Size, maxChartBytes>>20)
	}
	m.Layers = []ocispec.Descriptor{*layer}
	return m, desc, nil
}

// ociMetadata reads the chart's Chart.yaml (stored as the manifest's config blob).
func ociMetadata(ctx context.Context, repository *remote.Repository, m ocispec.Manifest) (*chart.Metadata, error) {
	if m.Config.Size <= 0 || m.Config.Size > maxConfigBytes {
		return nil, fmt.Errorf("the chart's config blob has an unreasonable size (%d bytes)", m.Config.Size)
	}
	data, err := content.FetchAll(ctx, repository, m.Config)
	if err != nil {
		return nil, fmt.Errorf("reading the chart's metadata: %w", err)
	}
	var md chart.Metadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, fmt.Errorf("the chart's metadata is invalid: %w", err)
	}
	return &md, nil
}

// loadOCIIndex builds an index for an OCI source: for each configured chart, its
// versions (from the registry's tag list) and — for the newest — the metadata a
// catalog card needs. Charts that can't be read are skipped; the source fails
// only if none can.
func (c *Catalog) loadOCIIndex(ctx context.Context, r Repo) (*repo.IndexFile, error) {
	idx := repo.NewIndexFile()
	idx.Generated = time.Now()
	var firstErr error
	for _, name := range r.Charts {
		versions, err := c.ociChartVersions(ctx, r, name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		idx.Entries[name] = versions
	}
	if len(idx.Entries) == 0 {
		return nil, fmt.Errorf("%s: no chart could be read: %w", r.Name, firstErr)
	}
	return idx, nil
}

func (c *Catalog) ociChartVersions(ctx context.Context, r Repo, name string) (repo.ChartVersions, error) {
	repository, err := c.ociRepository(r, name)
	if err != nil {
		return nil, err
	}
	type tagged struct {
		tag string
		v   *semver.Version
	}
	var tags []tagged
	err = repository.Tags(ctx, "", func(page []string) error {
		for _, t := range page {
			if v, err := semver.NewVersion(tagToVersion(t)); err == nil {
				tags = append(tags, tagged{t, v})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing the versions of %s/%s: %w", r.Name, name, err)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("%s/%s has no versioned tags", r.Name, name)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].v.GreaterThan(tags[j].v) })
	if len(tags) > maxVersionsPerChart {
		tags = tags[:maxVersionsPerChart]
	}

	out := make(repo.ChartVersions, 0, len(tags))
	for _, t := range tags {
		out = append(out, &repo.ChartVersion{Metadata: &chart.Metadata{Name: name, Version: t.v.Original()}})
	}
	// Metadata for the card comes from the newest version only — one manifest
	// and one small config blob, not a chart download.
	newest := out[0]
	m, _, err := ociManifest(ctx, repository, versionToTag(newest.Version))
	if err != nil {
		return nil, err
	}
	md, err := ociMetadata(ctx, repository, m)
	if err != nil {
		return nil, err
	}
	md.Name = name
	md.Version = newest.Version
	newest.Metadata = md
	return out, nil
}

// fetchOCIChart downloads one chart version's archive. The size is checked from
// the manifest before any byte of the layer is requested, and oras-go verifies
// the content against the layer's digest as it reads.
func (c *Catalog) fetchOCIChart(ctx context.Context, r Repo, name, version string) ([]byte, string, error) {
	repository, err := c.ociRepository(r, name)
	if err != nil {
		return nil, "", err
	}
	m, _, err := ociManifest(ctx, repository, versionToTag(version))
	if err != nil {
		return nil, "", err
	}
	layer := m.Layers[0]
	key := ociReference(r, name) + "@" + layer.Digest.String()

	c.mu.Lock()
	data, ok := c.charts[key]
	c.mu.Unlock()
	if ok {
		return data, key, nil
	}
	data, err = content.FetchAll(ctx, repository, layer)
	if err != nil {
		return nil, "", fmt.Errorf("downloading %s %s: %w", name, version, err)
	}
	return data, key, nil
}
