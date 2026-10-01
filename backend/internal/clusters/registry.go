// Package clusters holds the set of Kubernetes clusters this backend is
// configured to talk to (Roadmap Phase 11) — one Entry per kubeconfig
// context, keyed by context name. It has no dependency on internal/api or
// internal/stream so both can depend on it without an import cycle.
package clusters

import (
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// Entry is everything needed to talk to one configured cluster. Clientset
// is built once at startup with the backend's own credentials — used
// as-is only in internal/authn.ModeDisabled. Every other auth mode instead
// calls ClientsetForToken per session, so each request authenticates to
// the Kubernetes API as the actual logged-in identity (Roadmap Phase 10),
// not as the backend's own service account.
type Entry struct {
	Name             string
	Clientset        *kubernetes.Clientset
	RESTConfig       *rest.Config
	MetricsClientset *metricsclientset.Clientset
	GatewayClientset *gatewayclientset.Clientset

	// Removable is true for a cluster added through the cluster-management
	// UI (live or restored from Store at startup) — false for the single
	// env-var-configured cluster(s) from KUBECONFIG/CLUSTER_CONTEXTS, which
	// the UI shouldn't offer to remove (it would just reappear on the next
	// restart anyway).
	Removable bool

	// clusterType (Roadmap Phase 34) is what kind of cluster this is — see
	// the Type* constants. Detected after registration and editable by the
	// user, so it is read and written through Type/SetType under typeMu.
	typeMu      sync.RWMutex
	clusterType string
}

// Type returns the cluster's type (TypeUnknown until detected).
func (e *Entry) Type() string {
	e.typeMu.RLock()
	defer e.typeMu.RUnlock()
	if e.clusterType == "" {
		return TypeUnknown
	}
	return e.clusterType
}

// SetType records the cluster's type.
func (e *Entry) SetType(t string) {
	e.typeMu.Lock()
	defer e.typeMu.Unlock()
	e.clusterType = t
}

// RESTConfigForToken returns a copy of this Entry's RESTConfig that
// authenticates as token instead of whatever credentials the original
// carries — the server host/CA/TLS settings are reused, but every other
// auth field is cleared first so the caller's token is the only credential
// in effect. Exposed separately from ClientsetForToken because
// internal/stream's exec bridge needs the *rest.Config itself (for
// remotecommand.NewSPDYExecutor), not just a Clientset built from it.
func (e *Entry) RESTConfigForToken(token string) *rest.Config {
	cfg := rest.CopyConfig(e.RESTConfig)
	cfg.BearerToken = token
	cfg.BearerTokenFile = ""
	cfg.Username = ""
	cfg.Password = ""
	cfg.CertData = nil
	cfg.KeyData = nil
	cfg.CertFile = ""
	cfg.KeyFile = ""
	cfg.AuthProvider = nil
	cfg.ExecProvider = nil
	return cfg
}

// ClientsetForToken builds a clientset from RESTConfigForToken(token).
func (e *Entry) ClientsetForToken(token string) (*kubernetes.Clientset, error) {
	return kubernetes.NewForConfig(e.RESTConfigForToken(token))
}

// Registry holds every cluster Vinaire is configured to talk to. Entries
// preserve configuration order; the first one added is the default used
// when a request doesn't specify a cluster.
//
// Guarded by a mutex because, since the cluster-management feature, it's no
// longer just "populated once at startup, read-only forever": AddDynamic/
// Remove can run concurrently with every request handler's Get/Resolve/
// Names/Default.
type Registry struct {
	mu      sync.RWMutex
	order   []string
	entries map[string]*Entry
	onAdd   func(e *Entry) (stop func())
	stops   map[string]func()
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]*Entry), stops: make(map[string]func())}
}

// Add registers e under e.Name, keeping insertion order for Names/Default.
// Used only at startup (main.go's buildClusterEntries), before
// SetLifecycleHook — it does not invoke the lifecycle hook. Prefer
// AddDynamic once the app is serving traffic.
func (r *Registry) Add(e *Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[e.Name]; !exists {
		r.order = append(r.order, e.Name)
	}
	r.entries[e.Name] = e
}

// SetLifecycleHook installs the function AddDynamic calls for every cluster
// it adds (main.go wires this to start that cluster's live-update
// informers) — a plain callback, not an import on internal/stream, since
// internal/stream already imports internal/clusters and a reverse import
// would cycle. Call this once, before the app starts serving traffic.
func (r *Registry) SetLifecycleHook(onAdd func(e *Entry) (stop func())) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onAdd = onAdd
}

// AddDynamic adds a cluster at runtime — used both to replay the
// lifecycle hook for the initially-configured clusters at startup, and to
// register a cluster added live through the cluster-management UI. Unlike
// Add, it invokes the lifecycle hook and remembers its returned stop
// function so Remove can clean it up later.
func (r *Registry) AddDynamic(e *Entry) {
	r.mu.Lock()
	if _, exists := r.entries[e.Name]; !exists {
		r.order = append(r.order, e.Name)
	}
	r.entries[e.Name] = e
	hook := r.onAdd
	r.mu.Unlock()

	if hook == nil {
		return
	}
	stop := hook(e)
	r.mu.Lock()
	r.stops[e.Name] = stop
	r.mu.Unlock()
}

// Remove drops a dynamically-added cluster, calling its lifecycle stop
// function (if any) — e.g. cancelling its informers. ok is false if name
// wasn't configured at all.
func (r *Registry) Remove(name string) (ok bool) {
	r.mu.Lock()
	if _, exists := r.entries[name]; !exists {
		r.mu.Unlock()
		return false
	}
	delete(r.entries, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	stop := r.stops[name]
	delete(r.stops, name)
	r.mu.Unlock()

	if stop != nil {
		stop()
	}
	return true
}

func (r *Registry) Get(name string) (*Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	return e, ok
}

// Default is the first cluster added.
func (r *Registry) Default() *Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.order) == 0 {
		return nil
	}
	return r.entries[r.order[0]]
}

// Names lists every configured cluster in configuration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// Resolve picks the Entry for name, or the Default if name is empty. ok is
// false only when a non-empty name doesn't match any configured cluster.
func (r *Registry) Resolve(name string) (*Entry, bool) {
	if name == "" {
		d := r.Default()
		return d, d != nil
	}
	return r.Get(name)
}

// Empty reports whether no cluster was successfully configured at all.
func (r *Registry) Empty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.order) == 0
}
