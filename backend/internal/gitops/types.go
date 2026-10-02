// Package gitops is Vinaire's GitOps support (Roadmap Phase 29): per cluster, a
// mode — "off", "argocd" (show and drive an Argo CD that is already there) or
// "native" (a deliberately small poll-and-apply loop for clusters without one).
//
// Native mode is a stripped-down Argo CD and is meant to stay one: clone, apply
// with server-side apply, report Synced/OutOfSync. No health checks, no hooks,
// no sync waves, no pruning. Because it runs with nobody logged in, it applies
// as a ServiceAccount whose token the user supplied for that source — never as
// Vinaire's own identity — so the cluster's RBAC bounds what a repository can do.
package gitops

import (
	"regexp"
	"time"
)

// Mode is a cluster's GitOps setting.
type Mode string

const (
	ModeOff    Mode = "off"
	ModeNative Mode = "native"
	ModeArgoCD Mode = "argocd"
)

// ParseMode accepts the three valid values.
func ParseMode(s string) (Mode, bool) {
	switch m := Mode(s); m {
	case ModeOff, ModeNative, ModeArgoCD:
		return m, true
	}
	return "", false
}

// Secret holds a credential. It prints as [REDACTED] through fmt, slog and JSON,
// so it can't reach a log or an API response by accident; Reveal is the one
// deliberate way out.
type Secret struct{ v string }

func NewSecret(v string) Secret             { return Secret{v: v} }
func (s Secret) Reveal() string             { return s.v }
func (s Secret) IsSet() bool                { return s.v != "" }
func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (Secret) MarshalText() ([]byte, error) { return []byte("[REDACTED]"), nil }

// Source is one repository+path synced to one cluster.
type Source struct {
	ID              string
	Name            string
	Cluster         string
	RepoURL         string
	Revision        string // branch or tag; "" or "HEAD" = the repository's default branch
	Path            string // directory in the repository ("" = its root)
	Recurse         bool   // also read sub-directories
	Namespace       string // for namespaced objects that don't name one
	IntervalSeconds int
	AutoSync        bool // apply on every poll; off = only on "Sync now"
	Force           bool // take over fields another manager owns (server-side apply force)
	GitUsername     string
	GitToken        Secret // optional, read-only
	ApplyToken      Secret // the ServiceAccount the manifests are applied as
	ApplyAs         string // that ServiceAccount's identity, as the cluster reported it
	CreatedBy       string
	CreatedAt       time.Time
}

// Phase is a source's overall state.
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseSyncing   Phase = "Syncing"
	PhaseSynced    Phase = "Synced"
	PhaseOutOfSync Phase = "OutOfSync"
	PhaseError     Phase = "Error"
)

// Object states.
const (
	StateSynced    = "Synced"
	StateOutOfSync = "OutOfSync" // exists, differs from git
	StateMissing   = "Missing"   // in git, not in the cluster
	StateError     = "Error"
)

// ObjectRef names an object.
type ObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// ObjectStatus is one manifest's state.
type ObjectStatus struct {
	ObjectRef
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// Status is what the last poll found.
type Status struct {
	Phase     Phase          `json:"phase"`
	Message   string         `json:"message,omitempty"`
	Revision  string         `json:"revision,omitempty"` // commit that was checked
	Applied   string         `json:"appliedRevision,omitempty"`
	CheckedAt *time.Time     `json:"checkedAt,omitempty"`
	SyncedAt  *time.Time     `json:"syncedAt,omitempty"`
	Objects   []ObjectStatus `json:"objects"`
	// Orphans are objects this source applied earlier that are no longer in git.
	// Native mode never deletes (no pruning), so they stay in the cluster; this
	// is only a list, kept in memory for as long as Vinaire runs.
	Orphans []ObjectRef `json:"orphans,omitempty"`
}

// SourceView is a Source as the API shows it: no credentials, only whether they
// are set.
type SourceView struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Cluster         string    `json:"cluster"`
	RepoURL         string    `json:"repoURL"`
	Revision        string    `json:"revision"`
	Path            string    `json:"path"`
	Recurse         bool      `json:"recurse"`
	Namespace       string    `json:"namespace"`
	IntervalSeconds int       `json:"intervalSeconds"`
	AutoSync        bool      `json:"autoSync"`
	Force           bool      `json:"force"`
	HasGitToken     bool      `json:"hasGitToken"`
	ApplyAs         string    `json:"applyAs"`
	CreatedBy       string    `json:"createdBy,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	Status          Status    `json:"status"`
	// Warnings are shown next to the source (e.g. it applies as a cluster-admin).
	Warnings []string `json:"warnings,omitempty"`
}

// storedSource is the on-disk form, which — unlike Source — holds the tokens in
// the clear. It only ever exists inside a sealed document.
type storedSource struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Cluster         string    `json:"cluster"`
	RepoURL         string    `json:"repoURL"`
	Revision        string    `json:"revision,omitempty"`
	Path            string    `json:"path,omitempty"`
	Recurse         bool      `json:"recurse,omitempty"`
	Namespace       string    `json:"namespace,omitempty"`
	IntervalSeconds int       `json:"intervalSeconds"`
	AutoSync        bool      `json:"autoSync"`
	Force           bool      `json:"force,omitempty"`
	GitUsername     string    `json:"gitUsername,omitempty"`
	GitToken        string    `json:"gitToken,omitempty"`
	ApplyToken      string    `json:"applyToken"`
	ApplyAs         string    `json:"applyAs,omitempty"`
	CreatedBy       string    `json:"createdBy,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type storedConfig struct {
	Modes   map[string]Mode `json:"modes"`
	Sources []storedSource  `json:"sources"`
}

func toStored(s Source) storedSource {
	return storedSource{
		ID: s.ID, Name: s.Name, Cluster: s.Cluster, RepoURL: s.RepoURL, Revision: s.Revision,
		Path: s.Path, Recurse: s.Recurse, Namespace: s.Namespace, IntervalSeconds: s.IntervalSeconds,
		AutoSync: s.AutoSync, Force: s.Force, GitUsername: s.GitUsername, GitToken: s.GitToken.Reveal(),
		ApplyToken: s.ApplyToken.Reveal(), ApplyAs: s.ApplyAs, CreatedBy: s.CreatedBy, CreatedAt: s.CreatedAt,
	}
}

func fromStored(s storedSource) Source {
	return Source{
		ID: s.ID, Name: s.Name, Cluster: s.Cluster, RepoURL: s.RepoURL, Revision: s.Revision,
		Path: s.Path, Recurse: s.Recurse, Namespace: s.Namespace, IntervalSeconds: s.IntervalSeconds,
		AutoSync: s.AutoSync, Force: s.Force, GitUsername: s.GitUsername, GitToken: NewSecret(s.GitToken),
		ApplyToken: NewSecret(s.ApplyToken), ApplyAs: s.ApplyAs, CreatedBy: s.CreatedBy, CreatedAt: s.CreatedAt,
	}
}

var (
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,38}[A-Za-z0-9]$|^[A-Za-z0-9]$`)
	revisionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
	pathRe     = regexp.MustCompile(`^[A-Za-z0-9._/-]{0,200}$`)
	nsRe       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	userRe     = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)
)
