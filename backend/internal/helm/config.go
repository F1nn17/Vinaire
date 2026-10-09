// Package helm is Vinaire's application catalog (Roadmap Phase 28): browse
// charts from a fixed list of repositories and install, upgrade, roll back and
// uninstall releases with the Helm SDK — as the caller's own Kubernetes
// identity, never Vinaire's.
package helm

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Which repositories the catalog may talk to lives in configuration, not in
// code and not in the UI: catalog.json (embedded) is the default and
// CATALOG_CONFIG points at a replacement file. A user of the web UI can't add
// a repository, so they can't make Vinaire fetch an arbitrary URL or install a
// chart from a source nobody vetted.

//go:embed catalog.json
var defaultCatalogJSON []byte

// Repo is one configured source of charts: either a classic Helm repository
// (an index.yaml served over https) or an OCI registry (URL "oci://host/path").
// A registry has no index, so for an OCI source the charts to offer are listed
// explicitly in Charts — the catalog never enumerates a registry itself.
type Repo struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	// Category groups repositories in the UI ("Databases", "Messaging"...).
	// ChartTags are extra words a search matches for one chart, by chart name —
	// so that a search for "postgres" finds CloudNativePG's chart called
	// "cluster". They're per chart, not per repository, so they never make an
	// unrelated chart match.
	// Unmaintained marks a repository whose charts still work but whose images are no longer
	// updated (Bitnami's, since it moved its free images to an archive): it is left out of a
	// search unless asked for, and the install page says so.
	Unmaintained bool `json:"unmaintained,omitempty"`
	// Roles overrides the guessed kind of a chart by name ("operator" or "cluster"; see Role).
	Roles     map[string]string   `json:"roles,omitempty"`
	Category  string              `json:"category,omitempty"`
	ChartTags map[string][]string `json:"chartTags,omitempty"`
	Charts    []string            `json:"charts,omitempty"` // OCI sources only
}

// IsOCI reports whether the source is an OCI registry.
func (r Repo) IsOCI() bool { return strings.HasPrefix(r.URL, ociScheme) }

const ociScheme = "oci://"

// Config is the whole catalog configuration.
type Config struct {
	Repos []Repo `json:"repos"`
}

var (
	repoNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$`)
	categoryRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 &/-]{0,30}$`)
	tagRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]{0,30}$`)
	// OCI references: lowercase path components, as the distribution spec has them.
	ociPathRe = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	ociHostRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?(:[0-9]{1,5})?$`)
	chartName = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
)

const maxOCIChartsPerRepo = 40

// Validate checks the configuration is usable and safe: unique, well-formed
// names (they end up in URLs and audit records) and https repository URLs with
// no embedded credentials.
func (c Config) Validate() error {
	if len(c.Repos) == 0 {
		return errors.New("catalog config needs at least one repository")
	}
	seen := map[string]bool{}
	for _, r := range c.Repos {
		if !repoNameRe.MatchString(r.Name) || seen[r.Name] {
			return fmt.Errorf("catalog config: bad or duplicate repository name %q", r.Name)
		}
		seen[r.Name] = true
		if r.Category != "" && !categoryRe.MatchString(r.Category) {
			return fmt.Errorf("catalog config: repository %s has an invalid category", r.Name)
		}
		if len(r.ChartTags) > 100 {
			return fmt.Errorf("catalog config: repository %s has too many chartTags entries", r.Name)
		}
		for chartKey, tags := range r.ChartTags {
			if !chartName.MatchString(chartKey) || len(tags) > 10 {
				return fmt.Errorf("catalog config: repository %s has a bad chartTags entry %q", r.Name, chartKey)
			}
			for _, t := range tags {
				if !tagRe.MatchString(t) {
					return fmt.Errorf("catalog config: repository %s has an invalid tag %q", r.Name, t)
				}
			}
		}
		if r.IsOCI() {
			if err := validateOCI(r); err != nil {
				return err
			}
			continue
		}
		if len(r.Charts) != 0 {
			return fmt.Errorf("catalog config: repository %s lists charts, which only an oci:// source takes", r.Name)
		}
		u, err := url.Parse(r.URL)
		switch {
		case err != nil || u.Host == "":
			return fmt.Errorf("catalog config: repository %s has an invalid URL", r.Name)
		case u.Scheme != "https":
			return fmt.Errorf("catalog config: repository %s must use https (or be an oci:// source)", r.Name)
		case u.User != nil || u.RawQuery != "" || u.Fragment != "":
			return fmt.Errorf("catalog config: repository %s URL may not carry credentials, a query or a fragment", r.Name)
		}
	}
	return nil
}

// validateOCI checks an oci:// source: a host and path made only of what a
// registry reference allows (so it can't smuggle a scheme, credentials or a
// query), and an explicit, bounded list of chart names. There is deliberately
// no plain-HTTP option: an OCI source is always reached over TLS.
func validateOCI(r Repo) error {
	rest := strings.TrimPrefix(r.URL, ociScheme)
	host, path, _ := strings.Cut(rest, "/")
	if !ociHostRe.MatchString(host) || !ociPathRe.MatchString(path) {
		return fmt.Errorf("catalog config: repository %s has an invalid oci:// URL (want oci://host/path, lowercase, no credentials)", r.Name)
	}
	if len(r.Charts) == 0 || len(r.Charts) > maxOCIChartsPerRepo {
		return fmt.Errorf("catalog config: oci repository %s must list between 1 and %d charts", r.Name, maxOCIChartsPerRepo)
	}
	seen := map[string]bool{}
	for _, c := range r.Charts {
		if !chartName.MatchString(c) || len(c) > 60 || seen[c] {
			return fmt.Errorf("catalog config: oci repository %s has a bad or duplicate chart name %q", r.Name, c)
		}
		seen[c] = true
	}
	return nil
}

// Repo returns the configured repository called name.
func (c Config) Repo(name string) (Repo, bool) {
	for _, r := range c.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

func parseConfig(data []byte) (Config, error) {
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing catalog config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// DefaultConfig is the built-in configuration.
func DefaultConfig() Config {
	c, err := parseConfig(defaultCatalogJSON)
	if err != nil {
		panic("embedded catalog.json is invalid: " + err.Error()) // a build-time bug, caught by tests
	}
	return c
}

// LoadConfig reads and validates the configuration file at path.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is CATALOG_CONFIG, set by the operator
	if err != nil {
		return Config{}, fmt.Errorf("reading catalog config: %w", err)
	}
	return parseConfig(data)
}
