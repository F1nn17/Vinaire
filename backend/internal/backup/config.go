// Package backup builds the Jobs, CronJobs and VolumeSnapshots of Roadmap Phase 77b — backups of an
// application Vinaire installed from an image.
//
// It builds objects and validates what goes into them; it creates nothing (internal/k8s does, as the
// caller). Rules it keeps:
//   - A dump runs in the instance's own image (so the client matches the server's version), with a
//     fixed script. Nothing a person types is spliced into a script: the script reads environment
//     variables, and every value is validated by a strict pattern first.
//   - The database's password is never read by Vinaire. The Job gets a reference to the instance's
//     Secret (secretKeyRef) and the pod reads it.
//   - An S3 target comes only from the operator's configuration (BACKUP_TARGETS): a request names a
//     target and the Secret holding the credentials, never an endpoint.
package backup

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// DefaultUploadImage uploads a dump to S3 (the AWS CLI speaks to any S3-compatible store). It is
// pinned; an operator may change it in the configuration.
const DefaultUploadImage = "amazon/aws-cli:2.17.40"

var (
	nameRe   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	bucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	prefixRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*){0,5}$`)
	regionRe = regexp.MustCompile(`^[a-z0-9-]{2,32}$`)
	imageRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]{0,127}:[A-Za-z0-9._-]{1,64}$`)
)

// S3Target is one place dumps may be uploaded to, named by the operator.
type S3Target struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix,omitempty"`
	Region   string `json:"region,omitempty"`
}

// Config is the operator's backup configuration.
type Config struct {
	Targets     []S3Target `json:"targets"`
	UploadImage string     `json:"uploadImage,omitempty"`
}

// Target finds a configured S3 target by name.
func (c *Config) Target(name string) (S3Target, bool) {
	if c == nil {
		return S3Target{}, false
	}
	for _, t := range c.Targets {
		if t.Name == name {
			return t, true
		}
	}
	return S3Target{}, false
}

// Image is the upload image to use.
func (c *Config) Image() string {
	if c == nil || c.UploadImage == "" {
		return DefaultUploadImage
	}
	return c.UploadImage
}

// Validate checks every target: https endpoint that is not a loopback/link-local/unspecified literal,
// and strict patterns for the rest, because these values reach a Job's environment.
func (c *Config) Validate() error {
	seen := map[string]bool{}
	for i, t := range c.Targets {
		if !nameRe.MatchString(t.Name) || seen[t.Name] {
			return fmt.Errorf("target %d: invalid or duplicate name %q", i, t.Name)
		}
		seen[t.Name] = true
		u, err := url.Parse(t.Endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("target %q: endpoint must be a plain https URL", t.Name)
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
			return fmt.Errorf("target %q: endpoint address is not allowed", t.Name)
		}
		if strings.EqualFold(u.Hostname(), "localhost") {
			return fmt.Errorf("target %q: endpoint address is not allowed", t.Name)
		}
		if !bucketRe.MatchString(t.Bucket) {
			return fmt.Errorf("target %q: invalid bucket", t.Name)
		}
		if t.Prefix != "" && !prefixRe.MatchString(t.Prefix) {
			return fmt.Errorf("target %q: invalid prefix", t.Name)
		}
		if t.Region != "" && !regionRe.MatchString(t.Region) {
			return fmt.Errorf("target %q: invalid region", t.Name)
		}
	}
	if c.UploadImage != "" && !imageRe.MatchString(c.UploadImage) {
		return fmt.Errorf("invalid uploadImage")
	}
	return nil
}

// LoadConfig reads the file named by BACKUP_TARGETS (empty: no S3 targets, dumps to a PVC only).
func LoadConfig(path string) (*Config, error) {
	c := &Config{}
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path) // #nosec G304 -- the path is the operator's BACKUP_TARGETS setting, never from a request
	if err != nil {
		return nil, fmt.Errorf("read backup configuration: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("parse backup configuration: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("backup configuration: %w", err)
	}
	return c, nil
}
