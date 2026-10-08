package gitops

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	"github.com/shiraku/vinaire/backend/internal/clusters"
)

// Errors the HTTP layer turns into statuses.
var (
	ErrUnavailable = errors.New("GitOps isn't available: there is nowhere to keep its configuration (Vinaire isn't running in a cluster and VINAIRE_DATA_DIR isn't set)")
	ErrUnencrypted = errors.New("refusing to store credentials: the configuration store isn't encrypted (give Vinaire access to its key Secret or set CLUSTER_STORE_KEY)")
	ErrNotFound    = errors.New("not found")
	ErrInvalid     = errors.New("invalid request")
	ErrConflict    = errors.New("conflict")
)

const (
	docName           = "gitops"
	defaultInterval   = 180
	minInterval       = 30
	maxInterval       = 3600
	maxSourcesCluster = 20
	maxSourcesTotal   = 100
	maxParallel       = 3
	reconcileTimeout  = 5 * time.Minute
	checkTimeout      = 25 * time.Second
	maxAuditedObjects = 50
	maxStatusObjects  = maxObjects
	identifyTimeout   = 15 * time.Second
)

// ClusterLookup is the part of the cluster registry the manager needs.
type ClusterLookup interface {
	Get(name string) (*clusters.Entry, bool)
}

// AuditFunc records an action that has no HTTP request behind it (a background
// sync). detail never contains credentials.
type AuditFunc func(action, name, identity string, detail map[string]any)

// Manager owns the per-cluster modes and the native sources, and runs one worker
// per source of a cluster whose mode is "native".
type Manager struct {
	store    clusters.DocStore // nil: GitOps unavailable
	registry ClusterLookup
	fetcher  Fetcher
	audit    AuditFunc
	leader   func() bool // nil = this replica always polls

	// replaceable in tests
	newApplier func(cfg *rest.Config, src Source) (Applier, error)
	identify   func(ctx context.Context, entry *clusters.Entry, token string) (identity string, clusterAdmin bool, err error)
	now        func() time.Time

	sem chan struct{}

	mu    sync.Mutex
	root  context.Context
	modes map[string]Mode
	srcs  map[string]*runtime
}

type runtime struct {
	src     Source
	cancel  context.CancelFunc
	trigger chan string // a manual sync, carrying the identity that asked

	mu       sync.Mutex
	status   Status
	lastKeys map[string]ObjectRef
	warnings []string
}

// NewManager builds a manager. store may be nil, in which case every write
// reports ErrUnavailable and reads show everything off.
func NewManager(store clusters.DocStore, registry ClusterLookup, fetcher Fetcher, audit AuditFunc) *Manager {
	return &Manager{
		store: store, registry: registry, fetcher: fetcher, audit: audit,
		newApplier: NewApplier, identify: whoAmI, now: time.Now,
		sem:   make(chan struct{}, maxParallel),
		modes: map[string]Mode{}, srcs: map[string]*runtime{},
	}
}

// Available reports whether configuration can be kept.
func (m *Manager) Available() bool { return m.store != nil }

// Start loads the saved configuration and starts the workers. ctx ends them all.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.root = ctx
	if m.store == nil {
		return nil
	}
	data, err := m.store.LoadDoc(ctx, docName)
	if err != nil {
		return fmt.Errorf("loading the GitOps configuration: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var cfg storedConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing the GitOps configuration: %w", err)
	}
	for c, mode := range cfg.Modes {
		if _, ok := ParseMode(string(mode)); ok {
			m.modes[c] = mode
		}
	}
	for _, s := range cfg.Sources {
		rt := &runtime{src: fromStored(s), trigger: make(chan string, 1), status: Status{Phase: PhasePending, Objects: []ObjectStatus{}}}
		m.srcs[rt.src.ID] = rt
		m.startLocked(rt)
	}
	return nil
}

