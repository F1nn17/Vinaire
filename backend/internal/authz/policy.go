// Package authz implements Roadmap Phase 10's authorization: verifying an
// OIDC bearer token (see verifier.go) and mapping the token's group
// membership to an allow-list of namespaces/actions (this file) — a flat,
// config-backed policy, deliberately not a full policy engine (see
// AGENTS.md and ROADMAP.md Phase 10).
package authz

import (
	"fmt"
	"os"
	"slices"

	"sigs.k8s.io/yaml"
)

// GroupPolicy is what one OIDC group (from the token's `groups` claim) is
// allowed to do. A Namespaces entry of "*" means every namespace, not a
// namespace literally named "*".
type GroupPolicy struct {
	Namespaces []string `json:"namespaces"`
	Actions    []string `json:"actions"`
}

// Policy maps a group name to what that group may do. Loaded once at
// startup from a YAML file (see backend/policy.yaml).
type Policy struct {
	Groups map[string]GroupPolicy `json:"groups"`
}

// LoadPolicy reads and parses the group policy file. sigs.k8s.io/yaml
// (already a dependency) round-trips YAML through JSON, so plain `json`
// struct tags above are enough.
func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file %s: %w", path, err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy file %s: %w", path, err)
	}
	return &p, nil
}

// Claims is the subset of a verified token this backend acts on.
type Claims struct {
	Subject  string
	Username string
	Groups   []string
}

// AllowedNamespaces returns the union of namespaces every group in claims
// grants `action` on. allowAll is true when any of those groups grants
// "*" — callers should treat that as "don't filter" rather than looking
// for a namespace literally named "*", and namespaces is nil in that case.
func (p *Policy) AllowedNamespaces(claims Claims, action string) (namespaces []string, allowAll bool) {
	if p == nil {
		return nil, false
	}
	seen := map[string]struct{}{}
	for _, group := range claims.Groups {
		gp, ok := p.Groups[group]
		if !ok || !slices.Contains(gp.Actions, action) {
			continue
		}
		for _, ns := range gp.Namespaces {
			if ns == "*" {
				return nil, true
			}
			if _, dup := seen[ns]; !dup {
				seen[ns] = struct{}{}
				namespaces = append(namespaces, ns)
			}
		}
	}
	return namespaces, false
}

// Allowed reports whether claims' groups grant `action` on `namespace`
// specifically (used for detail/write endpoints that always target one
// exact namespace, as opposed to a possibly-empty "all namespaces" list
// query — see AllowedNamespaces for that case).
func (p *Policy) Allowed(claims Claims, namespace, action string) bool {
	allowed, allowAll := p.AllowedNamespaces(claims, action)
	return allowAll || slices.Contains(allowed, namespace)
}
