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

// Repo is one configured Helm repository (an index.yaml over https).
type Repo struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Config is the whole catalog configuration.
type Config struct {
	Repos []Repo `json:"repos"`
}

var repoNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$`)

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
		u, err := url.Parse(r.URL)
		switch {
		case err != nil || u.Host == "":
			return fmt.Errorf("catalog config: repository %s has an invalid URL", r.Name)
		case u.Scheme != "https":
			return fmt.Errorf("catalog config: repository %s must use https", r.Name)
		case u.User != nil || u.RawQuery != "" || u.Fragment != "":
			return fmt.Errorf("catalog config: repository %s URL may not carry credentials, a query or a fragment", r.Name)
		}
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
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading catalog config: %w", err)
	}
	return parseConfig(data)
}