func (m *Manager) persistLocked(ctx context.Context) error {
	if m.store == nil {
		return ErrUnavailable
	}
	cfg := storedConfig{Modes: map[string]Mode{}, Sources: []storedSource{}}
	for c, mode := range m.modes {
		cfg.Modes[c] = mode
	}
	ids := make([]string, 0, len(m.srcs))
	for id := range m.srcs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	hasSecret := false
	for _, id := range ids {
		s := m.srcs[id].src
		cfg.Sources = append(cfg.Sources, toStored(s))
		hasSecret = hasSecret || s.ApplyToken.IsSet() || s.GitToken.IsSet()
	}
	if hasSecret && !m.store.Encrypted() {
		return ErrUnencrypted
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return m.store.SaveDoc(ctx, docName, data)
}

// ---- modes ------------------------------------------------------------------

// Mode returns the cluster's GitOps mode (off if never set).
func (m *Manager) Mode(cluster string) Mode {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mode, ok := m.modes[cluster]; ok {
		return mode
	}
	return ModeOff
}

// SetMode changes a cluster's mode. Leaving "native" stops its workers (the
// sources stay configured, and run again if native is chosen again).
func (m *Manager) SetMode(ctx context.Context, cluster string, mode Mode) error {
	if _, ok := ParseMode(string(mode)); !ok {
		return fmt.Errorf("%w: mode must be off, native or argocd", ErrInvalid)
	}
	if _, ok := m.registry.Get(cluster); !ok {
		return fmt.Errorf("cluster %q: %w", cluster, ErrNotFound)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return ErrUnavailable
	}
	prev := m.modes[cluster]
	m.modes[cluster] = mode
	if err := m.persistLocked(ctx); err != nil {
		m.modes[cluster] = prev
		return err
	}
	for _, rt := range m.srcs {
		if rt.src.Cluster != cluster {
			continue
		}
		if mode == ModeNative {
			m.startLocked(rt)
		} else {
			m.stopLocked(rt)
		}
	}
	return nil
}

// ---- sources ----------------------------------------------------------------

// AddRequest is a new source as the user filled it in.
type AddRequest struct {
	Name, Cluster, RepoURL, Revision, Path, Namespace string
	Recurse, AutoSync, Force                          bool
	IntervalSeconds                                   int
	GitUsername, GitToken                             string
	ApplyToken                                        string
}

// AllowInsecureGit lets sources use plain http:// repositories on loopback —
// local development only. Set from GITOPS_ALLOW_INSECURE_GIT.
var AllowInsecureGit bool

func validate(r *AddRequest) error {
	r.Name = strings.TrimSpace(r.Name)
	r.RepoURL = strings.TrimSpace(r.RepoURL)
	r.Revision = strings.TrimSpace(r.Revision)
	r.Path = strings.Trim(strings.TrimSpace(r.Path), "/")
	if !nameRe.MatchString(r.Name) {
		return fmt.Errorf("%w: the name must be 1-40 letters, digits, spaces, dots, dashes or underscores", ErrInvalid)
	}
	u, err := url.Parse(r.RepoURL)
	switch {
	case err != nil || u.Host == "":
		return fmt.Errorf("%w: the repository URL isn't valid", ErrInvalid)
	case u.Scheme != "https" && !(AllowInsecureGit && u.Scheme == "http"):
		return fmt.Errorf("%w: the repository URL must be https://", ErrInvalid)
	case u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return fmt.Errorf("%w: put the access token in its own field, not in the URL", ErrInvalid)
	}
	if r.Revision != "" && (!revisionRe.MatchString(r.Revision) || strings.Contains(r.Revision, "..") || strings.HasPrefix(r.Revision, "-")) {
		return fmt.Errorf("%w: invalid branch or tag name", ErrInvalid)
	}
	if !pathRe.MatchString(r.Path) || strings.Contains(r.Path, "..") {
		return fmt.Errorf("%w: invalid path", ErrInvalid)
	}
	if r.Namespace != "" && !nsRe.MatchString(r.Namespace) {
		return fmt.Errorf("%w: invalid default namespace", ErrInvalid)
	}
	if r.GitUsername != "" && !userRe.MatchString(r.GitUsername) {
		return fmt.Errorf("%w: invalid git username", ErrInvalid)
	}
	if r.IntervalSeconds == 0 {
		r.IntervalSeconds = defaultInterval
	}
	if r.IntervalSeconds < minInterval || r.IntervalSeconds > maxInterval {
		return fmt.Errorf("%w: the poll interval must be between %d and %d seconds", ErrInvalid, minInterval, maxInterval)
	}
	if strings.TrimSpace(r.ApplyToken) == "" {
		return fmt.Errorf("%w: a ServiceAccount token to apply the manifests as is required", ErrInvalid)
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	if _, err := crand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// AddSource validates a new source — including that its ServiceAccount token is
// accepted by the cluster and its repository is reachable — saves it and starts
// syncing. Nothing is stored if any check fails.
func (m *Manager) AddSource(ctx context.Context, req AddRequest, identity string) (SourceView, error) {
	if m.store == nil {
		return SourceView{}, ErrUnavailable
	}
	if err := validate(&req); err != nil {
		return SourceView{}, err
	}
	entry, ok := m.registry.Get(req.Cluster)
	if !ok {
		return SourceView{}, fmt.Errorf("cluster %q: %w", req.Cluster, ErrNotFound)
	}
	if m.Mode(req.Cluster) != ModeNative {
		return SourceView{}, fmt.Errorf("%w: switch this cluster's GitOps mode to native first", ErrConflict)
	}
	m.mu.Lock()
	n := 0
	for _, rt := range m.srcs {
		if rt.src.Cluster == req.Cluster {
			n++
			if rt.src.Name == req.Name {
				m.mu.Unlock()
				return SourceView{}, fmt.Errorf("%w: this cluster already has a source called %q", ErrConflict, req.Name)
			}
		}
	}
	if n >= maxSourcesCluster || len(m.srcs) >= maxSourcesTotal {
		m.mu.Unlock()
		return SourceView{}, fmt.Errorf("%w: too many sources", ErrConflict)
	}
	m.mu.Unlock()

	src := Source{
		ID: newID(), Name: req.Name, Cluster: req.Cluster, RepoURL: req.RepoURL, Revision: req.Revision,
		Path: req.Path, Recurse: req.Recurse, Namespace: req.Namespace, IntervalSeconds: req.IntervalSeconds,
		AutoSync: req.AutoSync, Force: req.Force, GitUsername: req.GitUsername,
		GitToken: NewSecret(req.GitToken), ApplyToken: NewSecret(strings.TrimSpace(req.ApplyToken)),
		CreatedBy: identity, CreatedAt: m.now().UTC(),
	}

	idCtx, cancel := context.WithTimeout(ctx, identifyTimeout)
	who, admin, err := m.identify(idCtx, entry, src.ApplyToken.Reveal())
	cancel()
	if err != nil {
		return SourceView{}, fmt.Errorf("%w: the cluster doesn't accept that ServiceAccount token: %v", ErrInvalid, err)
	}
	src.ApplyAs = who

	chkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	err = m.fetcher.Check(chkCtx, src)
	cancel()
	if err != nil {
		return SourceView{}, fmt.Errorf("%w: couldn't reach the repository: %v", ErrInvalid, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	rt := &runtime{src: src, trigger: make(chan string, 1), status: Status{Phase: PhasePending, Objects: []ObjectStatus{}}}
	if admin {
		rt.warnings = append(rt.warnings, "This source applies as a cluster-admin: whatever the repository says, it can create or change anything in the cluster. Prefer a ServiceAccount limited to what this repository should manage.")
	}
	m.srcs[src.ID] = rt
	if err := m.persistLocked(ctx); err != nil {
		delete(m.srcs, src.ID)
		return SourceView{}, err
	}
	m.startLocked(rt)
	return rt.view(), nil
}

// RemoveSource stops and forgets a source. Objects it applied stay in the cluster.
func (m *Manager) RemoveSource(ctx context.Context, id string) (SourceView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.srcs[id]
	if !ok {
		return SourceView{}, fmt.Errorf("source: %w", ErrNotFound)
	}
	v := rt.view()
	m.stopLocked(rt)
	delete(m.srcs, id)
	if err := m.persistLocked(ctx); err != nil {
		m.srcs[id] = rt
		m.startLocked(rt)
		return SourceView{}, err
	}
	return v, nil
}

// ForgetCluster drops everything configured for a cluster that was removed.
func (m *Manager) ForgetCluster(ctx context.Context, cluster string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	for id, rt := range m.srcs {
		if rt.src.Cluster == cluster {
			m.stopLocked(rt)
			delete(m.srcs, id)
			changed = true
		}
	}
	if _, ok := m.modes[cluster]; ok {
		delete(m.modes, cluster)
		changed = true
	}
	if changed && m.store != nil {
		if err := m.persistLocked(ctx); err != nil {
			slog.Warn("gitops: couldn't save the configuration after a cluster was removed", "cluster", cluster, "error", err)
		}
	}
}

// Sources lists a cluster's sources by name.
func (m *Manager) Sources(cluster string) []SourceView {
	m.mu.Lock()
	rts := make([]*runtime, 0)
	for _, rt := range m.srcs {
		if rt.src.Cluster == cluster {
			rts = append(rts, rt)
		}
	}
	m.mu.Unlock()
	out := make([]SourceView, 0, len(rts))
	for _, rt := range rts {
		out = append(out, rt.view())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Source returns one source.
func (m *Manager) Source(id string) (SourceView, error) {
	m.mu.Lock()
	rt, ok := m.srcs[id]
	m.mu.Unlock()
	if !ok {
		return SourceView{}, fmt.Errorf("source: %w", ErrNotFound)
	}
	return rt.view(), nil
}

// SyncNow asks a source's worker to sync immediately, applying whatever is out
// of sync even when auto-sync is off. It returns at once; the status shows the
// outcome.
func (m *Manager) SyncNow(id, identity string) (SourceView, error) {
	m.mu.Lock()
	rt, ok := m.srcs[id]
	running := ok && rt.cancel != nil
	m.mu.Unlock()
	if !ok {
		return SourceView{}, fmt.Errorf("source: %w", ErrNotFound)
	}
	if !running {
		return SourceView{}, fmt.Errorf("%w: this cluster's GitOps mode isn't native, so nothing is syncing", ErrConflict)
	}
	select {
	case rt.trigger <- identity:
	default: // one is already queued
	}
	return rt.view(), nil
}

// ---- workers ----------------------------------------------------------------

// SetLeader makes the scheduled polls run only while fn says this replica is the
// leader (Roadmap Phase 48: several replicas must not all apply the same sources).
// A "Sync now" always runs where it was asked. nil = always (a single replica).
func (m *Manager) SetLeader(fn func() bool) {
	m.mu.Lock()
	m.leader = fn
	m.mu.Unlock()
}

func (m *Manager) isLeader() bool {
	m.mu.Lock()
	fn := m.leader
	m.mu.Unlock()
	return fn == nil || fn()
}

func (m *Manager) startLocked(rt *runtime) {
	if rt.cancel != nil || m.root == nil || m.modes[rt.src.Cluster] != ModeNative {
		return
	}
	ctx, cancel := context.WithCancel(m.root)
	rt.cancel = cancel
	go m.run(ctx, rt)
}

func (m *Manager) stopLocked(rt *runtime) {
	if rt.cancel != nil {
		rt.cancel()
		rt.cancel = nil
	}
}

func (m *Manager) run(ctx context.Context, rt *runtime) {
	interval := time.Duration(rt.src.IntervalSeconds) * time.Second
	m.refreshIdentity(ctx, rt)
	// the first look is soon, but not for every source at the same instant
	timer := time.NewTimer(time.Duration(500+rand.IntN(2500)) * time.Millisecond) // #nosec G404 -- poll jitter, not a security value
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if m.isLeader() {
				m.reconcile(ctx, rt, "")
			}
		case who := <-rt.trigger:
			m.reconcile(ctx, rt, who)
		}
		timer.Reset(interval + time.Duration(rand.IntN(5000))*time.Millisecond) // #nosec G404 -- poll jitter, not a security value
	}
}

// reconcile is one poll: fetch, compare, and — if allowed — apply. manualBy is
// the identity that clicked "Sync now", or "" for a scheduled poll.
func (m *Manager) reconcile(ctx context.Context, rt *runtime, manualBy string) {
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-ctx.Done():
		return
	}
	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()
	src := rt.src
	manual := manualBy != ""
	rt.update(func(s *Status) { s.Phase = PhaseSyncing })

	fail := func(msg string) {
		rt.update(func(s *Status) {
			s.Phase, s.Message = PhaseError, msg
			t := m.now().UTC()
			s.CheckedAt = &t
		})
	}

	entry, ok := m.registry.Get(src.Cluster)
	if !ok {
		fail("the cluster is no longer registered")
		return
	}
	snap, err := m.fetcher.Fetch(ctx, src)
	if err != nil {
		fail("git: " + err.Error())
		return
	}
	objs, err := ParseManifests(snap.Files)
	if err != nil {
		fail("manifests: " + err.Error())
		return
	}
	ap, err := m.newApplier(entry.RESTConfigForToken(src.ApplyToken.Reveal()), src)
	if err != nil {
		fail(err.Error())
		return
	}

	statuses := make([]ObjectStatus, len(objs))
	var pending []int
	for i, o := range objs {
		statuses[i] = ap.Check(ctx, o)
		if st := statuses[i].State; st == StateMissing || st == StateOutOfSync {
			pending = append(pending, i)
		}
	}

	applied, failed := 0, 0
	if len(pending) > 0 && (src.AutoSync || manual) {
		applied, failed = applyPending(ctx, ap, objs, statuses, pending)
	}

	// what is still wrong decides the phase
	outOfSync, errored := 0, 0
	for _, st := range statuses {
		switch st.State {
		case StateMissing, StateOutOfSync:
			outOfSync++
		case StateError:
			errored++
		}
	}
	phase, msg := PhaseSynced, ""
	switch {
	case failed > 0:
		phase, msg = PhaseError, fmt.Sprintf("%d of %d objects failed to apply", failed, len(objs))
	case errored > 0:
		phase, msg = PhaseError, fmt.Sprintf("%d of %d objects couldn't be checked", errored, len(objs))
	case outOfSync > 0:
		phase = PhaseOutOfSync
		msg = fmt.Sprintf("%d of %d objects differ from git", outOfSync, len(objs))
		if !src.AutoSync {
			msg += " — auto-sync is off; use Sync now"
		}
	}

	now := m.now().UTC()
	current := make(map[string]ObjectRef, len(objs))
	for _, o := range objs {
		current[objectKey(o)] = refOf(o)
	}
	rt.update(func(s *Status) {
		s.Phase, s.Message, s.Revision = phase, msg, snap.Commit
		s.CheckedAt = &now
		s.Objects = statuses
		if applied > 0 {
			s.SyncedAt = &now
		}
		if phase == PhaseSynced {
			s.Applied = snap.Commit
			if s.SyncedAt == nil {
				s.SyncedAt = &now
			}
		}
		s.Orphans = nil
		if rt.lastKeys != nil {
			for k, ref := range rt.lastKeys {
				if _, still := current[k]; !still {
					s.Orphans = append(s.Orphans, ref)
				}
			}
			sort.Slice(s.Orphans, func(i, j int) bool { return s.Orphans[i].Name < s.Orphans[j].Name })
		}
		rt.lastKeys = current
	})

	if (applied > 0 || failed > 0) && m.audit != nil {
		who := "gitops:" + src.Name
		if manual {
			who = manualBy
		}
		detail := map[string]any{
			"source": src.ID, "cluster": src.Cluster, "repo": src.RepoURL, "revision": snap.Commit,
			"applied": applied, "failed": failed, "manual": manual, "appliedAs": src.ApplyAs,
		}
		var list []string
		for _, i := range pending {
			if len(list) >= maxAuditedObjects {
				break
			}
			st := statuses[i]
			list = append(list, fmt.Sprintf("%s %s/%s: %s", st.Kind, st.Namespace, st.Name, st.State))
		}
		detail["objects"] = list
		m.audit("gitops-apply", src.Name, who, detail)
	}
}

// applyPending applies the objects that differ, in order. Objects that fail are
// tried once more after the cluster's API discovery is refreshed, which is what
// lets a custom resource follow its own CRD in the same sync.
func applyPending(ctx context.Context, ap Applier, objs []*unstructured.Unstructured, statuses []ObjectStatus, pending []int) (applied, failed int) {
	var retry []int
	apply := func(i int) bool {
		if err := ap.Apply(ctx, objs[i]); err != nil {
			statuses[i].State, statuses[i].Message = StateError, err.Error()
			return false
		}
		statuses[i].State, statuses[i].Message = StateSynced, "applied"
		return true
	}
	for _, i := range pending {
		if apply(i) {
			applied++
		} else {
			retry = append(retry, i)
		}
	}
	if len(retry) == 0 {
		return applied, 0
	}
	ap.Refresh()
	for _, i := range retry {
		if apply(i) {
			applied++
		} else {
			failed++
		}
	}
	return applied, failed
}

// ---- runtime helpers ----------------------------------------------------------

func (rt *runtime) update(f func(*Status)) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	f(&rt.status)
}

func (rt *runtime) view() SourceView {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	st := rt.status
	st.Objects = append([]ObjectStatus(nil), st.Objects...)
	if st.Objects == nil {
		st.Objects = []ObjectStatus{}
	}
	s := rt.src
	return SourceView{
		ID: s.ID, Name: s.Name, Cluster: s.Cluster, RepoURL: s.RepoURL, Revision: s.Revision, Path: s.Path,
		Recurse: s.Recurse, Namespace: s.Namespace, IntervalSeconds: s.IntervalSeconds, AutoSync: s.AutoSync,
		Force: s.Force, HasGitToken: s.GitToken.IsSet(), ApplyAs: s.ApplyAs, CreatedBy: s.CreatedBy,
		CreatedAt: s.CreatedAt, Status: st, Warnings: append([]string(nil), rt.warnings...),
	}
}

// refreshIdentity re-asks the cluster who the source's token is — warnings aren't
// persisted, and a token that has since been revoked should show as an error
// rather than as a source that never syncs.
func (m *Manager) refreshIdentity(ctx context.Context, rt *runtime) {
	entry, ok := m.registry.Get(rt.src.Cluster)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, identifyTimeout)
	defer cancel()
	who, admin, err := m.identify(ctx, entry, rt.src.ApplyToken.Reveal())
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.warnings = nil
	if err != nil {
		rt.warnings = append(rt.warnings, "The cluster doesn't accept this source's ServiceAccount token ("+err.Error()+"); syncing will fail until it is replaced.")
		return
	}
	rt.src.ApplyAs = who
	if admin {
		rt.warnings = append(rt.warnings, "This source applies as a cluster-admin: whatever the repository says, it can create or change anything in the cluster. Prefer a ServiceAccount limited to what this repository should manage.")
	}
}
