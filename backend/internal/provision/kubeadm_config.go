package provision

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
)

// Roadmap Phase 35: what Vinaire may install with kubeadm — which Kubernetes
// minors, and which CNI at which pinned version — lives in configuration, not
// in code. kubeadm.json (embedded) is the default; KUBEADM_CONFIG points at a
// replacement file. Nothing here is ever "latest": a CNI manifest is fetched
// from the exact version URL written down, so a node installed next month
// matches the one installed today.

//go:embed kubeadm.json
var defaultKubeadmJSON []byte

// KubeadmVersion is one supported Kubernetes minor.
type KubeadmVersion struct {
	Minor   string `json:"minor"`
	Default bool   `json:"default,omitempty"`
}

// KubeadmCNI is one installable pod network.
type KubeadmCNI struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Version  string `json:"version"`
	Manifest string `json:"manifest"` // https URL of the pinned manifest
	// PodCIDR must be what the manifest expects — a mismatch leaves pods without
	// networking, and the failure isn't visible right away.
	PodCIDR string `json:"podCIDR"`
	Default bool   `json:"default,omitempty"`
}

// KubeadmConfig is the whole kubeadm configuration.
type KubeadmConfig struct {
	Versions []KubeadmVersion `json:"versions"`
	CNIs     []KubeadmCNI     `json:"cnis"`
}

var (
	minorRe      = regexp.MustCompile(`^1\.[0-9]{1,2}$`)
	cniNameRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,20}$`)
	cidrRe       = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$`)
	versionTagRe = regexp.MustCompile(`^v[0-9][A-Za-z0-9.\-+]{0,30}$`)
)

// Validate checks the configuration is complete and safe to use: everything
// that ends up in a remote script has a strict shape, manifests are https, and
// there is exactly one default of each kind.
func (c KubeadmConfig) Validate() error {
	if len(c.Versions) == 0 || len(c.CNIs) == 0 {
		return errors.New("kubeadm config needs at least one version and one CNI")
	}
	defaults := 0
	for _, v := range c.Versions {
		if !minorRe.MatchString(v.Minor) {
			return fmt.Errorf("kubeadm config: bad Kubernetes minor %q", v.Minor)
		}
		if v.Default {
			defaults++
		}
	}
	if defaults != 1 {
		return errors.New("kubeadm config: exactly one version must be the default")
	}
	defaults = 0
	seen := map[string]bool{}
	for _, n := range c.CNIs {
		switch {
		case !cniNameRe.MatchString(n.Name) || seen[n.Name]:
			return fmt.Errorf("kubeadm config: bad or duplicate CNI name %q", n.Name)
		case !versionTagRe.MatchString(n.Version):
			return fmt.Errorf("kubeadm config: CNI %s needs a pinned version like v1.2.3", n.Name)
		case !cidrRe.MatchString(n.PodCIDR):
			return fmt.Errorf("kubeadm config: CNI %s has a bad pod CIDR %q", n.Name, n.PodCIDR)
		}
		if u, err := url.Parse(n.Manifest); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("kubeadm config: CNI %s manifest must be an https URL", n.Name)
		}
		seen[n.Name] = true
		if n.Default {
			defaults++
		}
	}
	if defaults != 1 {
		return errors.New("kubeadm config: exactly one CNI must be the default")
	}
	return nil
}

// DefaultKubeadmConfig returns the embedded configuration.
func DefaultKubeadmConfig() KubeadmConfig {
	var c KubeadmConfig
	if err := json.Unmarshal(defaultKubeadmJSON, &c); err != nil {
		panic("embedded kubeadm.json is invalid: " + err.Error())
	}
	if err := c.Validate(); err != nil {
		panic("embedded kubeadm.json is invalid: " + err.Error())
	}
	return c
}

// LoadKubeadmConfig reads a replacement configuration file.
func LoadKubeadmConfig(path string) (KubeadmConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return KubeadmConfig{}, err
	}
	var c KubeadmConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return KubeadmConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, c.Validate()
}

// HasMinor reports whether minor is a supported Kubernetes minor.
func (c KubeadmConfig) HasMinor(minor string) bool {
	for _, v := range c.Versions {
		if v.Minor == minor {
			return true
		}
	}
	return false
}

// DefaultMinor is the version marked default.
func (c KubeadmConfig) DefaultMinor() string {
	for _, v := range c.Versions {
		if v.Default {
			return v.Minor
		}
	}
	return ""
}

// CNI looks a CNI up by name; "" means the default.
func (c KubeadmConfig) CNI(name string) (KubeadmCNI, bool) {
	for _, n := range c.CNIs {
		if (name == "" && n.Default) || n.Name == name {
			return n, true
		}
	}
	return KubeadmCNI{}, false
}
